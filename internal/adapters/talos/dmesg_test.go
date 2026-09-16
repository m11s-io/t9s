package talos

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDmesgReaderOpenUsesFollowAndTail(t *testing.T) {
	client := &fakeDmesgClient{stream: &fakeTalosDataStream{responses: []*common.Data{{Bytes: []byte("line one\nline two\n")}}}}
	reader := newDmesgReader(client)

	stream, err := reader.Open(context.Background(), domain.DmesgRequest{Node: "cp-1", Follow: true, Tail: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })

	assert.Equal(t, "cp-1", client.node)
	assert.True(t, client.follow)
	assert.True(t, client.tail)

	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"line one", "line two"}, batch.Lines)
	assert.False(t, batch.EOF)
}

func TestDmesgReaderBoundsBatchLinesAndLineLength(t *testing.T) {
	lines := make([]string, 250)
	for index := range lines {
		lines[index] = strings.Repeat("x", 600)
	}
	client := &fakeDmesgClient{stream: &fakeTalosDataStream{responses: []*common.Data{{Bytes: []byte(strings.Join(lines, "\n"))}}}}
	stream, err := newDmesgReader(client).Open(context.Background(), domain.DmesgRequest{Node: "cp-1", Follow: true, Tail: true})
	require.NoError(t, err)

	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.Len(t, batch.Lines, 200)
	for _, line := range batch.Lines {
		assert.LessOrEqual(t, len([]rune(line)), 512)
	}
}

func TestDmesgReaderSanitizesMetadataErrorsAndReportsEOF(t *testing.T) {
	client := &fakeDmesgClient{stream: &fakeTalosDataStream{
		responses: []*common.Data{{Metadata: &common.Metadata{Error: "token=top-secret stream rejected"}}},
		errors:    []error{nil, io.EOF},
	}}
	stream, err := newDmesgReader(client).Open(context.Background(), domain.DmesgRequest{Node: "cp-1", Follow: true, Tail: true})
	require.NoError(t, err)

	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "log stream error", batch.Err)
	assert.NotContains(t, batch.Err, "top-secret")

	batch, err = stream.Next(context.Background())
	require.NoError(t, err)
	assert.True(t, batch.EOF)
}

func TestDmesgReaderOpenReturnsErrorWhenClientFails(t *testing.T) {
	client := &fakeDmesgClient{err: errors.New("unreachable")}
	reader := newDmesgReader(client)

	_, err := reader.Open(context.Background(), domain.DmesgRequest{Node: "cp-1"})

	assert.Error(t, err)
}

type fakeDmesgClient struct {
	stream talosDataStream
	err    error

	node          string
	follow, tail  bool
	openCallCount int
}

func (f *fakeDmesgClient) Dmesg(_ context.Context, node string, follow, tail bool) (talosDataStream, error) {
	f.openCallCount++
	f.node, f.follow, f.tail = node, follow, tail
	return f.stream, f.err
}
