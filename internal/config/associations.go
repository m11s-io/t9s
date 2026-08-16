package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Association struct {
	KubernetesContext string `yaml:"kubernetesContext"`
	TalosContext      string `yaml:"talosContext"`
}

type Associations struct {
	Items []Association `yaml:"kubernetesAssociations"`
}

func (a Associations) TalosContextFor(kubernetesContext string) (string, bool) {
	for _, association := range a.Items {
		if association.KubernetesContext == kubernetesContext {
			return association.TalosContext, true
		}
	}
	return "", false
}

// DefaultPath returns the standard XDG-conformant location for t9s's
// optional association config: $XDG_CONFIG_HOME/t9s/config.yaml, falling
// back to ~/.config/t9s/config.yaml when XDG_CONFIG_HOME is unset.
func DefaultPath() string {
	if home := os.Getenv("XDG_CONFIG_HOME"); home != "" {
		return filepath.Join(home, "t9s", "config.yaml")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "t9s", "config.yaml")
	}
	return filepath.Join(homeDir, ".config", "t9s", "config.yaml")
}

// Load reads and parses the association file at path. A missing file is
// not an error — it returns empty Associations, identical to having no
// config at all. A malformed file returns an error; the caller decides
// how to surface it (t9s treats it as a non-fatal startup notice).
func Load(path string) (Associations, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Associations{}, nil
	}
	if err != nil {
		return Associations{}, fmt.Errorf("read t9s config %q: %w", path, err)
	}

	var associations Associations
	if err := yaml.Unmarshal(content, &associations); err != nil {
		return Associations{}, fmt.Errorf("parse t9s config %q: %w", path, err)
	}

	return associations, nil
}
