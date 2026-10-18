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
		require.Error(t, err)
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

type blockingEventReaderFunc func(context.Context) (ports.EventStream, error)

func (f blockingEventReaderFunc) List(context.Context) (domain.EventSet, error) {
	return domain.EventSet{}, nil
}
func (f blockingEventReaderFunc) Open(ctx context.Context) (ports.EventStream, error) { return f(ctx) }
