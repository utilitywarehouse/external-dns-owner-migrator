package main

import (
	"fmt"
	"strings"
)

// replaceOwner expects an input in the form of:
// heritage=external-dns,external-dns/owner=infra,external-dns/resource=ingress/sys-terraform-applier/terraform-applier-pub
// and will replace the external-dns/owner=infra part with the new owner id.
func replaceOwner(input, newOwner string) (string, error) {
	pairs := strings.Split(input, ",")
	found := false
	for i, pair := range pairs {
		if strings.HasPrefix(pair, "external-dns/owner=") {
			found = true
			pairs[i] = "external-dns/owner=" + newOwner
			break
		}
	}

	if !found {
		return "", fmt.Errorf("`external-dns/owner` key not found in input")
	}
	return strings.Join(pairs, ","), nil
}

// verifyOwner expects an input in the form of:
// heritage=external-dns,external-dns/owner=infra,external-dns/resource=ingress/sys-terraform-applier/terraform-applier-pub
// and returns true if the owner matches the passed string.
func verifyOwner(input, owner string) bool {
	pairs := strings.Split(input, ",")
	for _, pair := range pairs {
		if strings.HasPrefix(pair, "external-dns/owner=") {
			o := strings.TrimPrefix(pair, "external-dns/owner=")
			return owner == o
		}
	}
	return false
}

// sanitizeDNSAddress get an address and ensures that there is a trailing dot
// in it
func sanitizeDNSAddress(address string) string {
	if !strings.HasSuffix(address, ".") {
		return address + "."
	}
	return address
}

// addressInList takes an address and a list of hostnames and checks if the
// sanitized name is found in the list
func addressInList(address string, hostnames []string) bool {
	a := sanitizeDNSAddress(address)
	for _, hostname := range hostnames {
		if a == sanitizeDNSAddress(hostname) {
			return true
		}
	}
	return false
}
