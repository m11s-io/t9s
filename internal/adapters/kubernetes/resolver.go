package kubernetes

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/m11s-io/t9s/internal/ports"
)

type resolver struct{}

func NewResolver() ports.KubernetesResolver {
	return &resolver{}
}

func (*resolver) Resolve(_ context.Context, talosContext string) (ports.KubernetesNodeReader, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := loadingRules.Load()
	if err != nil {
		return nil, nil
	}
	if _, ok := config.Contexts[talosContext]; !ok {
		return nil, nil
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: talosContext}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client config for context %q: %w", talosContext, err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client for context %q: %w", talosContext, err)
	}

	return newNodeReader(clientset), nil
}
