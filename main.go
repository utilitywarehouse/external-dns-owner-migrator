package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
)

var (
	flagAWSZoneID             = flag.String("aws-zone-id", getEnv("MIGRATOR_AWS_ZONE_ID", ""), "AWS Route53 Zone ID")
	flagDelete                = flag.Bool("delete", false, "Delete function will look for DNS records of an old owner and delete them. Not implemented yet")
	flagDryRun                = flag.Bool("dry-run", true, "Whether to dry run or actually apply changes. Defaults to true")
	flagExternalDNSOwnerIDNew = flag.String("external-dns-owner-id-new", getEnv("MIGRATOR_EXTERNAL_DNS_OWNER_ID_NEW", ""), "New ExternalDNS owner ID. Required for migration")
	flagExternalDNSOwnerIDOld = flag.String("external-dns-owner-id-old", getEnv("MIGRATOR_EXTERNAL_DNS_OWNER_ID_OLD", ""), "ExternalDNS owner ID to be replaced. Required for migration and deletion")
	flagExternalDNSPrefix     = flag.String("external-dns-prefix", getEnv("MIGRATOR_EXTERNAL_DNS_PREFIX", ""), "Prefix of ExternalDNS TXT records. Required for migration and deletion")
	flagMigrate               = flag.Bool("migrate", false, "Migrate function will migrate owners to the a new ID")
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
	kubeClient, err := kubeClientFromConfig(kubeConfigPath, *flagKubeContext)
	if err != nil {
		log.Fatalf("Cannot create Kubernetes client: %v\n", err)
	}

	route53Client := newRoute53Client()

	if *flagMigrate {
		if *flagExternalDNSOwnerIDNew == "" || *flagExternalDNSOwnerIDOld == "" || *flagExternalDNSPrefix == "" {
			usage()
		}
		err := migrateAWSRoute53Owner(route53Client, kubeClient, *flagExternalDNSPrefix, *flagExternalDNSOwnerIDOld, *flagExternalDNSOwnerIDNew, *flagAWSZoneID, *flagDryRun)
		if err != nil {
			log.Fatal(err)
		}
	}

	if *flagDelete {
		dynamicKubeClient, err := dynamicKubeClientFromConfig(kubeConfigPath, *flagKubeContext)
		if err != nil {
			log.Fatalf("Cannot create dynamic Kubernetes client: %v\n", err)
		}
		if *flagExternalDNSOwnerIDOld == "" || *flagExternalDNSPrefix == "" {
			usage()
		}
		log.Println("Delete not fully supported, can only dry-run")
		err = deleteAWSRoute53OwnerRecords(route53Client, kubeClient, dynamicKubeClient, *flagExternalDNSPrefix, *flagExternalDNSOwnerIDOld, *flagAWSZoneID, *flagDryRun)
		if err != nil {
			log.Fatal(err)
		}
	}
}
