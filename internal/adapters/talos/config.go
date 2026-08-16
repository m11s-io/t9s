package talos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// ConfigCatalog exposes the non-sensitive context metadata in a talosconfig.
type ConfigCatalog struct {
	paths []string
}

var _ ports.ContextCatalog = (*ConfigCatalog)(nil)

// NewConfigCatalog creates a catalog backed by one or more talosconfig files.
// With no paths, TALOSCONFIGS or Talos' standard path resolution is used.
func NewConfigCatalog(paths ...string) *ConfigCatalog {
	return &ConfigCatalog{paths: append([]string(nil), paths...)}
}

// List returns every Talos context without its credentials.
func (c *ConfigCatalog) List(context.Context) ([]domain.ClusterContext, error) {
	config, err := openTalosconfig(c.paths...)
	if err != nil {
		return nil, fmt.Errorf("load talosconfig: %w", err)
	}

	if selected, ok := config.Contexts[config.Context]; !ok || selected == nil {
		return nil, fmt.Errorf("selected Talos context %q is missing", config.Context)
	}

	contextNames := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		contextNames = append(contextNames, name)
	}
	sort.Strings(contextNames)

	contexts := make([]domain.ClusterContext, 0, len(contextNames))
	for _, name := range contextNames {
		talosContext := config.Contexts[name]
		if talosContext == nil {
			return nil, fmt.Errorf("Talos context %q is missing", name)
		}

		contexts = append(contexts, domain.ClusterContext{
			Name:      name,
			Cluster:   talosContext.Cluster,
			Endpoints: append([]string(nil), talosContext.Endpoints...),
			Nodes:     append([]string(nil), talosContext.Nodes...),
			Current:   name == config.Context,
		})
	}

	return contexts, nil
}

func configPaths(explicit []string) []string {
	if len(explicit) > 0 && !(len(explicit) == 1 && strings.TrimSpace(explicit[0]) == "") {
		return append([]string(nil), explicit...)
	}

	if value, ok := os.LookupEnv("TALOSCONFIGS"); ok {
		paths := filepath.SplitList(value)
		result := make([]string, 0, len(paths))
		for _, path := range paths {
			if strings.TrimSpace(path) != "" {
				result = append(result, path)
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	return []string{""}
}

func openTalosconfig(paths ...string) (config *clientconfig.Config, err error) {
	resolved := configPaths(paths)
	merged := &clientconfig.Config{Contexts: map[string]*clientconfig.Context{}}

	for index, path := range resolved {
		loaded, loadErr := openTalosconfigFile(path)
		if loadErr != nil {
			return nil, loadErr
		}

		if index == 0 {
			merged.Context = loaded.Context
		}

		for name, talosContext := range loaded.Contexts {
			if _, exists := merged.Contexts[name]; exists {
				return nil, fmt.Errorf("duplicate Talos context %q", name)
			}
			if talosContext == nil {
				return nil, fmt.Errorf("Talos context %q is missing", name)
			}
			merged.Contexts[name] = talosContext
		}
	}

	return merged, nil
}

func openTalosconfigFile(path string) (config *clientconfig.Config, err error) {
	defer func() {
		if recover() != nil {
			config = nil
			err = fmt.Errorf("invalid Talos context entry")
		}
	}()

	return clientconfig.Open(path)
}
