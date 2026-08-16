package talos

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

const (
	serviceLogTailLines    int32 = 100
	maxServiceLogBatch           = 200
	maxServiceLogLineRunes       = 512
)

type talosDataStream interface {
	Recv() (*common.Data, error)
}

type logClient interface {
	Logs(context.Context, string, string, common.ContainerDriver, string, bool, int32) (talosDataStream, error)
}

type machineryLogClient struct{ client *talosclient.Client }

func (c machineryLogClient) Logs(ctx context.Context, node, namespace string, driver common.ContainerDriver, id string, follow bool, tailLines int32) (talosDataStream, error) {
	return c.client.Logs(talosclient.WithNode(ctx, node), namespace, driver, id, follow, tailLines)
}

type serviceLogReader struct{ client logClient }

func newServiceLogReader(client logClient) ports.ServiceLogReader {
	return &serviceLogReader{client: client}
}

func (r *serviceLogReader) Open(ctx context.Context, request domain.LogRequest) (ports.ServiceLogStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.client.Logs(
		streamCtx,
		request.Node,
		constants.SystemContainerdNamespace,
		common.ContainerDriver_CONTAINERD,
		request.Service,
		true,
		serviceLogTailLines,
	)
	if err != nil {
		cancel()
		return nil, err
	}
	return &serviceLogStream{stream: stream, cancel: cancel}, nil
}

type serviceLogStream struct {
	stream talosDataStream
	cancel context.CancelFunc
	once   sync.Once
}

func (s *serviceLogStream) Next(ctx context.Context) (domain.LogBatch, error) {
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	data, err := s.stream.Recv()
	if err == io.EOF {
		return domain.LogBatch{EOF: true}, nil
	}
	if err != nil {
		return domain.LogBatch{}, err
	}
	if metadata := data.GetMetadata(); metadata != nil && metadata.GetError() != "" {
		return domain.LogBatch{Err: "log stream error"}, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data.GetBytes()), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if len(lines) > maxServiceLogBatch {
		lines = lines[:maxServiceLogBatch]
	}
	for index := range lines {
		lines[index] = boundRunes(lines[index], maxServiceLogLineRunes)
	}
	return domain.LogBatch{Lines: lines}, nil
}

func (s *serviceLogStream) Close() error {
	s.once.Do(s.cancel)
	return nil
}
