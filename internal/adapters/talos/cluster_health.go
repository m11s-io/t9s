package talos

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc/codes"
)

// maxHealthCheckLineRunes bounds a single progress line so a misbehaving
// server cannot grow the transcript line unbounded.
const maxHealthCheckLineRunes = 512

type clusterHealthStream interface {
	Recv() (*clusterapi.HealthCheckProgress, error)
}

type clusterHealthClient interface {
	ClusterHealthCheck(ctx context.Context, waitTimeout time.Duration, info *clusterapi.ClusterInfo) (clusterHealthStream, error)
}

type machineryClusterHealthClient struct{ client *talosclient.Client }

func (c machineryClusterHealthClient) ClusterHealthCheck(ctx context.Context, waitTimeout time.Duration, info *clusterapi.ClusterInfo) (clusterHealthStream, error) {
	return c.client.ClusterHealthCheck(ctx, waitTimeout, info)
}

type clusterHealthReader struct{ client clusterHealthClient }

func newClusterHealthReader(client clusterHealthClient) ports.ClusterHealthReader {
	return &clusterHealthReader{client: client}
}

func (r *clusterHealthReader) Open(ctx context.Context, request domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.client.ClusterHealthCheck(streamCtx, request.WaitTimeout, &clusterapi.ClusterInfo{
		ControlPlaneNodes: request.ControlPlaneNodes,
		WorkerNodes:       request.WorkerNodes,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &clusterHealthStreamImpl{stream: stream, cancel: cancel}, nil
}

type clusterHealthStreamImpl struct {
	stream clusterHealthStream
	cancel context.CancelFunc
	once   sync.Once
}

func (s *clusterHealthStreamImpl) Next(ctx context.Context) (domain.ClusterHealthProgress, error) {
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	progress, err := s.stream.Recv()
	if err == io.EOF || talosclient.StatusCode(err) == codes.Canceled {
		return domain.ClusterHealthProgress{EOF: true}, nil
	}
	if err != nil {
		return domain.ClusterHealthProgress{}, err
	}
	if meta := progress.GetMetadata(); meta != nil && meta.GetError() != "" {
		return domain.ClusterHealthProgress{Err: "cluster health check error"}, nil
	}
	return domain.ClusterHealthProgress{Message: boundRunes(progress.GetMessage(), maxHealthCheckLineRunes)}, nil
}

func (s *clusterHealthStreamImpl) Close() error {
	s.once.Do(s.cancel)
	return nil
}
