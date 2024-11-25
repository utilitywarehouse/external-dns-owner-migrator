package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cloudflare/cloudflare-go"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func newCloudflareAPIClient(key, email string) (*cloudflare.API, error) {
	return cloudflare.New(key, email)
}

// ListCloudflareRecords lists all DNS records for a given zone in a Cloudflare account.
func cloudflareRecordsList(api *cloudflare.API, zoneName string) ([]cloudflare.DNSRecord, error) {
	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch zone ID: %w", err)
	}

	records, _, err := api.ListDNSRecords(context.Background(), cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}
	return records, nil
}

func modifyCloudflareDNSRecord(api *cloudflare.API, zoneName string, record cloudflare.DNSRecord, newContent string) error {
	updatedRecord := cloudflare.UpdateDNSRecordParams{
		ID:      record.ID,
		Type:    record.Type,
		Name:    record.Name,
		Content: newContent,
		TTL:     record.TTL,     // Retain existing TTL
		Proxied: record.Proxied, // Retain existing Proxied status
	}

	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil {
		return fmt.Errorf("failed to fetch zone ID: %w", err)
	}
	_, err = api.UpdateDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), updatedRecord)
	if err != nil {
		return fmt.Errorf("failed to update DNS record: %w", err)
	}
	return nil
}

func deleteCloudflareDNSRecord(api *cloudflare.API, zoneName string, record cloudflare.DNSRecord) error {
	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil {
		return fmt.Errorf("failed to fetch zone ID: %w", err)
	}

	return api.DeleteDNSRecord(context.Background(), cloudflare.ZoneIdentifier(zoneID), record.ID)
}

func migrateCloudflareRecordOwner(api *cloudflare.API, kubeClient *kubernetes.Clientset, prefix, oldOwner, newOwner, zoneName string, dryRun bool) error {
	hostnames, err := externalDNSKubeHostnames(kubeClient)
	if err != nil {
		return err
	}
	records, err := cloudflareRecordsList(api, zoneName)
	if err != nil {
		return fmt.Errorf("Cannot list records in cloudfare zone named: %s, %v", zoneName, err)
	}
	for _, hostname := range hostnames {
		for _, record := range lookupExternalDNSCloudflareTXTRecords(hostname, prefix, records) {
			var newContent string
			if verifyOwner(record.Content, oldOwner) {
				v, err := replaceOwner(record.Content, newOwner)
				if err != nil {
					return err
				}
				newContent = v
			}
			if newContent == "" {
				continue
			}
			msg := fmt.Sprintf("Updating record: %s Type: %s with values: %s", record.Name, record.Type, newContent)
			if dryRun {
				msg += " (dry run)"
			}
			fmt.Println(msg)
			if !dryRun {
				if err := modifyCloudflareDNSRecord(api, zoneName, record, newContent); err != nil {
					log.Printf("Failed to update record: %v", err)
				}
			}

		}
	}
	return nil
}

func deleteCloudflareOwnerRecords(api *cloudflare.API, kubeClient *kubernetes.Clientset, dynamiKubeClient *dynamic.DynamicClient, prefix, owner, zoneName string, dryRun bool) error {
	ingressHostnames, err := allIngressHosts(kubeClient)
	if err != nil {
		return fmt.Errorf("Cannot list Ingresses: %v", err)
	}

	ingressRoutes, err := ingressRouteList(dynamiKubeClient)
	if err != nil {
		return fmt.Errorf("Cannot list IngressRoute hosts: %v", err)
	}
	ingressRouteHostnames, err := extractHostnamesFromIngressRoutes(ingressRoutes)
	if err != nil {
		return fmt.Errorf("Cannot extract hostnames from ingress routes")
	}
	serviceHostnames, err := externalDNSServiceHostnames(kubeClient)
	if err != nil {
		return fmt.Errorf("Cannot list Services: %v", err)
	}

	allRecords, err := cloudflareRecordsList(api, zoneName)
	if err != nil {
		return fmt.Errorf("Cannot list records in cloudflare zone: %s, %v", zoneName, err)
	}

	toDeleteRecords := ownedCloudflareRecordsList(allRecords, prefix, owner)
	for _, record := range toDeleteRecords {
		if record.Type == "TXT" {
			continue
		}
		// Skip if the record is still found in Ingress resources of the cluster
		if addressInList(record.Name, ingressHostnames) {
			fmt.Printf("Skipping record: %s found in Ingress rules hosts\n", record.Name)
			continue
		}
		// Skip if the record is still found in an IngressRoute host
		if addressInList(record.Name, ingressRouteHostnames) {
			fmt.Printf("Skipping record: %s found in IngressRoute rule hosts\n", record.Name)
			continue
		}
		// Skip if the record is still found as a hostname annotation in a Service
		if addressInList(record.Name, serviceHostnames) {
			fmt.Printf("Skipping record: %s found in Service as external-DNS hostname link\n", record.Name)
			continue
		}
		// Delete record
		msg := fmt.Sprintf("Deleting record: %s Type: %s", record.Name, record.Type)
		if dryRun {
			msg += " (dry run)"
		}
		fmt.Println(msg)
		if !dryRun {
			deleteCloudflareDNSRecord(api, zoneName, record)
		}
		// Delete TXT ownership records
		for _, txt := range lookupExternalDNSCloudflareTXTRecords(record.Name, prefix, allRecords) {
			msg := fmt.Sprintf("Deleting record: %s Type: %s", txt.Name, txt.Type)
			if dryRun {
				msg += " (dry run)"
			}
			fmt.Println(msg)
			if !dryRun {
				deleteCloudflareDNSRecord(api, zoneName, txt)
			}
		}
	}
	return nil
}

// ownedCloudflareRecordsList expects a list of route53 records, a prefix and an
// owner ID and will return a list of records, including TXT ones, that belong
// to the owner ID.
func ownedCloudflareRecordsList(records []cloudflare.DNSRecord, prefix, owner string) []cloudflare.DNSRecord {
	ownedRecords := []cloudflare.DNSRecord{}
	for _, record := range records {
		if record.Type == "TXT" {
			continue
		}
		owned := false
		for _, r := range lookupExternalDNSCloudflareTXTRecords(record.Name, prefix, records) {
			if verifyOwner(r.Content, owner) {
				owned = true
				break
			}
		}
		if owned {
			ownedRecords = append(ownedRecords, record)
		}

	}
	return ownedRecords
}

func lookupExternalDNSCloudflareTXTRecords(hostname, prefix string, records []cloudflare.DNSRecord) []cloudflare.DNSRecord {
	var externalDNSRecords []cloudflare.DNSRecord
	found, record := verifyCloudflareRecord(hostname, records)
	if !found {
		return externalDNSRecords
	}
	txtRecord := fmt.Sprintf("%s-%s", prefix, sanitizeDNSAddress(hostname))
	txtTypeRecord := fmt.Sprintf("%s-%s-%s", prefix, strings.ToLower(string(record.Type)), sanitizeDNSAddress(hostname))
	for _, record := range records {
		if (txtRecord == sanitizeDNSAddress(record.Name) || txtTypeRecord == sanitizeDNSAddress(record.Name)) && record.Type == "TXT" {
			externalDNSRecords = append(externalDNSRecords, record)
		}
	}
	return externalDNSRecords
}

// verifyCloudflareRecord verifies that a hostname is in the passed list of records and
// returns the respective record
func verifyCloudflareRecord(hostname string, records []cloudflare.DNSRecord) (bool, cloudflare.DNSRecord) {
	for _, record := range records {
		if sanitizeDNSAddress(record.Name) == sanitizeDNSAddress(hostname) {
			return true, record
		}
	}
	return false, cloudflare.DNSRecord{}
}
