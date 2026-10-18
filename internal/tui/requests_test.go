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

func TestDirectStartupLoadsSharedSnapshotsIndependently(t *testing.T) {
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return &testkit.FakeSession{
				NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
					return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-1", Name: "worker-1", Role: domain.NodeRoleControl}}}, nil
				}},
				ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
					return domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "worker-1", Name: "uncapped"}}}, errors.New("service detail")
				}},
				EventReader: &testkit.FakeEventReader{ListFunc: func(context.Context) (domain.EventSet, error) {
					return domain.EventSet{}, errors.New("events unavailable")
				}},
				EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
					return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1"}}}, nil
				}},
			}, nil
		}},
		KubernetesResolver: &testkit.FakeKubernetesResolver{ResolveFunc: func(context.Context, string) (ports.KubernetesNodeReader, error) {
			return &testkit.FakeKubernetesNodeReader{ListFunc: func(context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
				return map[string]domain.KubernetesNodeSnapshot{"worker-1": {Conditions: []domain.KubernetesCondition{{Type: "Ready", Status: "True"}}}}, nil
			}}, nil
		}},
	})
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model, command := applyDirectCommand(t, model, model.Init())
	model, command = applyDirectCommand(t, model, command)
	message := command()
	batch, ok := message.(tea.BatchMsg)
	require.True(t, ok, "session startup should launch independent snapshot commands")
	require.Len(t, batch, 4)

	updated, kubeCommand := model.Update(batch[3]())
	model = updated.(directModel)
	assert.Empty(t, model.cluster.nodes.Value.Nodes, "Kubernetes may complete before nodes without creating node rows")
	updated, eventsCommand := model.Update(batch[1]())
	model = updated.(directModel)
	assert.Equal(t, directFailed, model.cluster.events.Status)
	assert.Nil(t, kubeCommand)
	assert.Nil(t, eventsCommand)
	updated, etcdCommand := model.Update(batch[2]())
	model = updated.(directModel)
	assert.Equal(t, "cp-1", model.cluster.etcd.Value.Members[0].Hostname)
	updated, nodeCommand := model.Update(batch[0]())
	model = updated.(directModel)
	assert.Equal(t, directPartial, model.cluster.services.Status)
	assert.Equal(t, "uncapped", model.cluster.services.Value.Services[0].Name)
	assert.Equal(t, "worker-1", model.cluster.nodes.Value.Nodes[0].Name)
	assert.Equal(t, domain.KubernetesReady, model.cluster.nodes.Value.Nodes[0].Kubernetes)
	assert.Nil(t, etcdCommand)
	assert.Nil(t, nodeCommand)
}

func TestDirectNodeServicesNilErrorProblemsRemainPartial(t *testing.T) {
	model := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	model.generationCounter = 4
	request, _ := model.requests.begin("cluster-nodes-services", 4)
	updated, _ := model.Update(directNodeServicesLoaded{
		request:  request.stamp,
		nodes:    domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-1", Name: "worker-1", Problem: "backend-specific node detail"}}},
		services: domain.ServiceSet{Problems: []domain.ServiceProblem{{Node: "worker-1", Message: "backend-specific service detail"}}},
	})
	model = updated.(directModel)
	assert.Equal(t, directPartial, directNodeLoadStatus(model.cluster.nodes.Value))
	assert.Equal(t, application.Partial, model.cluster.nodes.Status)
	assert.Equal(t, directPartial, model.cluster.services.Status)
	assert.Equal(t, "Some nodes report problems", model.cluster.nodes.Err)
	assert.Equal(t, "Some services report problems", model.cluster.services.Err)
	assert.NotContains(t, model.View().Content, "backend-specific")
}

func TestDirectContextPickerSelectsAndSwitchResetsSessionData(t *testing.T) {
	opened := []string{}
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(_ context.Context, name string) (ports.Session, error) {
			opened = append(opened, name)
			return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node", Name: opened[len(opened)-1]}}, ObservedAt: time.Now()}, nil
			}}}, nil
		}},
	})
	model := newDirectModel(context.Background(), client, LaunchOptions{OpenContextPicker: true})
	model, command := applyDirectCommand(t, model, model.Init())
	model, command = applyDirectCommand(t, model, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.True(t, model.contexts.active)
	model.nodes.marked = map[string]struct{}{"node": {}}
	model.nodes.filter = "old"
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = updated.(directModel)
	assert.Equal(t, "dev", model.contexts.values[model.contexts.control.Cursor()].Name)
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(directModel)
	require.NotNil(t, command)
	updated, command = model.Update(command())
	model = updated.(directModel)
	assert.Equal(t, "dev", model.cluster.contextName)
	// The direct model consumes the keyboard selection and starts a new generation.
	assert.Equal(t, uint64(2), model.generation())
	assert.Empty(t, model.nodes.marked)
	assert.Empty(t, model.nodes.filter)
	assert.Equal(t, application.Loading, model.cluster.nodes.Status)
	assert.Equal(t, directLoading, model.cluster.services.Status)
	assert.Empty(t, model.cluster.services.Value.Services)
	assert.NotNil(t, command)
}

func TestDirectNodesBeforeKubernetesEnrichesSharedNodeSnapshot(t *testing.T) {
	model := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	model.generationCounter = 3
	nodeRequest, _ := model.requests.begin("cluster-nodes-services", 3)
	kubeRequest, _ := model.requests.begin("cluster-kubernetes", 3)
	updated, _ := model.Update(directNodeServicesLoaded{
		request: nodeRequest.stamp,
		nodes:   domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "worker-1", Name: "worker-1", Kubernetes: domain.KubernetesUnknown}}},
	})
	model = updated.(directModel)
	assert.Equal(t, domain.KubernetesUnknown, model.cluster.nodes.Value.Nodes[0].Kubernetes)
	updated, _ = model.Update(directKubernetesNodesLoaded{
		request: kubeRequest.stamp,
		nodes:   map[string]domain.KubernetesNodeSnapshot{"worker-1": {Conditions: []domain.KubernetesCondition{{Type: "Ready", Status: "True"}}}},
	})
	model = updated.(directModel)
	assert.Equal(t, domain.KubernetesReady, model.cluster.nodes.Value.Nodes[0].Kubernetes)
}

func TestDirectClusterCommandAliasesRouteSharedViews(t *testing.T) {
	for _, test := range []struct {
		alias string
		view  viewKind
	}{
		{alias: "no", view: viewNodes},
		{alias: "svc", view: viewServices},
		{alias: "ev", view: viewEvents},
		{alias: "et", view: viewEtcd},
		{alias: "ov", view: viewOverview},
		{alias: "contexts", view: viewContexts},
		{alias: "problems", view: viewProblems},
	} {
		t.Run(test.alias, func(t *testing.T) {
			model := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
			updated, command := model.Update(directRunCommand{value: test.alias})
			model = updated.(directModel)
			assert.Nil(t, command)
			if test.view == viewContexts {
				assert.True(t, model.contexts.active)
			} else {
				assert.Equal(t, test.view, model.views.top().Kind)
			}
		})
	}
}

func TestDirectPaletteSubmitsContextAlias(t *testing.T) {
	model := newDirectModel(context.Background(), application.NewClient(application.Dependencies{}), LaunchOptions{})
	model.cluster.contexts = []domain.ClusterContext{{Name: "dev"}, {Name: "prod", Current: true}}
	model.cluster.contextName = "prod"
	var command tea.Cmd
	for _, message := range []tea.KeyPressMsg{
		{Code: ':', Text: ":"},
		{Code: 'c', Text: "c"},
		{Code: 't', Text: "t"},
		{Code: 'x', Text: "x"},
		{Code: tea.KeyEnter},
	} {
		updated, next := model.Update(message)
		model = updated.(directModel)
		if next != nil {
			command = next
		}
	}
	require.NotNil(t, command)
	updated, commandMessage := model.Update(command())
	model = updated.(directModel)
	assert.Nil(t, commandMessage)
	assert.True(t, model.contexts.active)
	assert.Equal(t, "prod", model.contexts.values[model.contexts.control.Cursor()].Name)
}

func TestDirectStartupHonorsContextAndNodeFocus(t *testing.T) {
	opened := ""
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(_ context.Context, name string) (ports.Session, error) {
			opened = name
			return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "node-a", Name: "a"}, {ID: "node-b", Name: "b"}}}, nil
			}}}, nil
		}},
	})
	model := newDirectModel(context.Background(), client, LaunchOptions{ContextName: "dev", NodeFocus: "node-b", OpenContextPicker: true})
	model, command := applyDirectCommand(t, model, model.Init())
	model, command = applyDirectCommand(t, model, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, "dev", opened)
	assert.Equal(t, "dev", model.cluster.contextName)
	assert.True(t, model.contexts.active)
	assert.Equal(t, "node-b", model.nodes.selectedID)
}

func applyDirectCommand(t *testing.T, model directModel, command tea.Cmd) (directModel, tea.Cmd) {
	t.Helper()
	require.NotNil(t, command)
	message := command()
	require.NotNil(t, message)
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, command := range batch {
			model, _ = applyDirectCommand(t, model, command)
		}
		return model, nil
	}
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
