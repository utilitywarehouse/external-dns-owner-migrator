package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"google.golang.org/api/dns/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func newGCPDNSClient() (*dns.Service, error) {
	return dns.NewService(context.Background())
}

func gcpDNSRecordsList(dnsService *dns.Service, projectID, zoneName string) ([]*dns.ResourceRecordSet, error) {
	// List all DNS records in the zone
	resp, err := dnsService.ResourceRecordSets.List(projectID, zoneName).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list DNS records: %w", err)
	}
	return resp.Rrsets, nil
}

func modifyGCPRecordValues(service *dns.Service, projectID string, zoneName string, record *dns.ResourceRecordSet, newValues []string) error {
	change := &dns.Change{
		Deletions: []*dns.ResourceRecordSet{record},
		Additions: []*dns.ResourceRecordSet{
			{
				Name:    record.Name,
				Type:    record.Type,
				Ttl:     record.Ttl,
				Rrdatas: newValues,
			},
		},
	}

	_, err := service.Changes.Create(projectID, zoneName, change).Do()
	if err != nil {
		return fmt.Errorf("failed to update DNS record: %w", err)
	}
	return nil
}

func deleteGCPDNSRecord(service *dns.Service, projectID string, zoneName string, record *dns.ResourceRecordSet) error {
	change := &dns.Change{
		Deletions: []*dns.ResourceRecordSet{record},
	}

	_, err := service.Changes.Create(projectID, zoneName, change).Do()
	if err != nil {
		return fmt.Errorf("failed to delete DNS record: %w", err)
	}
	return nil
}

func migrateGCPDNSOwner(service *dns.Service, kubeClient *kubernetes.Clientset, prefix, oldOwner, newOwner, projectID, zoneName string, dryRun bool) error {
	hostnames, err := externalDNSKubeHostnames(kubeClient)
	if err != nil {
		return err
	}

	records, err := gcpDNSRecordsList(service, projectID, zoneName)
	if err != nil {
		return fmt.Errorf("Cannot list records in gcp zone: %s, %v", zoneName, err)
	}
	for _, hostname := range hostnames {
		for _, r := range lookupExternalDNSGCPTXTRecords(hostname, prefix, records) {
			var newValues []string
			for _, rr := range r.Rrdatas {
				if verifyOwner(rr, oldOwner) {
					v, err := replaceOwner(rr, newOwner)
					if err != nil {
						return err
					}
					newValues = append(newValues, v)
				}
			}
			if len(newValues) == 0 {
				continue
			}
			msg := fmt.Sprintf("Updating record: %s Type: %s with values: %s", r.Name, string(r.Type), newValues)
			if dryRun {
				msg += " (dry run)"
			}
			fmt.Println(msg)
			if !dryRun {
				if err := modifyGCPRecordValues(service, projectID, zoneName, r, newValues); err != nil {
					log.Printf("Failed to update record: %v", err)
				}
			}
		}
	}
	return nil
}

func deleteGCPDNSOwnerRecords(service *dns.Service, kubeClient *kubernetes.Clientset, dynamiKubeClient *dynamic.DynamicClient, prefix, owner, projectID, zoneName string, dryRun bool) error {
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

	allRecords, err := gcpDNSRecordsList(service, projectID, zoneName)
	if err != nil {
		return fmt.Errorf("Cannot list records in GCP zone: %s, project: %s : %v", zoneName, projectID, err)
	}

	toDeleteRecords := ownedGCPDNSRecordsList(allRecords, prefix, owner)
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
			deleteGCPDNSRecord(service, projectID, zoneName, record)
		}
		// Delete TXT ownership records
		for _, txt := range lookupExternalDNSGCPTXTRecords(record.Name, prefix, allRecords) {
			msg := fmt.Sprintf("Deleting record: %s Type: %s", txt.Name, txt.Type)
			if dryRun {
				msg += " (dry run)"
			}
			fmt.Println(msg)
			if !dryRun {
				deleteGCPDNSRecord(service, projectID, zoneName, txt)
			}
		}
	}
	return nil
}

// ownedGCPDNSRecordsList expects a list of GCP DNS records, a prefix and an
// owner ID and will return a list of records, including TXT ones, that belong
// to the owner ID.
func ownedGCPDNSRecordsList(records []*dns.ResourceRecordSet, prefix, owner string) []*dns.ResourceRecordSet {
	ownedRecords := []*dns.ResourceRecordSet{}
	for _, record := range records {
		if record.Type == "TXT" {
			continue
		}
		owned := false
		for _, r := range lookupExternalDNSGCPTXTRecords(record.Name, prefix, records) {
			for _, rr := range r.Rrdatas {
				if verifyOwner(rr, owner) {
					owned = true
					break
				}
			}
		}
		if owned {
			ownedRecords = append(ownedRecords, record)
		}

	}
	return ownedRecords
}

// lookupExternalDNSGCPTXTRecords returns all the TXT records found for a hostname
func lookupExternalDNSGCPTXTRecords(hostname, prefix string, records []*dns.ResourceRecordSet) []*dns.ResourceRecordSet {
	var externalDNSRecords []*dns.ResourceRecordSet
	found, record := verifyGCPRecord(hostname, records)
	if !found {
		return externalDNSRecords
	}
	txtRecord := fmt.Sprintf("%s-%s", prefix, sanitizeDNSAddress(hostname))
	txtTypeRecord := fmt.Sprintf("%s-%s-%s", prefix, strings.ToLower(string(record.Type)), sanitizeDNSAddress(hostname))
	for _, record := range records {
		if (txtRecord == record.Name || txtTypeRecord == record.Name) && record.Type == "TXT" {
			externalDNSRecords = append(externalDNSRecords, record)
		}
	}
	return externalDNSRecords
}

// verifyGCPDNSRecord verifies that a hostname is in the passed list of records and
// returns the respective record
func verifyGCPRecord(hostname string, records []*dns.ResourceRecordSet) (bool, *dns.ResourceRecordSet) {
	for _, record := range records {
		if record.Name == sanitizeDNSAddress(hostname) {
			return true, record
		}
	}
	return false, nil
}
