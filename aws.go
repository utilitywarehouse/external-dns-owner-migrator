package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"

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

// sanitizeAWSRecordAddress accepts a string and makes sure that it ends in a
// trailing dot to make sure hostnames comply with record names
func sanitizeAWSRecordAddress(address string) string {
	if !strings.HasSuffix(address, ".") {
		return address + "."
	}
	return address
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

// lookupExternalDNSTXTRecords returns all the TXT records found for a hostname
func lookupExternalDNSTXTRecords(hostname, prefix string, records []types.ResourceRecordSet) []types.ResourceRecordSet {
	var externalDNSRecords []types.ResourceRecordSet
	found, record := verifyRecord(hostname, records)
	if !found {
		return externalDNSRecords
	}
	txtRecord := fmt.Sprintf("%s-%s", prefix, sanitizeAWSRecordAddress(hostname))
	txtTypeRecord := fmt.Sprintf("%s-%s-%s", prefix, strings.ToLower(string(record.Type)), sanitizeAWSRecordAddress(hostname))
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
		if *record.Name == sanitizeAWSRecordAddress(hostname) {
			return true, record
		}
	}
	return false, types.ResourceRecordSet{}
}
