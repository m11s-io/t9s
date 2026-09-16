package talos

import (
	"context"
	"sync"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type dmesgClient interface {
	Dmesg(ctx context.Context, node string, follow, tail bool) (talosDataStream, error)
}

type machineryDmesgClient struct{ client *talosclient.Client }

func (c machineryDmesgClient) Dmesg(ctx context.Context, node string, follow, tail bool) (talosDataStream, error) {
	return c.client.Dmesg(talosclient.WithNode(ctx, node), follow, tail)
}

type dmesgReader struct{ client dmesgClient }

func newDmesgReader(client dmesgClient) ports.DmesgReader {
	return &dmesgReader{client: client}
}

func (r *dmesgReader) Open(ctx context.Context, request domain.DmesgRequest) (ports.DmesgStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.client.Dmesg(streamCtx, request.Node, request.Follow, request.Tail)
	if err != nil {
		cancel()
		return nil, err
	}
	return &dmesgStream{stream: stream, cancel: cancel}, nil
}

type dmesgStream struct {
	stream talosDataStream
	cancel context.CancelFunc
	once   sync.Once
}

func (s *dmesgStream) Next(ctx context.Context) (domain.DmesgBatch, error) {
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	batch, err := readDataStreamBatch(s.stream)
	return domain.DmesgBatch{Lines: batch.Lines, EOF: batch.EOF, Err: batch.Err}, err
}

func (s *dmesgStream) Close() error {
	s.once.Do(s.cancel)
	return nil
}
