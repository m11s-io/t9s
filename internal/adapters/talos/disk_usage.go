package talos

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"unicode"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

const maxDiskUsageBatch = 200

type diskUsageDataStream interface {
	Recv() (*machineapi.DiskUsageInfo, error)
}

type diskUsageClient interface {
	DiskUsage(context.Context, string, *machineapi.DiskUsageRequest) (diskUsageDataStream, error)
}

type machineryDiskUsageClient struct{ client *talosclient.Client }

func (c machineryDiskUsageClient) DiskUsage(ctx context.Context, node string, request *machineapi.DiskUsageRequest) (diskUsageDataStream, error) {
	return c.client.DiskUsage(talosclient.WithNode(ctx, node), request)
}

type diskUsageReader struct{ client diskUsageClient }

func newDiskUsageReader(client diskUsageClient) ports.DiskUsageReader {
	return &diskUsageReader{client: client}
}

func (r *diskUsageReader) Open(ctx context.Context, request domain.DiskUsageRequest) (ports.DiskUsageStream, error) {
	root := path.Clean(request.Path)
	if request.Node == "" || !path.IsAbs(root) || strings.ContainsFunc(request.Path, unicode.IsControl) {
		return nil, fmt.Errorf("disk usage requires a node and an absolute path")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.client.DiskUsage(streamCtx, request.Node, &machineapi.DiskUsageRequest{
		Paths:          []string{root},
		RecursionDepth: 1,
		All:            false,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open disk usage: %w", err)
	}
	return &diskUsageStream{stream: stream, ctx: streamCtx, cancel: cancel, root: root}, nil
}

type diskUsageStream struct {
	stream diskUsageDataStream
	ctx    context.Context
	cancel context.CancelFunc
	root   string
	once   sync.Once
}

func (s *diskUsageStream) Next(ctx context.Context) (domain.DiskUsageBatch, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()

	batch := domain.DiskUsageBatch{Rows: make([]domain.DiskUsageEntry, 0, maxDiskUsageBatch)}
	for len(batch.Rows) < maxDiskUsageBatch {
		info, err := s.stream.Recv()
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
			return batch, fmt.Errorf("receive disk usage: %w", err)
		}
		if info == nil {
			continue
		}
		name := info.GetName()
		if name == "" {
			name = path.Join(s.root, info.GetRelativeName())
		}
		rowErr := info.GetError()
		if metadataErr := info.GetMetadata().GetError(); metadataErr != "" {
			rowErr = metadataErr
		}
		batch.Rows = append(batch.Rows, domain.DiskUsageEntry{Path: name, SizeBytes: info.GetSize(), Error: rowErr})
	}
	return batch, nil
}

func (s *diskUsageStream) Close() error {
	s.once.Do(s.cancel)
	return nil
}
