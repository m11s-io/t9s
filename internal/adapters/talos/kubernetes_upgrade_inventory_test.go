//go:build !windows

package talos

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInspectTalosCLIRequiresSupportedPatch(t *testing.T) {
	for _, version := range []string{"v1.14.6", "v1.14.5", "v1.14.7", "v1.14.6-rc.1"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "talosctl")
			executable := []byte("#!/bin/sh\nprintf 'Client:\\n  Tag: " + version + "\\n'\n")
			require.NoError(t, os.WriteFile(path, executable, 0o700))
			t.Setenv("PATH", dir)
			identity, err := inspectTalosCLI(context.Background())
			if version != "v1.14.6" {
				require.ErrorContains(t, err, "talosctl v1.14.6 is required")
				require.Empty(t, identity)
				return
			}
			require.NoError(t, err)
			require.Equal(t, path, identity.Path)
			require.Equal(t, version, identity.Version)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(executable)), identity.SHA256)
		})
	}
}
