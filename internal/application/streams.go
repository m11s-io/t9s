package application

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func (s *Session) OpenServiceLogs(ctx context.Context, request domain.LogRequest) (ports.ServiceLogStream, error) {
	if s == nil || s.sdkSession == nil {
		return nil, ErrCapabilityUnavailable
	}
	reader := s.sdkSession.ServiceLogs()
	if reader == nil {
		return nil, ErrCapabilityUnavailable
	}
	stream, err := openLifecycleStream(s, ctx, func(ctx context.Context) (ports.ServiceLogStream, error) {
		return reader.Open(ctx, request)
	}, func(stream ports.ServiceLogStream) func(context.Context) (domain.LogBatch, error) { return stream.Next }, func(stream ports.ServiceLogStream) error { return stream.Close() }, func(batch domain.LogBatch, err error) bool {
		return err != nil || batch.EOF || batch.Err != ""
	})
	if err != nil {
		return nil, err
	}
	return &serviceLogStream{stream}, nil
}

func (s *Session) OpenDmesg(ctx context.Context, request domain.DmesgRequest) (ports.DmesgStream, error) {
	if s == nil || s.sdkSession == nil {
		return nil, ErrCapabilityUnavailable
	}
	reader := s.sdkSession.Dmesg()
	if reader == nil {
		return nil, ErrCapabilityUnavailable
	}
	stream, err := openLifecycleStream(s, ctx, func(ctx context.Context) (ports.DmesgStream, error) {
		return reader.Open(ctx, request)
	}, func(stream ports.DmesgStream) func(context.Context) (domain.DmesgBatch, error) { return stream.Next }, func(stream ports.DmesgStream) error { return stream.Close() }, func(batch domain.DmesgBatch, err error) bool {
		return err != nil || batch.EOF || batch.Err != ""
	})
	if err != nil {
		return nil, err
	}
	return &dmesgStream{stream}, nil
}

func (s *Session) FollowEvents(ctx context.Context) (ports.EventStream, error) {
	if s == nil || s.sdkSession == nil {
		return nil, ErrCapabilityUnavailable
	}
	reader := s.sdkSession.Events()
	if reader == nil {
		return nil, ErrCapabilityUnavailable
	}
	stream, err := openLifecycleStream(s, ctx, reader.Open, func(stream ports.EventStream) func(context.Context) (domain.EventSet, error) { return stream.Next }, func(stream ports.EventStream) error {
		return stream.Close()
	}, func(_ domain.EventSet, err error) bool { return err != nil })
	if err != nil {
		return nil, err
	}
	return &eventStream{stream}, nil
}

func (s *Session) OpenClusterHealth(ctx context.Context, request domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
	if s == nil || s.sdkSession == nil {
		return nil, ErrCapabilityUnavailable
	}
	reader := s.sdkSession.ClusterHealth()
	if reader == nil {
		return nil, ErrCapabilityUnavailable
	}
	stream, err := openLifecycleStream(s, ctx, func(ctx context.Context) (ports.ClusterHealthStream, error) {
		return reader.Open(ctx, request)
	}, func(stream ports.ClusterHealthStream) func(context.Context) (domain.ClusterHealthProgress, error) {
		return stream.Next
	}, func(stream ports.ClusterHealthStream) error { return stream.Close() }, func(progress domain.ClusterHealthProgress, err error) bool {
		return err != nil || progress.EOF || progress.Err != ""
	})
	if err != nil {
		return nil, err
	}
	return &clusterHealthStream{stream}, nil
}

type serviceLogStream struct {
	*lifecycleStream[domain.LogBatch]
}
type dmesgStream struct {
	*lifecycleStream[domain.DmesgBatch]
}
type eventStream struct {
	*lifecycleStream[domain.EventSet]
}
type clusterHealthStream struct {
	*lifecycleStream[domain.ClusterHealthProgress]
}

type lifecycleStream[T any] struct {
	next     func(context.Context) (T, error)
	close    func() error
	ctx      context.Context
	cancel   context.CancelFunc
	terminal func(T, error) bool
	stopMu   sync.Mutex
	stop     func() bool
	closed   bool
	once     sync.Once
	closeErr error
}

func openLifecycleStream[S, T any](session *Session, ctx context.Context, open func(context.Context) (S, error), next func(S) func(context.Context) (T, error), closeStream func(S) error, terminal func(T, error) bool) (*lifecycleStream[T], error) {
	streamCtx, cancel, err := session.operationContext(ctx, 0)
	if err != nil {
		return nil, err
	}
	stream, err := open(streamCtx)
	if err != nil {
		openErr := err
		contextErr := streamCtx.Err()
		cancel()
		if contextErr != nil && errors.Is(openErr, contextErr) {
			return nil, contextErr
		}
		return nil, openErr
	}
	if err := streamCtx.Err(); err != nil {
		_ = closeStream(stream)
		cancel()
		return nil, err
	}
	wrapped := &lifecycleStream[T]{
		next: next(stream), close: func() error { return closeStream(stream) },
		ctx: streamCtx, cancel: cancel, terminal: terminal,
	}
	wrapped.setStop(context.AfterFunc(streamCtx, wrapped.closeCore))
	return wrapped, nil
}

func (s *lifecycleStream[T]) Next(ctx context.Context) (T, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithCancel(ctx)
	stopLifetime := context.AfterFunc(s.ctx, cancel)
	stopCall := context.AfterFunc(ctx, func() { _ = s.Close() })
	value, err := s.next(callCtx)
	stopLifetime()
	stopCall()
	cancel()
	terminalPayload := s.terminal(value, nil)
	if terminalPayload || errors.Is(err, io.EOF) {
		_ = s.Close()
		return value, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		_ = s.Close()
		return value, ctxErr
	}
	if streamErr := s.ctx.Err(); streamErr != nil {
		_ = s.Close()
		return value, streamErr
	}
	if err != nil {
		if s.terminal(value, err) {
			_ = s.Close()
		}
		return value, err
	}
	return value, nil
}

func (s *lifecycleStream[T]) Close() error {
	s.stopMu.Lock()
	s.closed = true
	stop := s.stop
	s.stop = nil
	s.stopMu.Unlock()
	if stop != nil {
		stop()
	}
	s.closeCore()
	return s.closeErr
}

func (s *lifecycleStream[T]) setStop(stop func() bool) {
	s.stopMu.Lock()
	if s.closed {
		s.stopMu.Unlock()
		stop()
		return
	}
	s.stop = stop
	s.stopMu.Unlock()
}

func (s *lifecycleStream[T]) closeCore() {
	s.stopMu.Lock()
	s.closed = true
	s.stopMu.Unlock()
	s.once.Do(func() {
		s.cancel()
		s.closeErr = s.close()
	})
}
