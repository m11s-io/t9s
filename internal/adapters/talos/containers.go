package talos

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

const maxImageBatch = 200

type imageListDataStream interface {
	Recv() (*machineapi.ImageListResponse, error)
}

type containerClient interface {
	Containers(context.Context, string, string, common.ContainerDriver) (*machineapi.ContainersResponse, error)
	Stats(context.Context, string, string, common.ContainerDriver) (*machineapi.StatsResponse, error)
}

type imageClient interface {
	ImageList(context.Context, string, common.ContainerdNamespace) (imageListDataStream, error)
}

type machineryContainerClient struct{ client *talosclient.Client }

func (c machineryContainerClient) Containers(ctx context.Context, node, namespace string, driver common.ContainerDriver) (*machineapi.ContainersResponse, error) {
	return c.client.Containers(talosclient.WithNode(ctx, node), namespace, driver)
}

func (c machineryContainerClient) Stats(ctx context.Context, node, namespace string, driver common.ContainerDriver) (*machineapi.StatsResponse, error) {
	return c.client.Stats(talosclient.WithNode(ctx, node), namespace, driver)
}

type machineryImageClient struct{ client *talosclient.Client }

func (c machineryImageClient) ImageList(ctx context.Context, node string, namespace common.ContainerdNamespace) (imageListDataStream, error) {
	return c.client.ImageList(talosclient.WithNode(ctx, node), namespace)
}

type containerReader struct {
	containers containerClient
	images     imageClient
}

var _ ports.ContainerReader = (*containerReader)(nil)

func newContainerReader(containers containerClient, images imageClient) *containerReader {
	return &containerReader{containers: containers, images: images}
}

func (r *containerReader) List(ctx context.Context, request domain.ContainerRequest) (domain.ContainerSet, error) {
	runtimeNamespace, driver, _, err := containerScope(request.Namespace)
	if err != nil {
		return domain.ContainerSet{}, err
	}
	if request.Node == "" {
		return domain.ContainerSet{}, fmt.Errorf("containers require a node")
	}
	set := domain.ContainerSet{Node: request.Node, Namespace: request.Namespace, Containers: make([]domain.ContainerSnapshot, 0)}
	response, err := r.containers.Containers(ctx, request.Node, runtimeNamespace, driver)
	if err != nil {
		return set, fmt.Errorf("list containers: %w", err)
	}
	if response != nil {
		for _, group := range response.GetMessages() {
			if group == nil {
				continue
			}
			appendContainerMetadataProblem(&set, group.GetMetadata())
			for _, container := range group.GetContainers() {
				if container == nil {
					continue
				}
				set.Containers = append(set.Containers, domain.ContainerSnapshot{
					Namespace: container.GetNamespace(), ID: container.GetId(), Name: container.GetName(),
					PodID: container.GetPodId(), Image: container.GetImage(), PID: container.GetPid(), Status: container.GetStatus(),
				})
			}
		}
	}
	stats, statsErr := r.containers.Stats(ctx, request.Node, runtimeNamespace, driver)
	if statsErr != nil {
		appendContainerProblem(&set, "container statistics unavailable: "+statsErr.Error())
		return set, fmt.Errorf("read container statistics: %w", statsErr)
	}
	indexed := make(map[containerKey]*machineapi.Stat)
	if stats != nil {
		for _, group := range stats.GetMessages() {
			if group == nil {
				continue
			}
			appendContainerMetadataProblem(&set, group.GetMetadata())
			for _, stat := range group.GetStats() {
				if stat == nil {
					continue
				}
				indexed[containerKey{namespace: stat.GetNamespace(), id: stat.GetId()}] = stat
			}
		}
	}
	for i := range set.Containers {
		container := &set.Containers[i]
		stat := indexed[containerKey{namespace: container.Namespace, id: container.ID}]
		if stat == nil {
			continue
		}
		container.MemoryBytes = stat.GetMemoryUsage()
		container.MemoryKnown = true
		container.CPUTime = time.Duration(stat.GetCpuUsage())
		container.CPUTimeKnown = true
	}
	return set, nil
}

type containerKey struct{ namespace, id string }

func appendContainerMetadataProblem(set *domain.ContainerSet, metadata *common.Metadata) {
	if metadata == nil || metadata.GetError() == "" {
		return
	}
	appendContainerProblem(set, metadata.GetError())
}

func appendContainerProblem(set *domain.ContainerSet, problem string) {
	if len(set.Problems) < 100 {
		set.Problems = append(set.Problems, problem)
	}
}

func (r *containerReader) OpenImages(ctx context.Context, request domain.ImageRequest) (ports.ImageStream, error) {
	_, _, namespace, err := containerScope(request.Namespace)
	if err != nil {
		return nil, err
	}
	if request.Node == "" {
		return nil, fmt.Errorf("images require a node")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.images.ImageList(streamCtx, request.Node, namespace)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open image list: %w", err)
	}
	return &imageStream{stream: stream, ctx: streamCtx, cancel: cancel}, nil
}

func containerScope(namespace domain.ContainerNamespace) (string, common.ContainerDriver, common.ContainerdNamespace, error) {
	switch namespace {
	case domain.ContainerNamespaceSystem:
		return "system", common.ContainerDriver_CONTAINERD, common.ContainerdNamespace_NS_SYSTEM, nil
	case domain.ContainerNamespaceCRI:
		return "k8s.io", common.ContainerDriver_CRI, common.ContainerdNamespace_NS_CRI, nil
	case domain.ContainerNamespaceTalosContainers:
		return "taloscontainers", common.ContainerDriver_CONTAINERD, common.ContainerdNamespace_NS_TALOSCONTAINERS, nil
	default:
		return "", 0, 0, fmt.Errorf("unsupported container namespace %q", namespace)
	}
}

type imageStream struct {
	stream imageListDataStream
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (s *imageStream) Next(ctx context.Context) (domain.ImageBatch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	batch := domain.ImageBatch{Images: make([]domain.ImageSnapshot, 0, maxImageBatch)}
	for len(batch.Images) < maxImageBatch {
		response, err := s.stream.Recv()
		if err == io.EOF {
			batch.EOF = true
			return batch, nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return batch, ctx.Err()
			}
			if s.ctx.Err() != nil {
				return batch, s.ctx.Err()
			}
			return batch, fmt.Errorf("receive images: %w", err)
		}
		if response == nil {
			continue
		}
		if metadataError := response.GetMetadata().GetError(); metadataError != "" {
			batch.Err = metadataError
		}
		size := response.GetSize()
		if size < 0 {
			batch.Err = "image reported a negative size"
			size = 0
		}
		batch.Images = append(batch.Images, domain.ImageSnapshot{Reference: response.GetName(), Digest: response.GetDigest(), SizeBytes: uint64(size)})
	}
	return batch, nil
}

func (s *imageStream) Close() error {
	s.once.Do(s.cancel)
	return nil
}
