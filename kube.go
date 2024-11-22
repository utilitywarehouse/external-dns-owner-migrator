package main

import (
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

func dynamicKubeClientFromConfig(path, context string) (*dynamic.DynamicClient, error) {
	conf, err := getClientConfig(path, context)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes client config: %v", err)
	}
	return dynamic.NewForConfig(conf)
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

// externalDNSKubeHostnames will return all the hostnames found in a cluster
// that shall be managed by externalDNS
func externalDNSKubeHostnames(kubeClient *kubernetes.Clientset) ([]string, error) {
	ingresses, err := externalDNSIngressHostnames(kubeClient)
	if err != nil {
		return []string{}, fmt.Errorf("Cannot list Ingresses: %v", err)
	}
	services, err := externalDNSServiceHostnames(kubeClient)
	if err != nil {
		return []string{}, fmt.Errorf("Cannot list Services: %v", err)
	}
	return append(ingresses, services...), nil
}
