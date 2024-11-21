package main

import (
	"context"
	"fmt"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// IngressRoute GVR (GroupVersionResource) for Traefik
var ingressRouteGVR = schema.GroupVersionResource{
	Group:    "traefik.io",
	Version:  "v1alpha1",
	Resource: "ingressroutes",
}

func ingressRouteList(client *dynamic.DynamicClient) ([]runtime.Object, error) {
	// Fetch all IngressRoute resources across all namespaces
	ingressRoutes, err := client.Resource(ingressRouteGVR).Namespace("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list IngressRoute resources: %w", err)
	}

	// Convert items to runtime.Object
	var results []runtime.Object
	for _, item := range ingressRoutes.Items {
		results = append(results, item.DeepCopyObject())
	}

	return results, nil
}

// extractHostnamesFromIngressRoutes parses the list of IngressRoutes and extracts all hostnames.
func extractHostnamesFromIngressRoutes(ingressRoutes []runtime.Object) ([]string, error) {
	var hostnames []string

	for _, obj := range ingressRoutes {
		// Cast the runtime.Object to *unstructured.Unstructured
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return nil, fmt.Errorf("unexpected object type: %T", obj)
		}

		// Access the "spec" field
		spec, found, err := unstructured.NestedMap(unstructuredObj.Object, "spec")
		if err != nil || !found {
			continue
		}

		// Access the "routes" field within "spec"
		routes, found, err := unstructured.NestedSlice(spec, "routes")
		if err != nil || !found {
			continue
		}

		// Iterate over routes to extract hostnames
		for _, route := range routes {
			routeMap, ok := route.(map[string]interface{})
			if !ok {
				continue
			}

			// Check for "match" field and parse hostnames
			match, ok := routeMap["match"].(string)
			if ok && match != "" {
				hostname, err := extractHost(match)
				if err != nil {
					fmt.Printf("Cannot extract hostname from IngressRoute match rule: %s\n", match)
				}
				hostnames = append(hostnames, hostname)
			}
		}
	}

	return hostnames, nil
}

// ExtractHost extracts the content inside Host(`...`) and removes backticks.
func extractHost(input string) (string, error) {
	re := regexp.MustCompile(`Host\(` + "`(.*?)`" + `\)`)
	match := re.FindStringSubmatch(input)
	if len(match) < 2 {
		return "", fmt.Errorf("no valid Host(`...`) pattern found in input")
	}
	return match[1], nil
}
