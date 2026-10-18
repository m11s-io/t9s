package talos

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceLogsOpenUsesTalosSystemContainerStream(t *testing.T) {
	client := &fakeLogClient{stream: &fakeTalosDataStream{responses: []*common.Data{{Bytes: []byte("line one\nline two\n")}}}}
	reader := newServiceLogReader(client)

	stream, err := reader.Open(context.Background(), domain.LogRequest{Node: "cp-1", Service: "etcd"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })

	assert.Equal(t, "cp-1", client.node)
	assert.Equal(t, constants.SystemContainerdNamespace, client.namespace)
	assert.Equal(t, common.ContainerDriver_CONTAINERD, client.driver)
	assert.Equal(t, "etcd", client.id)
	assert.True(t, client.follow)
	assert.Equal(t, int32(100), client.tailLines)

	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"line one", "line two"}, batch.Lines)
	assert.False(t, batch.EOF)
}

func TestServiceLogsSanitizesMetadataErrorsAndReportsEOF(t *testing.T) {
	client := &fakeLogClient{stream: &fakeTalosDataStream{
		responses: []*common.Data{{Metadata: &common.Metadata{Error: "token=top-secret stream rejected"}}},
		errors:    []error{nil, io.EOF},
	}}
	reader := newServiceLogReader(client)
	stream, err := reader.Open(context.Background(), domain.LogRequest{Node: "cp-1", Service: "etcd"})
	require.NoError(t, err)

	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "log stream error", batch.Err)
	assert.NotContains(t, batch.Err, "top-secret")

	batch, err = stream.Next(context.Background())
	require.NoError(t, err)
	assert.True(t, batch.EOF)
}

func TestServiceLogsBoundsBatchLinesAndLineLength(t *testing.T) {
	lines := make([]string, 250)
	for index := range lines {
		lines[index] = strings.Repeat("x", 600)
	}
	client := &fakeLogClient{stream: &fakeTalosDataStream{responses: []*common.Data{{Bytes: []byte(strings.Join(lines, "\n"))}}}}
	stream, err := newServiceLogReader(client).Open(context.Background(), domain.LogRequest{Node: "cp-1", Service: "etcd"})
	require.NoError(t, err)

	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.Len(t, batch.Lines, 200)
	for _, line := range batch.Lines {
		assert.LessOrEqual(t, len([]rune(line)), 512)
	}
}

type fakeLogClient struct {
	stream              talosDataStream
	node, namespace, id string
	driver              common.ContainerDriver
	follow              bool
	tailLines           int32
}

func (f *fakeLogClient) Logs(_ context.Context, node, namespace string, driver common.ContainerDriver, id string, follow bool, tailLines int32) (talosDataStream, error) {
	f.node, f.namespace, f.driver, f.id, f.follow, f.tailLines = node, namespace, driver, id, follow, tailLines
	return f.stream, nil
}

type fakeTalosDataStream struct {
	responses []*common.Data
	errors    []error
	index     int
}

func (f *fakeTalosDataStream) Recv() (*common.Data, error) {
	index := f.index
	f.index++
	var response *common.Data
	if index < len(f.responses) {
		response = f.responses[index]
	}
	if index < len(f.errors) {
		return response, f.errors[index]
	}
	return response, nil
}

func TestServiceLogsReassemblesChunksAndFlushesEOF(t *testing.T) {
	raw := []byte("hello 世界\nlast")
	source := &fakeTalosDataStream{responses: []*common.Data{{Bytes: raw[:8]}, {Bytes: raw[8:]}}, errors: []error{nil, nil, io.EOF}}
	stream := &serviceLogStream{stream: source, cancel: func() {}}
	batch, err := stream.Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"hello 世界"}, batch.Lines)
	batch, err = stream.Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"last"}, batch.Lines)
	assert.True(t, batch.EOF)
}

func TestServiceLogsDrainsLargeFramesWithoutDroppingLines(t *testing.T) {
	source := &fakeTalosDataStream{responses: []*common.Data{{Bytes: []byte(strings.Repeat("line\n", 451))}}, errors: []error{nil, io.EOF}}
	stream := &serviceLogStream{stream: source, cancel: func() {}}
	var count int
	for {
		batch, err := stream.Next(t.Context())
		require.NoError(t, err)
		require.LessOrEqual(t, len(batch.Lines), 200)
		count += len(batch.Lines)
		if batch.EOF {
			break
		}
	}
	assert.Equal(t, 451, count)
}

func TestServiceLogsBoundsPartialLineAndResumesAfterNewline(t *testing.T) {
	source := &fakeTalosDataStream{responses: []*common.Data{
		{Bytes: []byte(strings.Repeat("界", 10000))},
		{Metadata: &common.Metadata{Error: "private upstream detail"}},
		{Bytes: []byte("ignored suffix\nnext\n")},
	}}
	stream := &serviceLogStream{stream: source, cancel: func() {}}
	batch, err := stream.Next(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "log stream error", batch.Err)
	assert.LessOrEqual(t, len(stream.decoder.partial), maxServiceLogLineRunes*4)
	batch, err = stream.Next(t.Context())
	require.NoError(t, err)
	require.Len(t, batch.Lines, 2)
	assert.LessOrEqual(t, len([]rune(batch.Lines[0])), maxServiceLogLineRunes)
	assert.NotContains(t, batch.Lines[0], "�")
	assert.Equal(t, "next", batch.Lines[1])
}
