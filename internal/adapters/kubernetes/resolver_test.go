package kubernetes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: prod
  cluster:
    server: https://10.0.0.1:6443
    insecure-skip-tls-verify: true
contexts:
- name: prod
  context:
    cluster: prod
    user: prod-user
users:
- name: prod-user
  user:
    token: fake-token-for-testing-only
current-context: prod
`

const talosNamedClusterKubeconfig = "apiVersion: v1\n" +
	"kind: Config\n" +
	"clusters:\n" +
	"- name: plt\n" +
	"  cluster:\n" +
	"    server: https://10.0.0.1:6443\n" +
	"    insecure-skip-tls-verify: true\n" +
	"contexts:\n" +
	"- name: admin@plt\n" +
	"  context:\n" +
	"    cluster: plt\n" +
	"    user: admin\n" +
	"users:\n" +
	"- name: admin\n" +
	"  user:\n" +
	"    token: fake-token-for-testing-only\n" +
	"current-context: admin@plt\n"

func writeTestKubeconfig(t *testing.T) string {
	t.Helper()
	return writeTestKubeconfigContents(t, testKubeconfig)
}

func writeTestKubeconfigContents(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestResolveReturnsReaderWhenContextNameMatchesExactly(t *testing.T) {
	t.Setenv("KUBECONFIG", writeTestKubeconfig(t))
	resolver := NewResolver()

	reader, err := resolver.Resolve(t.Context(), "prod")

	require.NoError(t, err)
	assert.NotNil(t, reader)
}

func TestResolveUsesCurrentKubeContextWhenItsClusterMatchesTalosContext(t *testing.T) {
	t.Setenv("KUBECONFIG", writeTestKubeconfigContents(t, talosNamedClusterKubeconfig))
	resolver := NewResolver()

	reader, err := resolver.Resolve(t.Context(), "plt")

	require.NoError(t, err)
	assert.NotNil(t, reader)
}

func TestResolveReturnsNilReaderWhenNoContextMatches(t *testing.T) {
	t.Setenv("KUBECONFIG", writeTestKubeconfig(t))
	resolver := NewResolver()

	reader, err := resolver.Resolve(t.Context(), "staging")

	require.NoError(t, err)
	assert.Nil(t, reader)
}

func TestResolveReturnsNilReaderWhenKubeconfigIsAbsent(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	resolver := NewResolver()

	reader, err := resolver.Resolve(t.Context(), "prod")

	require.NoError(t, err)
	assert.Nil(t, reader)
}
