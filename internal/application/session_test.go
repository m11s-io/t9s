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
)

type lifecycleCatalog struct{}

func (lifecycleCatalog) List(context.Context) ([]domain.ClusterContext, error) {
	return []domain.ClusterContext{{Name: "alpha"}, {Name: "beta"}}, nil
}

type lifecycleFactory struct {
	open func(context.Context, string) (ports.Session, error)
}

func (f lifecycleFactory) Open(ctx context.Context, name string) (ports.Session, error) {
	return f.open(ctx, name)
}

type lifecycleResolver struct {
	resolve func(context.Context, string) (ports.KubernetesNodeReader, error)
}

func (r lifecycleResolver) Resolve(ctx context.Context, name string) (ports.KubernetesNodeReader, error) {
	return r.resolve(ctx, name)
}

func newLifecycleClient(open func(context.Context, string) (ports.Session, error), resolver ports.KubernetesResolver) *Client {
	return NewClient(Dependencies{ContextCatalog: lifecycleCatalog{}, SessionFactory: lifecycleFactory{open: open}, KubernetesResolver: resolver})
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestClientOpensIndependentSessions(t *testing.T) {
	var mu sync.Mutex
	opened := map[string]*testkit.FakeSession{}
	client := newLifecycleClient(func(_ context.Context, name string) (ports.Session, error) {
		session := &testkit.FakeSession{}
		mu.Lock()
		opened[name] = session
		mu.Unlock()
		return session, nil
	}, nil)

	first, err := client.Open(context.Background(), "alpha", Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Open(context.Background(), "beta", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("Open returned the same session twice")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel, err := second.operationContext(context.Background(), 0)
	if err != nil {
		t.Fatalf("second session was canceled by closing the first: %v", err)
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("second session operation context is canceled: %v", err)
	}
}

func TestSessionCloseCancelsInflightAndRejectsNewWork(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{}, nil
	}, nil)
	session, err := client.Open(parent, "alpha", Options{})
	if err != nil {
		t.Fatal(err)
	}
	operation, operationCancel, err := session.operationContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer operationCancel()
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-operation.Done():
	case <-time.After(time.Second):
		t.Fatal("session close did not cancel an in-flight operation")
	}
	if _, _, err := session.operationContext(context.Background(), 0); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("new operation after close error = %v, want ErrSessionClosed", err)
	}
}

func TestSessionConcurrentCloseClosesAdapterOnce(t *testing.T) {
	adapter := &testkit.FakeSession{}
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) { return adapter, nil }, nil)
	session, err := client.Open(context.Background(), "alpha", Options{})
	if err != nil {
		t.Fatal(err)
	}
	const callers = 32
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = session.Close() }()
	}
	wg.Wait()
	waitFor(t, func() bool { return adapter.CloseCount() == 1 })
	if got := adapter.CloseCount(); got != 1 {
		t.Fatalf("adapter close count = %d, want 1", got)
	}
}

func TestOpenCanceledAfterAdapterReturnsClosesLateSession(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	adapter := &testkit.FakeSession{}
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) {
		cancel()
		return adapter, nil
	}, nil)
	if _, err := client.Open(parent, "alpha", Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open error = %v, want context.Canceled", err)
	}
	waitFor(t, func() bool { return adapter.CloseCount() == 1 })
	if got := adapter.CloseCount(); got != 1 {
		t.Fatalf("late adapter close count = %d, want 1", got)
	}
}

func TestSuccessfulOpenRetainsParentLifetime(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	adapter := &testkit.FakeSession{}
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) { return adapter, nil }, nil)
	session, err := client.Open(parent, "alpha", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, operationCancel, err := session.operationContext(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer operationCancel()
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not reach session operation")
	}
	waitFor(t, func() bool { return adapter.CloseCount() == 1 })
}

func TestKubernetesResolutionCancellationClosesSession(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	adapter := &testkit.FakeSession{}
	resolver := lifecycleResolver{resolve: func(context.Context, string) (ports.KubernetesNodeReader, error) {
		cancel()
		return nil, errors.New("resolution interrupted")
	}}
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) { return adapter, nil }, resolver)
	if _, err := client.Open(parent, "alpha", Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open error = %v, want context.Canceled", err)
	}
	waitFor(t, func() bool { return adapter.CloseCount() == 1 })
	if got := adapter.CloseCount(); got != 1 {
		t.Fatalf("adapter close count = %d, want 1", got)
	}
}

func TestKubernetesResolutionCancellationClosesAdapterWhileResolverIsBlocked(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	adapter := &testkit.FakeSession{}
	resolverStarted := make(chan struct{})
	resolverRelease := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-resolverRelease:
		default:
			close(resolverRelease)
		}
	})
	resolver := lifecycleResolver{resolve: func(context.Context, string) (ports.KubernetesNodeReader, error) {
		close(resolverStarted)
		<-resolverRelease
		return nil, errors.New("resolution interrupted")
	}}
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) { return adapter, nil }, resolver)
	openResult := make(chan error, 1)
	go func() {
		_, err := client.Open(parent, "alpha", Options{})
		openResult <- err
	}()
	select {
	case <-resolverStarted:
	case <-time.After(time.Second):
		t.Fatal("resolver was not entered")
	}
	cancel()
	waitFor(t, func() bool { return adapter.CloseCount() == 1 })
	close(resolverRelease)
	select {
	case err := <-openResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Open error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Open did not return after blocked resolver was released")
	}
}

func TestKubernetesResolutionErrorDegradesCorrelation(t *testing.T) {
	adapter := &testkit.FakeSession{}
	resolver := lifecycleResolver{resolve: func(context.Context, string) (ports.KubernetesNodeReader, error) {
		return nil, errors.New("optional Kubernetes config is unavailable")
	}}
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) { return adapter, nil }, resolver)
	session, err := client.Open(context.Background(), "alpha", Options{})
	if err != nil {
		t.Fatalf("Open returned an error for optional Kubernetes resolution failure: %v", err)
	}
	if session.kubernetesNodes != nil {
		t.Fatal("session retained a Kubernetes reader after resolution failed")
	}
}

func TestSessionCloseReturnsWhileAdapterCloseBlocks(t *testing.T) {
	closeStarted := make(chan struct{})
	closeRelease := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-closeRelease:
		default:
			close(closeRelease)
		}
	})
	adapter := &testkit.FakeSession{CloseFunc: func() error {
		close(closeStarted)
		<-closeRelease
		return nil
	}}
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) { return adapter, nil }, nil)
	session, err := client.Open(context.Background(), "alpha", Options{})
	if err != nil {
		t.Fatal(err)
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- session.Close() }()
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("Close returned an error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close waited for a blocked adapter Close")
	}
	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("adapter Close was not started")
	}
}

func TestSessionOperationContextCombinesCallerCancellation(t *testing.T) {
	client := newLifecycleClient(func(context.Context, string) (ports.Session, error) { return &testkit.FakeSession{}, nil }, nil)
	session, err := client.Open(context.Background(), "alpha", Options{})
	if err != nil {
		t.Fatal(err)
	}
	caller, callerCancel := context.WithCancel(context.Background())
	operation, operationCancel, err := session.operationContext(caller, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer operationCancel()
	callerCancel()
	select {
	case <-operation.Done():
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not reach operation context")
	}
}
