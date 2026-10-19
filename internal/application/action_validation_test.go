package application

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildResetPreviewExcludesReadOnlyDisks(t *testing.T) {
	preview := BuildResetPreview("cp-1", domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}, {DeviceName: "/dev/sdb"}, {DeviceName: "/dev/sr0", ReadOnly: true}}})
	require.True(t, preview.Known)
	assert.Equal(t, []string{"/dev/sdb"}, preview.UserDisks)
}

func TestBuildResetPreviewClassifiesSystemAndUserDisks(t *testing.T) {
	preview := BuildResetPreview("cp-1", domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "sda", SystemDisk: true}, {DeviceName: "sdb"}, {DeviceName: "sdc"}}})
	assert.True(t, preview.Known)
	assert.Equal(t, "cp-1", preview.Node)
	assert.Equal(t, "sda", preview.SystemDisk)
	assert.Equal(t, []string{"sdb", "sdc"}, preview.UserDisks)
	assert.False(t, BuildResetPreview("cp-1", domain.DiskSet{}).Known)
}

func TestResetConfirmationTokenShapes(t *testing.T) {
	assert.Equal(t, "cp-1", ResetConfirmationToken([]string{"cp-1"}))
	assert.Equal(t, "wipe 2 nodes", ResetConfirmationToken([]string{"cp-1", "cp-2"}))
	assert.Empty(t, ResetConfirmationToken(nil))
}

func TestValidateDeviceWipeConfirmationNormalizesDevPrefix(t *testing.T) {
	assert.NoError(t, ValidateDeviceWipeConfirmation("/dev/sdb", "sdb"))
	assert.NoError(t, ValidateDeviceWipeConfirmation("/dev/sdb", "/dev/sdb"))
	assert.NoError(t, ValidateDeviceWipeConfirmation("/dev/sdb", "  /dev/sdb  "))
	assert.Error(t, ValidateDeviceWipeConfirmation("/dev/sdb", "sdc"))
	assert.Error(t, ValidateDeviceWipeConfirmation("", "sdb"))
}

func TestUpgradeMinorWarning(t *testing.T) {
	assert.Equal(t, "skips intermediate Talos minor releases", UpgradeMinorWarning("v1.13.3", "factory.talos.dev/metal-installer/id:v1.15.0"))
	assert.Empty(t, UpgradeMinorWarning("v1.13.3", "factory.talos.dev/metal-installer/id:v1.14.0"))
}
