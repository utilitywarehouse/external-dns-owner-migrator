package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
)

var (
	flagAWSZoneID             = flag.String("aws-zone-id", getEnv("MIGRATOR_AWS_ZONE_ID", ""), "AWS Route53 Zone ID")
	flagCloudflareZoneName    = flag.String("cloudflare-zone-name", getEnv("MIGRATOR_CF_ZONE_NAME", ""), "Cloudflare DNS zone name")
	flagDelete                = flag.Bool("delete", false, "Delete function will look for DNS records of an old owner and delete them. Not implemented yet")
	flagDryRun                = flag.Bool("dry-run", true, "Whether to dry run or actually apply changes. Defaults to true")
	flagExternalDNSOwnerIDNew = flag.String("external-dns-owner-id-new", getEnv("MIGRATOR_EXTERNAL_DNS_OWNER_ID_NEW", ""), "New ExternalDNS owner ID. Required for migration")
	flagExternalDNSOwnerIDOld = flag.String("external-dns-owner-id-old", getEnv("MIGRATOR_EXTERNAL_DNS_OWNER_ID_OLD", ""), "ExternalDNS owner ID to be replaced. Required for migration and deletion")
	flagExternalDNSPrefix     = flag.String("external-dns-prefix", getEnv("MIGRATOR_EXTERNAL_DNS_PREFIX", ""), "Prefix of ExternalDNS TXT records. Required for migration and deletion")
	flagMigrate               = flag.Bool("migrate", false, "Migrate function will migrate owners to the a new ID")
	flagProvider              = flag.String("provider", getEnv("MIGRATOR_PROVIDER", ""), "(required) The cloud provider of the DNS zones to manage records. [aws|cloudflare]")
	flagKubeContext           = flag.String("kube-context", getEnv("MIGRATOR_KUBE_CONTEXT", ""), "Kubernetes cluster context to look for extarnal-DNS ingresses")
	flagKubeConfigPath        = flag.String("kube-config", getEnv("MIGRATOR_KUBE_CONFIG", ""), "Path to the local kube config. If not set ~/.kube/config will be used")
)

func usage() {
	flag.Usage()
	os.Exit(1)
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

func main() {
	flag.Parse()
	kubeConfigPath := *flagKubeConfigPath
	if kubeConfigPath == "" {
		kubeConfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	if *flagProvider == "" {
		usage()
	}
	if *flagProvider == "aws" {
		providerAWS(*flagMigrate, *flagDelete, *flagDryRun, *flagAWSZoneID, *flagExternalDNSOwnerIDOld, *flagExternalDNSOwnerIDNew, *flagExternalDNSPrefix, kubeConfigPath, *flagKubeContext)
	}
	if *flagProvider == "cloudflare" {
		providerCloudflare(*flagMigrate, *flagDelete, *flagDryRun, *flagCloudflareZoneName, *flagExternalDNSOwnerIDOld, *flagExternalDNSOwnerIDNew, *flagExternalDNSPrefix, kubeConfigPath, *flagKubeContext)
	}

}

func providerAWS(migrate, del, dryRun bool, zoneID, oldOwnerID, newOwnerID, prefix, kubeConfigPath, kubeContext string) {
	kubeClient, err := kubeClientFromConfig(kubeConfigPath, kubeContext)
	if err != nil {
		log.Fatalf("Cannot create Kubernetes client: %v\n", err)
	}
	route53Client := newRoute53Client()
	if migrate {
		if newOwnerID == "" || oldOwnerID == "" || prefix == "" {
			usage()
		}
		err := migrateAWSRoute53Owner(route53Client, kubeClient, prefix, oldOwnerID, newOwnerID, zoneID, dryRun)
		if err != nil {
			log.Fatal(err)
		}
	}

	if del {
		dynamicKubeClient, err := dynamicKubeClientFromConfig(kubeConfigPath, kubeContext)
		if err != nil {
			log.Fatalf("Cannot create dynamic Kubernetes client: %v\n", err)
		}
		if oldOwnerID == "" || prefix == "" {
			usage()
		}
		err = deleteAWSRoute53OwnerRecords(route53Client, kubeClient, dynamicKubeClient, prefix, oldOwnerID, zoneID, dryRun)
		if err != nil {
			log.Fatal(err)
		}
	}

}

func providerCloudflare(migrate, del, dryRun bool, zoneName, oldOwnerID, newOwnerID, prefix, kubeConfigPath, kubeContext string) {
	kubeClient, err := kubeClientFromConfig(kubeConfigPath, kubeContext)
	if err != nil {
		log.Fatalf("Cannot create Kubernetes client: %v\n", err)
	}
	apiKey := getEnv("CLOUDFLARE_API_KEY", "")
	email := getEnv("CLOUDFLARE_EMAIL", "")
	cloudflareAPIClient, err := newCloudflareAPIClient(apiKey, email)
	if err != nil {
		log.Fatalf("Cannot create Cloudflare API client from key: %v\n", err)
	}
	if migrate {
		if newOwnerID == "" || oldOwnerID == "" || prefix == "" {
			usage()
		}
		err := migrateCloudflareRecordOwner(cloudflareAPIClient, kubeClient, prefix, oldOwnerID, newOwnerID, zoneName, dryRun)
		if err != nil {
			log.Fatal(err)
		}
	}
	if del {
		dynamicKubeClient, err := dynamicKubeClientFromConfig(kubeConfigPath, kubeContext)
		if err != nil {
			log.Fatalf("Cannot create dynamic Kubernetes client: %v\n", err)
		}
		if oldOwnerID == "" || prefix == "" {
			usage()
		}
		err = deleteCloudflareOwnerRecords(cloudflareAPIClient, kubeClient, dynamicKubeClient, prefix, oldOwnerID, zoneName, dryRun)
		if err != nil {
			log.Fatal(err)
		}
	}

}
