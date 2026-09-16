package talos

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryReaderListConvertsMemInfo(t *testing.T) {
	client := &fakeMemoryClient{response: &machineapi.MemoryResponse{Messages: []*machineapi.Memory{
		{Meminfo: &machineapi.MemInfo{
			Memtotal:     1000,
			Memfree:      100,
			Memavailable: 250,
			Buffers:      20,
			Cached:       30,
			Swaptotal:    40,
			Swapfree:     10,
			Dirty:        5,
			Slab:         6,
		}},
	}}}
	reader := newMemoryReader(client)

	snapshot, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	// MemInfo fields are kB (procfs raw), not bytes, so every field must be
	// normalized to bytes at the adapter boundary.
	const kib = 1024
	assert.Equal(t, uint64(1000*kib), snapshot.TotalBytes)
	assert.Equal(t, uint64(250*kib), snapshot.AvailableBytes)
	assert.Equal(t, uint64(750*kib), snapshot.UsedBytes)
	assert.Equal(t, uint64(100*kib), snapshot.FreeBytes)
	assert.Equal(t, uint64(20*kib), snapshot.BuffersBytes)
	assert.Equal(t, uint64(30*kib), snapshot.CachedBytes)
	assert.Equal(t, uint64(40*kib), snapshot.SwapTotalBytes)
	assert.Equal(t, uint64(10*kib), snapshot.SwapFreeBytes)
	assert.Equal(t, uint64(5*kib), snapshot.DirtyBytes)
	assert.Equal(t, uint64(6*kib), snapshot.SlabBytes)
	assert.Equal(t, "cp-1", client.node)
}

func TestMemoryReaderListErrorsOnClientFailure(t *testing.T) {
	client := &fakeMemoryClient{err: errors.New("unreachable")}
	reader := newMemoryReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}

func TestMemoryReaderListErrorsOnEmptyMessages(t *testing.T) {
	client := &fakeMemoryClient{response: &machineapi.MemoryResponse{}}
	reader := newMemoryReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}

func TestMemoryReaderListAllowsNilMeminfo(t *testing.T) {
	client := &fakeMemoryClient{response: &machineapi.MemoryResponse{Messages: []*machineapi.Memory{{}}}}
	reader := newMemoryReader(client)

	snapshot, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	assert.Equal(t, uint64(0), snapshot.TotalBytes)
}

type fakeMemoryClient struct {
	response *machineapi.MemoryResponse
	err      error

	node string
}

func (f *fakeMemoryClient) Memory(_ context.Context, node string) (*machineapi.MemoryResponse, error) {
	f.node = node
	return f.response, f.err
}
