package application_test

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestImagesStopAt5000Rows(t *testing.T) {
	images := make([]domain.ImageSnapshot, 5100)
	for index := range images {
		images[index] = domain.ImageSnapshot{Reference: "example/image"}
	}
	upstream := &fakeImageStream{batch: domain.ImageBatch{Images: images}}
	reader := &fakeContainerReader{stream: upstream}
	session := openReadTestSession(t, &testkit.FakeSession{ContainerReader: reader})

	stream, err := session.OpenImages(context.Background(), domain.ImageRequest{Node: "worker-1", Namespace: domain.ContainerNamespaceCRI})
	require.NoError(t, err)
	batch, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.Len(t, batch.Images, 5000)
	require.True(t, batch.EOF)
	require.True(t, batch.Truncated)
	require.True(t, upstream.closed)
}

func TestServiceDetailUsesCapturedIdentity(t *testing.T) {
	var node, service string
	reader := &testkit.FakeServiceReader{DetailFunc: func(_ context.Context, requestedNode, requestedService string) (domain.ServiceDetail, error) {
		node, service = requestedNode, requestedService
		return domain.ServiceDetail{Node: requestedNode, Service: requestedService, State: "Running"}, nil
	}}
	session := openReadTestSession(t, &testkit.FakeSession{ServiceReader: reader})

	detail, err := session.ServiceDetail(context.Background(), "node-a", "kubelet")
	require.NoError(t, err)
	require.Equal(t, "node-a", node)
	require.Equal(t, "kubelet", service)
	require.Equal(t, "Running", detail.State)
}

type fakeContainerReader struct {
	stream ports.ImageStream
}

func (r *fakeContainerReader) List(context.Context, domain.ContainerRequest) (domain.ContainerSet, error) {
	return domain.ContainerSet{}, nil
}

func (r *fakeContainerReader) OpenImages(context.Context, domain.ImageRequest) (ports.ImageStream, error) {
	return r.stream, nil
}

type fakeImageStream struct {
	batch  domain.ImageBatch
	closed bool
}

func (s *fakeImageStream) Next(context.Context) (domain.ImageBatch, error) { return s.batch, nil }
func (s *fakeImageStream) Close() error {
	s.closed = true
	return nil
}
