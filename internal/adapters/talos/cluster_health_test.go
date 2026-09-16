package talos

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClusterHealthReaderOpenBuildsClusterInfoAndStreamsProgress(t *testing.T) {
	client := &fakeClusterHealthClient{stream: &fakeClusterHealthStream{
		responses: []*clusterapi.HealthCheckProgress{{Message: "waiting for etcd"}},
	}}
	reader := newClusterHealthReader(client)

	stream, err := reader.Open(context.Background(), domain.ClusterHealthRequest{
		ControlPlaneNodes: []string{"10.0.0.1", "10.0.0.2"},
		WorkerNodes:       []string{"10.0.0.3"},
		WaitTimeout:       5 * time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })

	require.NotNil(t, client.info)
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, client.info.GetControlPlaneNodes())
	assert.Equal(t, []string{"10.0.0.3"}, client.info.GetWorkerNodes())
	assert.Equal(t, 5*time.Minute, client.waitTimeout)

	progress, err := stream.Next(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "waiting for etcd", progress.Message)
	assert.False(t, progress.EOF)
}

func TestClusterHealthStreamMapsEOFAndRecvError(t *testing.T) {
	t.Run("io.EOF is a clean completion", func(t *testing.T) {
		client := &fakeClusterHealthClient{stream: &fakeClusterHealthStream{errors: []error{io.EOF}}}
		stream, err := newClusterHealthReader(client).Open(context.Background(), domain.ClusterHealthRequest{})
		require.NoError(t, err)

		progress, err := stream.Next(context.Background())
		require.NoError(t, err)
		assert.True(t, progress.EOF)
	})

	t.Run("Canceled is a clean completion", func(t *testing.T) {
		client := &fakeClusterHealthClient{stream: &fakeClusterHealthStream{errors: []error{status.Error(codes.Canceled, "context canceled")}}}
		stream, err := newClusterHealthReader(client).Open(context.Background(), domain.ClusterHealthRequest{})
		require.NoError(t, err)

		progress, err := stream.Next(context.Background())
		require.NoError(t, err)
		assert.True(t, progress.EOF)
	})

	t.Run("a non-cancel gRPC error is returned to the caller", func(t *testing.T) {
		client := &fakeClusterHealthClient{stream: &fakeClusterHealthStream{errors: []error{status.Error(codes.DeadlineExceeded, "timed out")}}}
		stream, err := newClusterHealthReader(client).Open(context.Background(), domain.ClusterHealthRequest{})
		require.NoError(t, err)

		// The adapter forwards the raw error; the reducer is what replaces it
		// with a generic, non-gRPC user-facing message.
		_, err = stream.Next(context.Background())
		require.Error(t, err)
	})

	t.Run("metadata error becomes a generic progress error", func(t *testing.T) {
		client := &fakeClusterHealthClient{stream: &fakeClusterHealthStream{
			responses: []*clusterapi.HealthCheckProgress{{Message: "ready", Metadata: &common.Metadata{Error: "token=top-secret rejected"}}},
		}}
		stream, err := newClusterHealthReader(client).Open(context.Background(), domain.ClusterHealthRequest{})
		require.NoError(t, err)

		progress, err := stream.Next(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "cluster health check error", progress.Err)
		assert.NotContains(t, progress.Err, "top-secret")
	})
}

func TestClusterHealthReaderOpenReturnsErrorWhenClientFails(t *testing.T) {
	client := &fakeClusterHealthClient{err: errors.New("unreachable")}

	_, err := newClusterHealthReader(client).Open(context.Background(), domain.ClusterHealthRequest{})

	assert.Error(t, err)
}

type fakeClusterHealthClient struct {
	stream      clusterHealthStream
	err         error
	info        *clusterapi.ClusterInfo
	waitTimeout time.Duration
}

func (f *fakeClusterHealthClient) ClusterHealthCheck(_ context.Context, waitTimeout time.Duration, info *clusterapi.ClusterInfo) (clusterHealthStream, error) {
	f.info = info
	f.waitTimeout = waitTimeout
	return f.stream, f.err
}

type fakeClusterHealthStream struct {
	responses []*clusterapi.HealthCheckProgress
	errors    []error

	index int
}

func (s *fakeClusterHealthStream) Recv() (*clusterapi.HealthCheckProgress, error) {
	if s.index < len(s.responses) {
		response := s.responses[s.index]
		s.index++
		return response, nil
	}
	if s.index < len(s.errors) {
		err := s.errors[s.index]
		s.index++
		return nil, err
	}
	return nil, io.EOF
}
