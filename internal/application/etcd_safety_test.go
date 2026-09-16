package application_test

import (
	"strings"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/stretchr/testify/assert"
)

func TestValidateEtcdSnapshotPathRejectsEmptyAndDotPaths(t *testing.T) {
	for _, path := range []string{"", "   ", ".", "..", "/tmp/../..", "foo/", "/var/backups/"} {
		t.Run(path, func(t *testing.T) {
			assert.Error(t, application.ValidateEtcdSnapshotPath(path))
		})
	}
}

func TestValidateEtcdSnapshotPathAcceptsFilePaths(t *testing.T) {
	for _, path := range []string{"backup.db", "/var/backups/etcd.db", "sub/dir/snap.db"} {
		t.Run(path, func(t *testing.T) {
			assert.NoError(t, application.ValidateEtcdSnapshotPath(path))
		})
	}
}

func TestDefaultEtcdSnapshotPathSanitizesContextAndHostname(t *testing.T) {
	now := time.Date(2026, 8, 18, 23, 15, 0, 0, time.UTC)

	path := application.DefaultEtcdSnapshotPathForTest("prod/../evil", `cp-1"; rm -rf /`, now)

	assert.True(t, strings.HasPrefix(path, "etcd-"), path)
	assert.True(t, strings.HasSuffix(path, "-20260818T231500Z.db"), path)
	assert.NotContains(t, path, "/")
	assert.NotContains(t, path, "..")
}

func TestDefaultEtcdSnapshotPathLooksLikeBlueprintExample(t *testing.T) {
	now := time.Date(2026, 8, 18, 23, 15, 0, 0, time.UTC)

	path := application.DefaultEtcdSnapshotPathForTest("prod", "cp-1", now)

	assert.Equal(t, "etcd-prod-cp-1-20260818T231500Z.db", path)
}
