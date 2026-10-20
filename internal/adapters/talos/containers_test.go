package talos

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainersNamespaceDriverMappingAndScopedStatsJoin(t *testing.T) {
	cases := []struct {
		name             domain.ContainerNamespace
		runtimeNamespace string
		driver           common.ContainerDriver
	}{
		{name: domain.ContainerNamespaceSystem, runtimeNamespace: "system", driver: common.ContainerDriver_CONTAINERD},
		{name: domain.ContainerNamespaceCRI, runtimeNamespace: "k8s.io", driver: common.ContainerDriver_CRI},
		{name: domain.ContainerNamespaceTalosContainers, runtimeNamespace: "taloscontainers", driver: common.ContainerDriver_CONTAINERD},
	}
	for _, test := range cases {
		t.Run(string(test.name), func(t *testing.T) {
			client := &fakeContainerClient{
				containers: &machineapi.ContainersResponse{Messages: []*machineapi.Container{{Containers: []*machineapi.ContainerInfo{{
					Namespace: test.runtimeNamespace, Id: "same-id", Name: "api", Image: "example/api:v1", Pid: 12, Status: "running", PodId: "pod-a",
				}}}}},
				stats: &machineapi.StatsResponse{Messages: []*machineapi.Stats{{Stats: []*machineapi.Stat{
					{Namespace: "another-namespace", Id: "same-id", MemoryUsage: 999, CpuUsage: 999},
					{Namespace: test.runtimeNamespace, Id: "same-id", MemoryUsage: 64, CpuUsage: uint64(time.Second)},
				}}}},
			}
			reader := newContainerReader(client, &fakeImageClient{})

			set, err := reader.List(t.Context(), domain.ContainerRequest{Node: "worker-2", Namespace: test.name})
			require.NoError(t, err)
			require.Len(t, set.Containers, 1)
			assert.Equal(t, "worker-2", client.containerNode)
			assert.Equal(t, "worker-2", client.statsNode)
			assert.Equal(t, test.runtimeNamespace, client.containerNamespace)
			assert.Equal(t, test.runtimeNamespace, client.statsNamespace)
			assert.Equal(t, test.driver, client.containerDriver)
			assert.Equal(t, test.driver, client.statsDriver)
			assert.Equal(t, "pod-a", set.Containers[0].PodID)
			assert.True(t, set.Containers[0].MemoryKnown)
			assert.Equal(t, uint64(64), set.Containers[0].MemoryBytes)
			assert.True(t, set.Containers[0].CPUTimeKnown)
			assert.Equal(t, time.Second, set.Containers[0].CPUTime)
		})
	}
}

func TestContainersPartialStatsFailurePreservesRows(t *testing.T) {
	readErr := errors.New("stats permission denied")
	client := &fakeContainerClient{containers: &machineapi.ContainersResponse{Messages: []*machineapi.Container{{Containers: []*machineapi.ContainerInfo{{Namespace: "system", Id: "runtime-1", Name: "container"}}}}}, statsErr: readErr}
	reader := newContainerReader(client, &fakeImageClient{})

	set, err := reader.List(t.Context(), domain.ContainerRequest{Node: "worker-1", Namespace: domain.ContainerNamespaceSystem})
	require.ErrorIs(t, err, readErr)
	require.Len(t, set.Containers, 1)
	assert.False(t, set.Containers[0].MemoryKnown)
	assert.False(t, set.Containers[0].CPUTimeKnown)
	assert.NotEmpty(t, set.Problems)
}

func TestImageListUsesSelectedNamespace(t *testing.T) {
	cases := []struct {
		name      domain.ContainerNamespace
		namespace common.ContainerdNamespace
	}{
		{name: domain.ContainerNamespaceSystem, namespace: common.ContainerdNamespace_NS_SYSTEM},
		{name: domain.ContainerNamespaceCRI, namespace: common.ContainerdNamespace_NS_CRI},
		{name: domain.ContainerNamespaceTalosContainers, namespace: common.ContainerdNamespace_NS_TALOSCONTAINERS},
	}
	for _, test := range cases {
		t.Run(string(test.name), func(t *testing.T) {
			client := &fakeImageClient{stream: &fakeImageListStream{items: []*machineapi.ImageListResponse{{Name: "example/api:v1", Digest: "sha256:abc", Size: 512}}}}
			reader := newContainerReader(&fakeContainerClient{}, client)
			stream, err := reader.OpenImages(t.Context(), domain.ImageRequest{Node: "worker-2", Namespace: test.name})
			require.NoError(t, err)
			defer stream.Close()
			batch, err := stream.Next(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "worker-2", client.node)
			assert.Equal(t, test.namespace, client.namespace)
			assert.Equal(t, []domain.ImageSnapshot{{Reference: "example/api:v1", Digest: "sha256:abc", SizeBytes: 512}}, batch.Images)
			assert.True(t, batch.EOF)
		})
	}
}

func TestImageListPreservesRowsOnPartialStreamError(t *testing.T) {
	client := &fakeImageClient{stream: &fakeImageListStream{items: []*machineapi.ImageListResponse{{Name: "example/first:v1", Size: 100}}, err: errors.New("stream ended")}}
	reader := newContainerReader(&fakeContainerClient{}, client)
	stream, err := reader.OpenImages(t.Context(), domain.ImageRequest{Node: "worker-1", Namespace: domain.ContainerNamespaceCRI})
	require.NoError(t, err)
	defer stream.Close()

	batch, err := stream.Next(t.Context())
	require.ErrorContains(t, err, "stream ended")
	assert.Equal(t, []domain.ImageSnapshot{{Reference: "example/first:v1", SizeBytes: 100}}, batch.Images)
}

type fakeContainerClient struct {
	containers         *machineapi.ContainersResponse
	containersErr      error
	stats              *machineapi.StatsResponse
	statsErr           error
	containerNode      string
	containerNamespace string
	containerDriver    common.ContainerDriver
	statsNode          string
	statsNamespace     string
	statsDriver        common.ContainerDriver
}

func (f *fakeContainerClient) Containers(_ context.Context, node, namespace string, driver common.ContainerDriver) (*machineapi.ContainersResponse, error) {
	f.containerNode, f.containerNamespace, f.containerDriver = node, namespace, driver
	return f.containers, f.containersErr
}

func (f *fakeContainerClient) Stats(_ context.Context, node, namespace string, driver common.ContainerDriver) (*machineapi.StatsResponse, error) {
	f.statsNode, f.statsNamespace, f.statsDriver = node, namespace, driver
	return f.stats, f.statsErr
}

type fakeImageClient struct {
	stream    imageListDataStream
	node      string
	namespace common.ContainerdNamespace
}

func (f *fakeImageClient) ImageList(_ context.Context, node string, namespace common.ContainerdNamespace) (imageListDataStream, error) {
	f.node, f.namespace = node, namespace
	return f.stream, nil
}

type fakeImageListStream struct {
	items []*machineapi.ImageListResponse
	index int
	err   error
}

func (f *fakeImageListStream) Recv() (*machineapi.ImageListResponse, error) {
	if f.index < len(f.items) {
		item := f.items[f.index]
		f.index++
		return item, nil
	}
	if f.err != nil {
		return nil, f.err
	}
	return nil, io.EOF
}
