package talos

import (
	"bytes"
	"context"
	"io"
	"sync"
	"unicode/utf8"

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
	stream  talosDataStream
	cancel  context.CancelFunc
	once    sync.Once
	decoder dataStreamDecoder
}

func (s *serviceLogStream) Next(ctx context.Context) (domain.LogBatch, error) {
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	return s.decoder.next(s.stream, false)
}

// dataStreamDecoder retains a frame cursor and a bounded line prefix. Logs are
// arbitrary byte chunks; dmesg frames end a record even without a newline.
type dataStreamDecoder struct {
	frame     []byte
	partial   []byte
	truncated bool
	eof       bool
}

func (d *dataStreamDecoder) finishLine() string {
	prefix := d.partial
	if d.truncated {
		// A retained byte prefix can end inside a UTF-8 rune.
		for len(prefix) > 0 && !utf8.Valid(prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	line := boundRunes(string(prefix), maxServiceLogLineRunes)
	d.partial = nil
	d.truncated = false
	return line
}

func (d *dataStreamDecoder) next(stream talosDataStream, recordFrames bool) (domain.LogBatch, error) {
	batch := domain.LogBatch{}
	for {
		if len(d.frame) > 0 {
			end := bytes.IndexByte(d.frame, '\n')
			size := len(d.frame)
			if end >= 0 {
				size = end
			}
			room := maxServiceLogLineRunes*utf8.UTFMax - len(d.partial)
			take := min(size, room)
			d.partial = append(d.partial, d.frame[:take]...)
			if take < size {
				d.truncated = true
			}
			d.frame = d.frame[size:]
			if end >= 0 {
				d.frame = d.frame[1:]
				batch.Lines = append(batch.Lines, d.finishLine())
			}
			if len(d.frame) == 0 {
				d.frame = nil
				if recordFrames && (len(d.partial) > 0 || d.truncated) {
					batch.Lines = append(batch.Lines, d.finishLine())
				}
			}
			if len(batch.Lines) >= maxServiceLogBatch || (len(d.frame) == 0 && len(batch.Lines) > 0) {
				return batch, nil
			}
			continue
		}
		if d.eof {
			batch.EOF = true
			return batch, nil
		}
		data, err := stream.Recv()
		if err == io.EOF {
			d.eof = true
			if len(d.partial) > 0 || d.truncated {
				batch.Lines = append(batch.Lines, d.finishLine())
			}
			batch.EOF = true
			return batch, nil
		}
		if err != nil {
			return domain.LogBatch{}, err
		}
		if metadata := data.GetMetadata(); metadata != nil && metadata.GetError() != "" {
			return domain.LogBatch{Err: "log stream error"}, nil
		}
		d.frame = data.GetBytes()
	}
}

func (s *serviceLogStream) Close() error {
	s.once.Do(s.cancel)
	return nil
}
