package talos

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMountReaderListConvertsAndSorts(t *testing.T) {
	client := &fakeMountClient{response: &machineapi.MountsResponse{Messages: []*machineapi.Mounts{
		{Stats: []*machineapi.MountStat{
			{Filesystem: "tmpfs", MountedOn: "/var", Size: 2000, Available: 500},
			{Filesystem: "/dev/sda1", MountedOn: "/", Size: 1000, Available: 400},
		}},
	}}}
	reader := newMountReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	require.Len(t, set.Mounts, 2)

	assert.Equal(t, "/dev/sda1", set.Mounts[0].Filesystem)
	assert.Equal(t, "/", set.Mounts[0].MountedOn)
	assert.Equal(t, uint64(1000), set.Mounts[0].SizeBytes)
	assert.Equal(t, uint64(600), set.Mounts[0].UsedBytes)
	assert.Equal(t, uint64(400), set.Mounts[0].AvailableBytes)
	assert.InDelta(t, 60.0, set.Mounts[0].UsedPercent, 0.01)

	assert.Equal(t, "/var", set.Mounts[1].MountedOn)
	assert.Equal(t, "cp-1", client.node, "the mounts RPC must be scoped to the requested node")
}

func TestMountReaderListComputesZeroPercentWhenSizeIsZero(t *testing.T) {
	client := &fakeMountClient{response: &machineapi.MountsResponse{Messages: []*machineapi.Mounts{
		{Stats: []*machineapi.MountStat{
			{Filesystem: "proc", MountedOn: "/proc", Size: 0, Available: 0},
		}},
	}}}
	reader := newMountReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	require.Len(t, set.Mounts, 1)
	assert.Equal(t, uint64(0), set.Mounts[0].UsedBytes)
	assert.Equal(t, 0.0, set.Mounts[0].UsedPercent)
}

func TestMountReaderListErrorsOnClientFailure(t *testing.T) {
	client := &fakeMountClient{err: errors.New("unreachable")}
	reader := newMountReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}

func TestMountReaderListErrorsOnEmptyMessages(t *testing.T) {
	client := &fakeMountClient{response: &machineapi.MountsResponse{}}
	reader := newMountReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
	assert.Empty(t, set.Mounts)
}

type fakeMountClient struct {
	response *machineapi.MountsResponse
	err      error

	node string
}

func (f *fakeMountClient) Mounts(_ context.Context, node string) (*machineapi.MountsResponse, error) {
	f.node = node
	return f.response, f.err
}
