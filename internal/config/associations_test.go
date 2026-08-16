package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/m11s-io/t9s/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPathPrefersXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg-home")

	assert.Equal(t, filepath.Join("/xdg-home", "t9s", "config.yaml"), config.DefaultPath())
}

func TestDefaultPathFallsBackToDotConfigWhenXDGConfigHomeUnset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	assert.Equal(t, filepath.Join(home, ".config", "t9s", "config.yaml"), config.DefaultPath())
}

func TestLoadReturnsEmptyAssociationsWhenFileIsMissing(t *testing.T) {
	associations, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	require.NoError(t, err)
	assert.Empty(t, associations.Items)
}

func TestLoadParsesWellFormedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "kubernetesAssociations:\n  - kubernetesContext: prod-eu\n    talosContext: prod\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	associations, err := config.Load(path)

	require.NoError(t, err)
	require.Len(t, associations.Items, 1)
	assert.Equal(t, "prod-eu", associations.Items[0].KubernetesContext)
	assert.Equal(t, "prod", associations.Items[0].TalosContext)
}

func TestLoadReturnsErrorForMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not: [valid: yaml"), 0o600))

	_, err := config.Load(path)

	assert.Error(t, err)
}

func TestTalosContextForReturnsMappedContext(t *testing.T) {
	associations := config.Associations{Items: []config.Association{{KubernetesContext: "prod-eu", TalosContext: "prod"}}}

	talosContext, ok := associations.TalosContextFor("prod-eu")

	assert.True(t, ok)
	assert.Equal(t, "prod", talosContext)
}

func TestTalosContextForReturnsFalseWhenUnmapped(t *testing.T) {
	associations := config.Associations{}

	_, ok := associations.TalosContextFor("staging")

	assert.False(t, ok)
}
