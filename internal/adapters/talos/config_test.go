package talos_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/m11s-io/t9s/internal/adapters/talos"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigCatalogListReturnsSortedSanitizedContexts(t *testing.T) {
	path := writeTalosconfig(t, `
context: prod
contexts:
  prod:
    cluster: production
    endpoints:
      - https://prod.example:50000
    nodes:
      - 10.0.0.10
    ca: fake-ca-credential
    crt: fake-crt-credential
    key: fake-key-credential
  dev:
    cluster: development
    endpoints:
      - https://dev.example:50000
    nodes:
      - 10.0.0.20
    ca: fake-dev-ca-credential
    crt: fake-dev-crt-credential
    key: fake-dev-key-credential
`)

	catalog := talos.NewConfigCatalog(path)
	contexts, err := catalog.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "prod"}, names(contexts))
	assert.True(t, contexts[1].Current)
	assert.Equal(t, []string{"https://prod.example:50000"}, contexts[1].Endpoints)

	encoded, err := json.Marshal(contexts)
	require.NoError(t, err)
	assert.Equal(t, []string{"Cluster", "Current", "Endpoints", "Name", "Nodes"}, jsonFieldNames(t, encoded))
	assert.NotContains(t, string(encoded), "fake-ca-credential")
	assert.NotContains(t, string(encoded), "fake-crt-credential")
	assert.NotContains(t, string(encoded), "fake-key-credential")
	assert.NotContains(t, string(encoded), "fake-dev-ca-credential")
	assert.NotContains(t, string(encoded), "fake-dev-crt-credential")
	assert.NotContains(t, string(encoded), "fake-dev-key-credential")
}

func TestConfigCatalogListMergesTALOSCONFIGSFiles(t *testing.T) {
	mgmt := writeTalosconfig(t, `
context: mgmt
contexts:
  mgmt:
    cluster: management
    endpoints: [https://mgmt.example:50000]
`)
	ai := writeTalosconfig(t, `
context: ai
contexts:
  ai:
    cluster: artificial-intelligence
    endpoints: [https://ai.example:50000]
`)
	t.Setenv("TALOSCONFIGS", mgmt+string(filepath.ListSeparator)+ai)

	contexts, err := talos.NewConfigCatalog("").List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"ai", "mgmt"}, names(contexts))
	assert.False(t, contexts[0].Current)
	assert.True(t, contexts[1].Current)
}

func TestConfigCatalogListRejectsDuplicateTALOSCONFIGSContext(t *testing.T) {
	first := writeTalosconfig(t, `
context: mgmt
contexts:
  mgmt:
    endpoints: [https://first.example:50000]
`)
	second := writeTalosconfig(t, `
context: mgmt
contexts:
  mgmt:
    endpoints: [https://second.example:50000]
`)
	t.Setenv("TALOSCONFIGS", first+string(filepath.ListSeparator)+second)

	_, err := talos.NewConfigCatalog("").List(context.Background())

	require.EqualError(t, err, `load talosconfig: duplicate Talos context "mgmt"`)
}

func TestConfigCatalogExplicitPathOverridesTALOSCONFIGS(t *testing.T) {
	explicit := writeTalosconfig(t, `
context: explicit
contexts:
  explicit:
    endpoints: [https://explicit.example:50000]
`)
	other := writeTalosconfig(t, `
context: other
contexts:
  other:
    endpoints: [https://other.example:50000]
`)
	t.Setenv("TALOSCONFIGS", other)

	contexts, err := talos.NewConfigCatalog(explicit).List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"explicit"}, names(contexts))
}

func TestConfigCatalogListReturnsSelectedContextErrorForAbsentExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "talosconfig")
	catalog := talos.NewConfigCatalog(path)

	_, err := catalog.List(context.Background())

	require.EqualError(t, err, `selected Talos context "" is missing`)
	_, statErr := os.Stat(path)
	require.NoError(t, statErr)
}

func TestConfigCatalogListReturnsLoadErrorForUnreadableConfigPath(t *testing.T) {
	catalog := talos.NewConfigCatalog(t.TempDir())

	_, err := catalog.List(context.Background())

	require.Error(t, err)
	assert.ErrorContains(t, err, "load talosconfig")
}

func TestConfigCatalogListReturnsErrorWhenSelectedContextIsMissing(t *testing.T) {
	path := writeTalosconfig(t, `
context: missing
contexts:
  dev:
    endpoints:
      - https://dev.example:50000
`)

	catalog := talos.NewConfigCatalog(path)

	_, err := catalog.List(context.Background())

	require.EqualError(t, err, `selected Talos context "missing" is missing`)
}

func TestConfigCatalogListReturnsErrorForNullNonSelectedContext(t *testing.T) {
	path := writeTalosconfig(t, `
context: prod
contexts:
  prod:
    endpoints:
      - https://prod.example:50000
  broken: null
`)

	catalog := talos.NewConfigCatalog(path)

	_, err := catalog.List(context.Background())

	require.EqualError(t, err, "load talosconfig: invalid Talos context entry")
}

func names(contexts []domain.ClusterContext) []string {
	result := make([]string, len(contexts))

	for i, clusterContext := range contexts {
		result[i] = clusterContext.Name
	}

	return result
}

func jsonFieldNames(t *testing.T, encoded []byte) []string {
	t.Helper()

	var contexts []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &contexts))
	require.NotEmpty(t, contexts)

	fields := make([]string, 0, len(contexts[0]))
	for field := range contexts[0] {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	return fields
}

func writeTalosconfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "talosconfig")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}
