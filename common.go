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
