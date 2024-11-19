package main

import (
	"context"
	"fmt"
	"regexp"

	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	externalDNSRegex = regexp.MustCompile(`^external-dns\.alpha\.kubernetes\.io/.*`)
)

// kubeClientFromConfig returns a Kubernetes client (clientset) from the kubeconfig
// path or from the in-cluster service account environment.
func kubeClientFromConfig(path, context string) (*kubernetes.Clientset, error) {
	conf, err := getClientConfig(path, context)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client config: %v", err)
	}
	return kubernetes.NewForConfig(conf)
}

// getClientConfig returns a Kubernetes client Config.
func getClientConfig(path, context string) (*rest.Config, error) {
	if path != "" {
		// build Config from a kubeconfig filepath
		configLoader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: path},
			&clientcmd.ConfigOverrides{CurrentContext: context},
		)
		return configLoader.ClientConfig()
	}
	// uses pod's service account to get a Config
	return rest.InClusterConfig()
}

func ingressList(clientset *kubernetes.Clientset) ([]v1.Ingress, error) {
	ingressList, err := clientset.NetworkingV1().Ingresses("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Ingress resources: %w", err)
	}

	return ingressList.Items, nil
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
