package application_test

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskUsageRejectsInvalidPathBeforeRPC(t *testing.T) {
	reader := &fakeDiskUsageReader{}
	session := openReadTestSession(t, &testkit.FakeSession{DiskUsageReader: reader})

	for _, path := range []string{"relative/path", "/mnt/data/\nsecret"} {
		stream, err := session.OpenDiskUsage(context.Background(), domain.DiskUsageRequest{Node: "worker-1", Path: path})
		assert.Nil(t, stream)
		assert.Error(t, err)
	}
	assert.Zero(t, reader.openCalls)
}

func TestDiskUsageStopsAt5000Rows(t *testing.T) {
	rows := make([]domain.DiskUsageEntry, 5100)
	for index := range rows {
		rows[index] = domain.DiskUsageEntry{Path: "/mnt/data/file"}
	}
	upstream := &fakeDiskUsageStream{batch: domain.DiskUsageBatch{Rows: rows}}
	session := openReadTestSession(t, &testkit.FakeSession{DiskUsageReader: &fakeDiskUsageReader{stream: upstream}})

	stream, err := session.OpenDiskUsage(context.Background(), domain.DiskUsageRequest{Node: "worker-1", Path: "/mnt/data/"})
	require.NoError(t, err)
	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Len(t, batch.Rows, 5000)
	assert.True(t, batch.EOF)
	assert.True(t, batch.Truncated)
	assert.True(t, upstream.closed)
}

func TestDiskUsageSessionCancellationClosesStream(t *testing.T) {
	upstream := &fakeDiskUsageStream{started: make(chan struct{}), waitForContext: true}
	session := openReadTestSession(t, &testkit.FakeSession{DiskUsageReader: &fakeDiskUsageReader{stream: upstream}})
	openCtx, cancelOpen := context.WithCancel(context.Background())
	stream, err := session.OpenDiskUsage(openCtx, domain.DiskUsageRequest{Node: "worker-1", Path: "/var"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := stream.Next(context.Background())
		done <- err
	}()
	<-upstream.started
	cancelOpen()
	require.ErrorIs(t, <-done, context.Canceled)
	assert.True(t, upstream.closed)
}

type fakeDiskUsageReader struct {
	stream    ports.DiskUsageStream
	openCalls int
}

func (r *fakeDiskUsageReader) Open(context.Context, domain.DiskUsageRequest) (ports.DiskUsageStream, error) {
	r.openCalls++
	return r.stream, nil
}

type fakeDiskUsageStream struct {
	batch          domain.DiskUsageBatch
	started        chan struct{}
	waitForContext bool
	closed         bool
}

func (s *fakeDiskUsageStream) Next(ctx context.Context) (domain.DiskUsageBatch, error) {
	if s.waitForContext {
		close(s.started)
		<-ctx.Done()
		return domain.DiskUsageBatch{}, ctx.Err()
	}
	return s.batch, nil
}

func (s *fakeDiskUsageStream) Close() error {
	s.closed = true
	return nil
}
