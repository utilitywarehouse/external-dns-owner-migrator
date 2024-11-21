package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func newRoute53Client() *route53.Client {
	// Load AWS SDK configuration
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("Unable to load AWS SDK config: %v", err)
	}
	return route53.NewFromConfig(cfg)
}

func route53RecordsList(client *route53.Client, zoneID string) ([]types.ResourceRecordSet, error) {
	var allRecords []types.ResourceRecordSet
	var nextRecordName *string
	var nextRecordType types.RRType

	for {
		// Get the records page by page
		resp, err := client.ListResourceRecordSets(context.TODO(), &route53.ListResourceRecordSetsInput{
			HostedZoneId:    &zoneID,
			StartRecordName: nextRecordName,
			StartRecordType: nextRecordType,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list resource record sets: %w", err)
		}
		// Append the current page of records to the allRecords slice
		allRecords = append(allRecords, resp.ResourceRecordSets...)

		// If there are no more records, break out of the loop
		if resp.IsTruncated {
			nextRecordName = resp.NextRecordName
			nextRecordType = resp.NextRecordType
		} else {
			break
		}
	}
	return allRecords, nil
}

func modifyRoute53RecordValue(client *route53.Client, zoneID string, record *types.ResourceRecordSet, newValues []string) error {
	var resourceRecords []types.ResourceRecord
	for _, value := range newValues {
		resourceRecords = append(resourceRecords, types.ResourceRecord{Value: &value})
	}

	changeBatch := &types.ChangeBatch{
		Changes: []types.Change{
			{
				Action: types.ChangeActionUpsert, // Update the existing record or create it if it doesn’t exist
				ResourceRecordSet: &types.ResourceRecordSet{
					Name:            record.Name,
					Type:            record.Type,
					TTL:             record.TTL,
					ResourceRecords: resourceRecords,
				},
			},
		},
	}

	// Execute the changes
	_, err := client.ChangeResourceRecordSets(context.TODO(), &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: &zoneID,
		ChangeBatch:  changeBatch,
	})
	if err != nil {
		return fmt.Errorf("failed to modify record value: %w", err)
	}
	return nil
}

func deleteRoute53Record(client *route53.Client, zoneID string, record types.ResourceRecordSet) error {
	change := types.Change{
		Action:            types.ChangeActionDelete,
		ResourceRecordSet: &record,
	}

	input := &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: &zoneID,
		ChangeBatch: &types.ChangeBatch{
			Changes: []types.Change{change},
		},
	}

	_, err := client.ChangeResourceRecordSets(context.TODO(), input)
	if err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}
	return nil
}

func migrateAWSRoute53Owner(client *route53.Client, kubeClient *kubernetes.Clientset, prefix, oldOwner, newOwner, zoneID string, dryRun bool) error {
	hostnames, err := externalDNSHostnames(kubeClient)
	if err != nil {
		return fmt.Errorf("Cannot list Ingresses: %v", err)
	}

	records, err := route53RecordsList(client, zoneID)
	if err != nil {
		return fmt.Errorf("Cannot list records in aws zone with ID: %s, %v", zoneID, err)
	}
	for _, hostname := range hostnames {
		for _, r := range lookupExternalDNSTXTRecords(hostname, prefix, records) {
			var newValues []string
			for _, rr := range r.ResourceRecords {
				if verifyOwner(*rr.Value, oldOwner) {
					r, err := replaceOwner(*rr.Value, newOwner)
					if err != nil {
						return err
					}
					newValues = append(newValues, r)
				}
			}
			if len(newValues) == 0 {
				continue
			}
			msg := fmt.Sprintf("Updating record: %s Type: %s with values: %s", *r.Name, string(r.Type), newValues)
			if dryRun {
				msg += " (dry run)"
			}
			fmt.Println(msg)
			if !dryRun {
				if err := modifyRoute53RecordValue(client, zoneID, &r, newValues); err != nil {
					log.Printf("Failed to update record: %v", err)
				}
			}
		}
	}
	return nil
}

func deleteAWSRoute53OwnerRecords(client *route53.Client, kubeClient *kubernetes.Clientset, dynamiKubeClient *dynamic.DynamicClient, prefix, owner, zoneID string, dryRun bool) error {
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

	allRecords, err := route53RecordsList(client, zoneID)
	if err != nil {
		return fmt.Errorf("Cannot list records in aws zone with ID: %s, %v", zoneID, err)
	}

	toDeleteRecords := ownedRoute53RecordsList(allRecords, prefix, owner)
	for _, record := range toDeleteRecords {
		if record.Type == "TXT" {
			continue
		}
		// Skip if the record is still found in Ingress resources of the cluster
		if addressInList(*record.Name, ingressHostnames) {
			fmt.Printf("Skipping record: %s found in Ingress rules hosts\n", *record.Name)
			continue
		}

		// Skip if the record is still found in an IngressRoute host
		if addressInList(*record.Name, ingressRouteHostnames) {
			fmt.Printf("Skipping record: %s found in IngressRoute rule hosts\n", *record.Name)
			continue
		}

		// Delete record
		msg := fmt.Sprintf("Deleting record: %s Type: %s", *record.Name, record.Type)
		if dryRun {
			msg += " (dry run)"
		}
		fmt.Println(msg)
		if !dryRun {
			deleteRoute53Record(client, zoneID, record)
		}
		// Delete TXT ownership records
		for _, txt := range lookupExternalDNSTXTRecords(*record.Name, prefix, allRecords) {
			msg := fmt.Sprintf("Deleting record: %s Type: %s", *txt.Name, txt.Type)
			if dryRun {
				msg += " (dry run)"
			}
			fmt.Println(msg)
			if !dryRun {
				deleteRoute53Record(client, zoneID, txt)
			}
		}
	}
	return nil
}

// ownedRoute53RecordsList expects a list of route53 records, a prefix and an
// owner ID and will return a list of records, including TXT ones, that belong
// to the owner ID.
func ownedRoute53RecordsList(records []types.ResourceRecordSet, prefix, owner string) []types.ResourceRecordSet {
	ownedRecords := []types.ResourceRecordSet{}
	for _, record := range records {
		if record.Type == "TXT" {
			continue
		}
		owned := false
		for _, r := range lookupExternalDNSTXTRecords(*record.Name, prefix, records) {
			for _, rr := range r.ResourceRecords {
				if verifyOwner(*rr.Value, owner) {
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

// lookupExternalDNSTXTRecords returns all the TXT records found for a hostname
func lookupExternalDNSTXTRecords(hostname, prefix string, records []types.ResourceRecordSet) []types.ResourceRecordSet {
	var externalDNSRecords []types.ResourceRecordSet
	found, record := verifyRecord(hostname, records)
	if !found {
		return externalDNSRecords
	}
	txtRecord := fmt.Sprintf("%s-%s", prefix, sanitizeDNSAddress(hostname))
	txtTypeRecord := fmt.Sprintf("%s-%s-%s", prefix, strings.ToLower(string(record.Type)), sanitizeDNSAddress(hostname))
	for _, record := range records {
		if (txtRecord == *record.Name || txtTypeRecord == *record.Name) && record.Type == "TXT" {
			externalDNSRecords = append(externalDNSRecords, record)
		}
	}
	return externalDNSRecords
}

// verifyRecord verifies that a hostname is in the passed list of records and
// returns the respective record
func verifyRecord(hostname string, records []types.ResourceRecordSet) (bool, types.ResourceRecordSet) {
	for _, record := range records {
		if *record.Name == sanitizeDNSAddress(hostname) {
			return true, record
		}
	}
	return false, types.ResourceRecordSet{}
}
