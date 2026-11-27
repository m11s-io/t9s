//go:build !windows

package talos

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestInspectTalosCLIRequiresSupportedPatch(t *testing.T) {
	for _, tc := range talosCLIVersionCases(t, domain.MinimumTalosCLIVersion) {
		t.Run(tc.version, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "talosctl")
			executable := []byte("#!/bin/sh\nprintf 'Client:\\n  Tag: " + tc.version + "\\n'\n")
			require.NoError(t, os.WriteFile(path, executable, 0o700))
			t.Setenv("PATH", dir)
			identity, err := inspectTalosCLI(context.Background())
			if !tc.supported {
				require.ErrorContains(t, err, talosCLIRequirement+" is required")
				require.Empty(t, identity)
				return
			}
			require.NoError(t, err)
			require.Equal(t, path, identity.Path)
			require.Equal(t, tc.version, identity.Version)
			require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(executable)), identity.SHA256)
		})
	}
}

type talosCLIVersionCase struct {
	version   string
	supported bool
}

// talosCLIVersionCases derives accepted and rejected talosctl versions from
// the minimum so bumping it never requires editing tests.
func talosCLIVersionCases(t *testing.T, minimum string) []talosCLIVersionCase {
	t.Helper()
	v, err := semver.ParseTolerant(minimum)
	require.NoError(t, err)
	require.NotZero(t, v.Patch, "minimum needs a previous patch to test against")
	previous, next, nextMinor := v, v, v
	previous.Patch--
	next.Patch++
	nextMinor.Minor++
	nextMinor.Patch = 0
	return []talosCLIVersionCase{
		{minimum, true},
		{"v" + next.String(), true},
		{"v" + previous.String(), false},
		{"v" + nextMinor.String(), false},
		{"v" + next.String() + "-rc.1", false},
		{strings.TrimPrefix(minimum, "v"), false},
	}
}
