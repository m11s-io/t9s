package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
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
	kubeContext := talosContext
	if _, ok := config.Contexts[kubeContext]; !ok {
		// Kubeconfig context names often include a user prefix, while the
		// cluster name still matches the Talos context (for example plt/admin@plt).
		kubeContext = ""
		for name, candidate := range config.Contexts {
			if candidate == nil || candidate.Cluster != talosContext {
				continue
			}
			if name == config.CurrentContext {
				kubeContext = name
				break
			}
			if kubeContext != "" {
				return nil, nil
			}
			kubeContext = name
		}
	}
	if kubeContext == "" {
		return nil, nil
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client config for context %q: %w", kubeContext, err)
	}

	clientset, err := corev1.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes client for context %q: %w", kubeContext, err)
	}

	return newNodeReader(clientset.Nodes()), nil
}
