package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/require"
)

type blockingEventReader struct {
	stream *blockingEventStream
}

func (r blockingEventReader) List(context.Context) (domain.EventSet, error) {
	return domain.EventSet{}, nil
}
func (r blockingEventReader) Open(context.Context) (ports.EventStream, error) {
	return r.stream, nil
}

type blockingEventStream struct {
	closed     chan struct{}
	once       sync.Once
	mu         sync.Mutex
	closeCalls int
}

func newBlockingEventStream() *blockingEventStream {
	return &blockingEventStream{closed: make(chan struct{})}
}

func (s *blockingEventStream) Next(context.Context) (domain.EventSet, error) {
	<-s.closed
	return domain.EventSet{}, errors.New("stream closed")
}

func (s *blockingEventStream) Close() error {
	s.mu.Lock()
	s.closeCalls++
	s.mu.Unlock()
	s.once.Do(func() { close(s.closed) })
	return nil
}

func (s *blockingEventStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCalls
}

func newStreamTestSession(adapter ports.Session) *Session {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	return &Session{sdkSession: adapter, lifetimeCtx: lifetimeCtx, lifetimeCancel: lifetimeCancel}
}

func TestSessionCloseUnblocksStreamNext(t *testing.T) {
	underlying := newBlockingEventStream()
	session := newStreamTestSession(&testkit.FakeSession{EventReader: blockingEventReader{stream: underlying}})
	stream, err := session.FollowEvents(context.Background())
	require.NoError(t, err)

	nextDone := make(chan error, 1)
	go func() { _, err := stream.Next(context.Background()); nextDone <- err }()
	require.NoError(t, session.Close())

	select {
	case err := <-nextDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Next remained blocked after session close")
	}
	require.Equal(t, 1, underlying.closeCount())
}

func TestStreamCloseIsIdempotent(t *testing.T) {
	underlying := newBlockingEventStream()
	session := newStreamTestSession(&testkit.FakeSession{EventReader: blockingEventReader{stream: underlying}})
	stream, err := session.FollowEvents(context.Background())
	require.NoError(t, err)

	nextDone := make(chan error, 1)
	go func() { _, err := stream.Next(context.Background()); nextDone <- err }()
	const closers = 12
	var wg sync.WaitGroup
	wg.Add(closers)
	for range closers {
		go func() { defer wg.Done(); _ = stream.Close() }()
	}
	wg.Wait()
	select {
	case <-nextDone:
	case <-time.After(time.Second):
		t.Fatal("concurrent Close did not unblock Next")
	}
	require.Equal(t, 1, underlying.closeCount())
	_ = session.Close()
}

func TestCanceledOpenClosesReturnedStream(t *testing.T) {
	underlying := newBlockingEventStream()
	openEntered := make(chan struct{})
	allowOpenReturn := make(chan struct{})
	reader := blockingEventReaderFunc(func(ctx context.Context) (ports.EventStream, error) {
		close(openEntered)
		<-allowOpenReturn
		return underlying, nil
	})
	session := newStreamTestSession(&testkit.FakeSession{EventReader: reader})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := session.FollowEvents(ctx); done <- err }()
	<-openEntered
	cancel()
	close(allowOpenReturn)
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, 1, underlying.closeCount())
	_ = session.Close()
}

func TestCancelOneStreamKeepsOthersAlive(t *testing.T) {
	first := newBlockingEventStream()
	second := newBlockingEventStream()
	var mu sync.Mutex
	streams := []*blockingEventStream{first, second}
	reader := blockingEventReaderFunc(func(context.Context) (ports.EventStream, error) {
		mu.Lock()
		defer mu.Unlock()
		stream := streams[0]
		streams = streams[1:]
		return stream, nil
	})
	session := newStreamTestSession(&testkit.FakeSession{EventReader: reader})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstStream, err := session.FollowEvents(firstCtx)
	require.NoError(t, err)
	secondStream, err := session.FollowEvents(context.Background())
	require.NoError(t, err)
	cancelFirst()

	firstResult := make(chan error, 1)
	go func() { _, err := firstStream.Next(context.Background()); firstResult <- err }()
	select {
	case <-firstResult:
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not close its stream")
	}
	require.Equal(t, 1, first.closeCount())
	require.Zero(t, second.closeCount())
	_ = secondStream.Close()
	_ = session.Close()
}

func TestTerminalStreamPayloadKeepsOriginalResult(t *testing.T) {
	t.Run("service logs EOF", func(t *testing.T) {
		underlying := &terminalLogStream{batch: domain.LogBatch{Lines: []string{"last"}, EOF: true}}
		session := newStreamTestSession(&testkit.FakeSession{LogReader: terminalLogReader{stream: underlying}})
		stream, err := session.OpenServiceLogs(context.Background(), domain.LogRequest{})
		require.NoError(t, err)
		batch, err := stream.Next(context.Background())
		require.NoError(t, err)
		require.Equal(t, []string{"last"}, batch.Lines)
		require.True(t, batch.EOF)
		require.Equal(t, 1, underlying.closeCount())
		_ = session.Close()
	})

	t.Run("dmesg terminal error payload", func(t *testing.T) {
		underlying := &terminalDmesgStream{batch: domain.DmesgBatch{Lines: []string{"partial"}, Err: "receive failed"}}
		session := newStreamTestSession(&testkit.FakeSession{DmesgReader: &testkit.FakeDmesgReader{OpenFunc: func(context.Context, domain.DmesgRequest) (ports.DmesgStream, error) {
			return underlying, nil
		}}})
		stream, err := session.OpenDmesg(context.Background(), domain.DmesgRequest{})
		require.NoError(t, err)
		batch, err := stream.Next(context.Background())
		require.NoError(t, err)
		require.Equal(t, []string{"partial"}, batch.Lines)
		require.Equal(t, "receive failed", batch.Err)
		require.Equal(t, 1, underlying.closeCount())
		_ = session.Close()
	})

	t.Run("cluster health EOF", func(t *testing.T) {
		underlying := &terminalHealthStream{progress: domain.ClusterHealthProgress{Message: "healthy", EOF: true}}
		session := newStreamTestSession(&testkit.FakeSession{ClusterHealthReader: &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
			return underlying, nil
		}}})
		stream, err := session.OpenClusterHealth(context.Background(), domain.ClusterHealthRequest{})
		require.NoError(t, err)
		progress, err := stream.Next(context.Background())
		require.NoError(t, err)
		require.Equal(t, "healthy", progress.Message)
		require.True(t, progress.EOF)
		require.Equal(t, 1, underlying.closeCount())
		_ = session.Close()
	})
}

func TestStreamOpenPreservesAdapterError(t *testing.T) {
	wantErr := errors.New("adapter open failure")
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	reader := blockingEventReaderFunc(func(ctx context.Context) (ports.EventStream, error) {
		cancelCaller()
		return nil, wantErr
	})
	session := newStreamTestSession(&testkit.FakeSession{EventReader: reader})
	_, err := session.FollowEvents(callerCtx)
	require.ErrorIs(t, err, wantErr)
	require.NotErrorIs(t, err, context.Canceled)
	_ = session.Close()
}

func TestNextContextCancellationClosesStream(t *testing.T) {
	underlying := newBlockingEventStream()
	session := newStreamTestSession(&testkit.FakeSession{EventReader: blockingEventReader{stream: underlying}})
	stream, err := session.FollowEvents(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	nextDone := make(chan error, 1)
	go func() { _, err := stream.Next(ctx); nextDone <- err }()
	cancel()
	select {
	case err := <-nextDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Next remained blocked after its context was canceled")
	}
	require.Equal(t, 1, underlying.closeCount())
	_ = session.Close()
}

func TestStreamCancellationDuringCallbackRegistrationAndClose(t *testing.T) {
	for range 1000 {
		underlying := newBlockingEventStream()
		ctx, cancel := context.WithCancel(context.Background())
		openReturned := make(chan struct{})
		reader := blockingEventReaderFunc(func(context.Context) (ports.EventStream, error) {
			close(openReturned)
			return underlying, nil
		})
		session := newStreamTestSession(&testkit.FakeSession{EventReader: reader})
		opened := make(chan ports.EventStream, 1)
		openErr := make(chan error, 1)
		go func() {
			stream, err := session.FollowEvents(ctx)
			opened <- stream
			openErr <- err
		}()
		<-openReturned
		cancel()
		stream := <-opened
		if err := <-openErr; err == nil {
			_ = stream.Close()
		}
		require.NoError(t, session.Close())
		require.Equal(t, 1, underlying.closeCount())
	}
}

type terminalLogReader struct{ stream ports.ServiceLogStream }

func (r terminalLogReader) Open(context.Context, domain.LogRequest) (ports.ServiceLogStream, error) {
	return r.stream, nil
}

type terminalLogStream struct {
	batch  domain.LogBatch
	mu     sync.Mutex
	closes int
}

func (s *terminalLogStream) Next(context.Context) (domain.LogBatch, error) { return s.batch, nil }
func (s *terminalLogStream) Close() error                                  { s.mu.Lock(); s.closes++; s.mu.Unlock(); return nil }
func (s *terminalLogStream) closeCount() int                               { s.mu.Lock(); defer s.mu.Unlock(); return s.closes }

type terminalDmesgStream struct {
	batch  domain.DmesgBatch
	mu     sync.Mutex
	closes int
}

func (s *terminalDmesgStream) Next(context.Context) (domain.DmesgBatch, error) { return s.batch, nil }
func (s *terminalDmesgStream) Close() error                                    { s.mu.Lock(); s.closes++; s.mu.Unlock(); return nil }
func (s *terminalDmesgStream) closeCount() int                                 { s.mu.Lock(); defer s.mu.Unlock(); return s.closes }

type terminalHealthStream struct {
	progress domain.ClusterHealthProgress
	mu       sync.Mutex
	closes   int
}

func (s *terminalHealthStream) Next(context.Context) (domain.ClusterHealthProgress, error) {
	return s.progress, nil
}
func (s *terminalHealthStream) Close() error    { s.mu.Lock(); s.closes++; s.mu.Unlock(); return nil }
func (s *terminalHealthStream) closeCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.closes }

type blockingEventReaderFunc func(context.Context) (ports.EventStream, error)

func (f blockingEventReaderFunc) List(context.Context) (domain.EventSet, error) {
	return domain.EventSet{}, nil
}
func (f blockingEventReaderFunc) Open(ctx context.Context) (ports.EventStream, error) { return f(ctx) }
