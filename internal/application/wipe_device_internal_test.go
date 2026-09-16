package application

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
)

func TestDeviceWipeBlockReason(t *testing.T) {
	disks := DisksState{Status: Ready, Node: "cp-1", Value: domain.DiskSet{Disks: []domain.DiskSnapshot{
		{DeviceName: "/dev/sda", SystemDisk: true},
		{DeviceName: "/dev/sdb"},
		{DeviceName: "/dev/sr0", ReadOnly: true},
	}}}

	assert.Empty(t, DeviceWipeBlockReason(disks, ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb"}))
	assert.Contains(t, DeviceWipeBlockReason(disks, ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sda"}), "system disk")
	assert.Contains(t, DeviceWipeBlockReason(disks, ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sr0"}), "read-only")
	assert.Contains(t, DeviceWipeBlockReason(disks, ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdz"}), "not in the current inventory")
	assert.Contains(t, DeviceWipeBlockReason(DisksState{}, ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb"}), "inventory")
	// A different node's loaded inventory must not authorize this node's wipe.
	assert.Contains(t, DeviceWipeBlockReason(disks, ports.DeviceWipeOptions{Node: "cp-2", Device: "/dev/sdb"}), "inventory")
}

func TestValidateDeviceWipeConfirmationNormalizesDevPrefix(t *testing.T) {
	assert.NoError(t, ValidateDeviceWipeConfirmation("/dev/sdb", "sdb"))
	assert.NoError(t, ValidateDeviceWipeConfirmation("/dev/sdb", "/dev/sdb"))
	assert.NoError(t, ValidateDeviceWipeConfirmation("/dev/sdb", "  /dev/sdb  "))
	assert.Error(t, ValidateDeviceWipeConfirmation("/dev/sdb", "sdc"))
	assert.Error(t, ValidateDeviceWipeConfirmation("", "sdb"))
}
