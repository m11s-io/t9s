package talos

import (
	"context"
	"errors"
	"testing"

	storageapi "github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDiskClient struct {
	response *storageapi.DisksResponse
	err      error
}

func (c *fakeDiskClient) Disks(context.Context, string) (*storageapi.DisksResponse, error) {
	return c.response, c.err
}

func TestDiskReaderListConvertsAndSortsByDeviceName(t *testing.T) {
	client := &fakeDiskClient{response: &storageapi.DisksResponse{
		Messages: []*storageapi.Disks{{Disks: []*storageapi.Disk{
			{DeviceName: "sdb", Size: 2000, Type: storageapi.Disk_HDD},
			{DeviceName: "sda", Size: 1000, Type: storageapi.Disk_SSD, SystemDisk: true},
		}}},
	}}
	reader := newDiskReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	require.Len(t, set.Disks, 2)
	assert.Equal(t, "sda", set.Disks[0].DeviceName)
	assert.Equal(t, "ssd", set.Disks[0].Type)
	assert.True(t, set.Disks[0].SystemDisk)
	assert.Equal(t, uint64(1000), set.Disks[0].SizeBytes)
	assert.Equal(t, "sdb", set.Disks[1].DeviceName)
	assert.Equal(t, "hdd", set.Disks[1].Type)
}

func TestDiskReaderListReturnsErrorWhenClientFails(t *testing.T) {
	client := &fakeDiskClient{err: errors.New("unreachable")}
	reader := newDiskReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}

func TestDiskReaderListReturnsErrorWhenResponseHasNoMessages(t *testing.T) {
	client := &fakeDiskClient{response: &storageapi.DisksResponse{}}
	reader := newDiskReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}
