package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/cloudflare/cloudflare-go"
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
