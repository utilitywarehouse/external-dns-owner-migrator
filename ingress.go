package main

import (
	"context"
	"fmt"
	"regexp"

	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

var (
	externalDNSRegex = regexp.MustCompile(`^external-dns\.alpha\.kubernetes\.io/.*`)
)

func ingressList(clientset *kubernetes.Clientset) ([]v1.Ingress, error) {
	ingressList, err := clientset.NetworkingV1().Ingresses("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Ingress resources: %w", err)
	}

	return ingressList.Items, nil
}

func allIngressHosts(clientset *kubernetes.Clientset) ([]string, error) {
	var hostnames []string
	ingresses, err := ingressList(clientset)
	if err != nil {
		return hostnames, err
	}
	for _, ingress := range ingresses {
		for _, rule := range ingress.Spec.Rules {
			if rule.Host != "" {
				hostnames = append(hostnames, rule.Host)
			}
		}

	}
	return hostnames, nil
}

func externalDNSHostnames(clientset *kubernetes.Clientset) ([]string, error) {
	var hostnames []string
	ingresses, err := ingressList(clientset)
	if err != nil {
		return hostnames, err
	}
	for _, ingress := range ingresses {
		for key := range ingress.Annotations {
			if externalDNSRegex.MatchString(key) {
				for _, rule := range ingress.Spec.Rules {
					if rule.Host != "" {
						hostnames = append(hostnames, rule.Host)
					}
				}
				break // No need to check other annotations for this ingress
			}
		}
	}
	return hostnames, nil
}
