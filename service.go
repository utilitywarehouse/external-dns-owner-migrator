package main

import (
	"context"
	"fmt"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func externalDNSServiceHostnames(clientset *kubernetes.Clientset) ([]string, error) {
	var hostnames []string
	// Regex to match annotations like external-dns.alpha.kubernetes.io/.*=<value>
	re := regexp.MustCompile(`^external-dns\.alpha\.kubernetes\.io/.*$`)

	services, err := clientset.CoreV1().Services("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services.Items {
		for key, value := range svc.Annotations {
			if re.MatchString(key) {
				hostnames = append(hostnames, value)
			}
		}
	}
	return hostnames, nil
}
