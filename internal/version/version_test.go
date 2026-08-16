package version_test

import (
	"testing"

	"github.com/m11s-io/t9s/internal/version"
	"github.com/stretchr/testify/assert"
)

func TestDefaultsAreUnsetPlaceholders(t *testing.T) {
	assert.Equal(t, "dev", version.Version)
	assert.Equal(t, "unknown", version.Commit)
	assert.Equal(t, "unknown", version.Date)
}

func TestStringFormatsAllThreeFields(t *testing.T) {
	originalVersion, originalCommit, originalDate := version.Version, version.Commit, version.Date
	defer func() {
		version.Version, version.Commit, version.Date = originalVersion, originalCommit, originalDate
	}()

	version.Version = "0.1.0"
	version.Commit = "abc1234"
	version.Date = "2026-08-16"

	assert.Equal(t, "0.1.0 (abc1234, 2026-08-16)", version.String())
}
