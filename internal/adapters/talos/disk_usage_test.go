package talos

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskUsageUsesSelectedMountAndDepthOne(t *testing.T) {
	client := &fakeDiskUsageClient{stream: &fakeDiskUsageDataStream{items: []*machineapi.DiskUsageInfo{{Name: "/var/lib/a", RelativeName: "a", Size: 42}}}}
	reader := newDiskUsageReader(client)

	stream, err := reader.Open(t.Context(), domain.DiskUsageRequest{Node: "worker-2", Path: "/var/lib/containers/"})
	require.NoError(t, err)
	defer stream.Close()

	batch, err := stream.Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "worker-2", client.node)
	require.NotNil(t, client.request)
	assert.Equal(t, int32(1), client.request.GetRecursionDepth())
	assert.False(t, client.request.GetAll())
	assert.Equal(t, []string{"/var/lib/containers"}, client.request.GetPaths())
	assert.Equal(t, []domain.DiskUsageEntry{{Path: "/var/lib/a", SizeBytes: 42}}, batch.Rows)
}

func TestDiskUsagePreservesRowsOnPermissionError(t *testing.T) {
	client := &fakeDiskUsageClient{stream: &fakeDiskUsageDataStream{items: []*machineapi.DiskUsageInfo{
		{Name: "/mnt/data/visible", RelativeName: "visible", Size: 128},
		{Name: "/mnt/data/private", RelativeName: "private", Error: "permission denied"},
	}}}
	reader := newDiskUsageReader(client)
	stream, err := reader.Open(t.Context(), domain.DiskUsageRequest{Node: "worker-1", Path: "/mnt/data"})
	require.NoError(t, err)
	defer stream.Close()

	batch, err := stream.Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []domain.DiskUsageEntry{
		{Path: "/mnt/data/visible", SizeBytes: 128},
		{Path: "/mnt/data/private", Error: "permission denied"},
	}, batch.Rows)
	assert.True(t, batch.EOF)
	assert.Empty(t, batch.Err)
}

func TestDiskUsageCancellationUnblocksReceive(t *testing.T) {
	client := &fakeDiskUsageClient{block: true}
	reader := newDiskUsageReader(client)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := reader.Open(ctx, domain.DiskUsageRequest{Node: "worker-1", Path: "/var"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := stream.Next(context.Background())
		done <- err
	}()
	<-client.started
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("disk usage receive remained blocked after cancellation")
	}
	require.NoError(t, stream.Close())
}

type fakeDiskUsageClient struct {
	node    string
	request *machineapi.DiskUsageRequest
	stream  *fakeDiskUsageDataStream
	block   bool
	started chan struct{}
}

func (f *fakeDiskUsageClient) DiskUsage(ctx context.Context, node string, request *machineapi.DiskUsageRequest) (diskUsageDataStream, error) {
	f.node, f.request = node, request
	if f.block {
		f.started = make(chan struct{})
		return &fakeDiskUsageDataStream{block: true, ctx: ctx, started: f.started}, nil
	}
	return f.stream, nil
}

type fakeDiskUsageDataStream struct {
	items   []*machineapi.DiskUsageInfo
	index   int
	block   bool
	ctx     context.Context
	started chan struct{}
}

func (f *fakeDiskUsageDataStream) Recv() (*machineapi.DiskUsageInfo, error) {
	if f.index < len(f.items) {
		item := f.items[f.index]
		f.index++
		return item, nil
	}
	if f.block {
		if f.started != nil {
			close(f.started)
			f.started = nil
		}
		<-f.ctx.Done()
		return nil, f.ctx.Err()
	}
	return nil, io.EOF
}
