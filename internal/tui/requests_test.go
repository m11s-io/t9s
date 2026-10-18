package tui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectStartupRendersBeforeIO(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	client := application.NewClient(application.Dependencies{ContextCatalog: &testkit.FakeContextCatalog{
		ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			close(started)
			<-finish
			return nil, errors.New("offline")
		},
	}})

	model := newDirectModel(context.Background(), client, LaunchOptions{})
	firstFrame := model.View().Content
	assert.Contains(t, firstFrame, "t9s")
	assert.Contains(t, firstFrame, "Loading")

	command := model.Init()
	require.NotNil(t, command)
	done := make(chan struct{})
	go func() { defer close(done); _ = command() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("startup command did not begin context discovery")
	}
	assert.Equal(t, firstFrame, model.View().Content)
	close(finish)
	<-done
}

func TestDirectRequestRejectsSameNodeOlderResult(t *testing.T) {
	requests := newDirectRequests()
	older, _ := requests.begin("node:worker-1", 4)
	newer, _ := requests.begin("node:worker-1", 4)

	assert.False(t, requests.current(older.stamp))
	assert.True(t, requests.current(newer.stamp))
}

func TestDirectContextSwitchClosesLateSessionOpen(t *testing.T) {
	openStarted := make(chan struct{})
	finishOpen := make(chan struct{})
	adapter := &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{}, nil
	}}}
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(ctx context.Context, _ string) (ports.Session, error) {
			close(openStarted)
			<-finishOpen
			if ctx.Err() != nil {
				return adapter, nil
			}
			return adapter, nil
		}},
	})
	model := newDirectModel(context.Background(), client, LaunchOptions{})

	contexts := model.Init()()
	updated, open := model.Update(contexts)
	model = updated.(directModel)
	require.NotNil(t, open)
	opened := make(chan tea.Msg, 1)
	go func() { opened <- open() }()
	<-openStarted

	next, _ := model.Update(directSelectContext{name: "dev"})
	model = next.(directModel)
	close(finishOpen)
	result := <-opened
	next, _ = model.Update(result)
	model = next.(directModel)
	assert.Nil(t, model.session)
	require.Eventually(t, func() bool { return adapter.CloseCount() == 1 }, time.Second, time.Millisecond)
}

func TestDirectStartupAndContextSwitchLoadRows(t *testing.T) {
	opened := make([]string, 0, 2)
	adapters := make(map[string]*testkit.FakeSession)
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(_ context.Context, contextName string) (ports.Session, error) {
			opened = append(opened, contextName)
			adapter := &testkit.FakeSession{
				NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
					return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: contextName + "-worker", Name: contextName + "-worker", Role: domain.NodeRoleWorker}}}, nil
				}},
				ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
					return domain.ServiceSet{}, nil
				}},
			}
			adapters[contextName] = adapter
			return adapter, nil
		}},
	})
	model := newDirectModel(context.Background(), client, LaunchOptions{})

	model, command := applyDirectCommand(t, model, model.Init())
	model, command = applyDirectCommand(t, model, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, uint64(1), model.generation())
	assert.NotNil(t, model.session)
	assert.Contains(t, model.View().Content, "prod-worker")

	updated, command := model.Update(directSelectContext{name: "dev"})
	model = updated.(directModel)
	model, command = applyDirectCommand(t, model, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, []string{"prod", "dev"}, opened)
	assert.Equal(t, uint64(2), model.generation())
	assert.NotNil(t, model.session)
	assert.Contains(t, model.View().Content, "dev-worker")
	assert.NotContains(t, model.View().Content, "prod-worker")
	require.Eventually(t, func() bool { return adapters["prod"].CloseCount() == 1 }, time.Second, time.Millisecond)
}

func TestDirectStartupErrorsDoNotRenderBackendDetails(t *testing.T) {
	const secret = "credential-like backend detail"
	t.Run("context catalog", func(t *testing.T) {
		client := application.NewClient(application.Dependencies{ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return nil, errors.New(secret)
		}}})
		model := newDirectModel(context.Background(), client, LaunchOptions{})
		model, _ = applyDirectCommand(t, model, model.Init())
		assert.NotContains(t, model.View().Content, secret)
	})
	t.Run("session open", func(t *testing.T) {
		client := application.NewClient(application.Dependencies{
			ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
				return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
			}},
			SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
				return nil, errors.New(secret)
			}},
		})
		model := newDirectModel(context.Background(), client, LaunchOptions{})
		model, command := applyDirectCommand(t, model, model.Init())
		model, _ = applyDirectCommand(t, model, command)
		assert.NotContains(t, model.View().Content, secret)
	})
}

func TestDirectNodeReadFailureKeepsRowsAndHidesBackendDetail(t *testing.T) {
	const secret = "private node and service diagnostic"
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return &testkit.FakeSession{
				NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
					return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-1", Name: "worker-1", Role: domain.NodeRoleWorker}}}, nil
				}},
				ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
					return domain.ServiceSet{}, errors.New(secret)
				}},
			}, nil
		}},
	})
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model, command := applyDirectCommand(t, model, model.Init())
	model, command = applyDirectCommand(t, model, command)
	model, _ = applyDirectCommand(t, model, command)

	assert.Equal(t, application.Partial, model.cluster.nodes.Status)
	assert.Len(t, model.cluster.nodes.Value.Nodes, 1)
	assert.Contains(t, model.View().Content, "worker-1")
	assert.NotContains(t, model.View().Content, secret)
}

func applyDirectCommand(t *testing.T, model directModel, command tea.Cmd) (directModel, tea.Cmd) {
	t.Helper()
	require.NotNil(t, command)
	message := command()
	require.NotNil(t, message)
	updated, next := model.Update(message)
	return updated.(directModel), next
}

func TestDirectViewExitCancelsUnstartedCommand(t *testing.T) {
	requests := newDirectRequests()
	request, cancel := requests.begin("node:worker-1", 1)
	var calls atomic.Int32
	command := directCommand(request.ctx, func(context.Context) tea.Msg {
		calls.Add(1)
		return directNoop{}
	})
	cancel()
	require.NotNil(t, command)
	assert.Nil(t, command())
	assert.Zero(t, calls.Load())
}

func TestDirectLateStreamOpenIsClosed(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	lateStream := &directTestLogStream{}
	session, err := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{
		OpenFunc: func(context.Context, string) (ports.Session, error) {
			return &testkit.FakeSession{
				LogReader: directTestLogReader{open: func(ctx context.Context, _ domain.LogRequest) (ports.ServiceLogStream, error) {
					cancelRoot()
					<-ctx.Done()
					return lateStream, nil
				}},
			}, nil
		},
	}}).Open(root, "prod", application.Options{})
	require.NoError(t, err)
	stream, err := session.OpenServiceLogs(context.Background(), domain.LogRequest{})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, stream)
	require.Eventually(t, func() bool { return lateStream.closed.Load() }, time.Second, time.Millisecond)
}

type directTestLogReader struct {
	open func(context.Context, domain.LogRequest) (ports.ServiceLogStream, error)
}

type directNoop struct{}

func (r directTestLogReader) Open(ctx context.Context, request domain.LogRequest) (ports.ServiceLogStream, error) {
	return r.open(ctx, request)
}

type directTestLogStream struct {
	closed atomic.Bool
	mu     sync.Mutex
}

func (s *directTestLogStream) Next(context.Context) (domain.LogBatch, error) {
	return domain.LogBatch{}, nil
}

func (s *directTestLogStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed.Store(true)
	return nil
}

func TestDirectTeardownCoversRuntimeFailure(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	adapter := &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{}, nil
	}}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return adapter, nil
	}}})
	model := newDirectModel(parent, client, LaunchOptions{})
	ctx, cancel := context.WithCancel(model.rootCtx)
	session, err := client.Open(ctx, "prod", application.Options{})
	require.NoError(t, err)
	model.session = session
	model.sessionCancel = cancel

	// Simulate Bubble Tea returning a runtime error before a shutdown message.
	model.fallbackCleanup()
	require.Eventually(t, func() bool { return adapter.CloseCount() == 1 }, time.Second, time.Millisecond)
	assert.ErrorIs(t, model.rootCtx.Err(), context.Canceled)
}
