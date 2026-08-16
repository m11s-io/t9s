package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateLoadsNodesForCurrentGeneration(t *testing.T) {
	model, effect := application.NewModel("prod")
	require.NotNil(t, effect)
	assert.Equal(t, application.RouteNodes, model.Route)
	assert.Equal(t, application.Loading, model.Nodes.Status)

	model, _ = application.Update(model, application.NodesLoaded{
		Generation: model.Generation,
		Nodes:      domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1"}}},
	})
	assert.Equal(t, application.Ready, model.Nodes.Status)
	assert.Len(t, model.Nodes.Value.Nodes, 1)
}

func TestUpdateIgnoresNodeResultsFromOlderGeneration(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.Generation = 2
	model.Nodes = application.NodeState{
		Status: application.Ready,
		Value:  domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "new"}}},
	}

	got, effect := application.Update(model, application.NodesLoaded{
		Generation: 1,
		Nodes:      domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "old"}}},
	})

	assert.Equal(t, model.Nodes, got.Nodes)
	assert.Equal(t, model.ContextName, got.ContextName)
	assert.Equal(t, model.Generation, got.Generation)
	assert.Nil(t, effect)
}

func TestUpdateIgnoresLoadFailuresFromOlderGeneration(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.Generation = 2
	model.Nodes = application.NodeState{
		Status: application.Ready,
		Value:  domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "new"}}},
	}

	got, effect := application.Update(model, application.LoadFailed{
		Generation: 1,
		Err:        errors.New("old cluster failed"),
	})

	assert.Equal(t, model, got)
	assert.Nil(t, effect)
}

func TestUpdateMarksNodeSetWithProblemsPartial(t *testing.T) {
	model, _ := application.NewModel("prod")

	got, _ := application.Update(model, application.NodesLoaded{
		Generation: model.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{Name: "cp-1"},
			{Name: "worker-1", Problem: "unreachable"},
		}},
	})

	assert.Equal(t, application.Partial, got.Nodes.Status)
	assert.Empty(t, got.Nodes.Err)
}

func TestUpdateSelectContextStartsNewGeneration(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.Nodes = application.NodeState{
		Status: application.Ready,
		Value:  domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1"}}},
	}
	previousGeneration := model.Generation

	got, effect := application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, "dev", got.ContextName)
	assert.Equal(t, previousGeneration+1, got.Generation)
	assert.Equal(t, application.NodeState{Status: application.Loading}, got.Nodes)
	assert.NotNil(t, effect)
}

func TestUpdateIgnoresSessionFromOlderGeneration(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.Generation = 2

	got, effect := application.Update(model, application.SessionOpened{
		Generation: 1,
		Nodes: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "old"}}}, nil
		}},
	})

	assert.Equal(t, model, got)
	assert.Nil(t, effect)
}

func TestUpdateSessionOpenedSchedulesNodeReadWithoutCallingIt(t *testing.T) {
	var calls atomic.Int32
	reader := &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		calls.Add(1)
		return domain.NodeSet{}, nil
	}}
	model, _ := application.NewModel("prod")

	got, effect := application.Update(model, application.SessionOpened{
		Generation: model.Generation,
		Nodes:      reader,
	})

	assert.Equal(t, model.Nodes, got.Nodes)
	assert.Equal(t, model.ContextName, got.ContextName)
	assert.Equal(t, model.Generation, got.Generation)
	assert.NotNil(t, effect)
	assert.Zero(t, calls.Load())
}

func TestRefreshServicesLoadsANewerSnapshot(t *testing.T) {
	var calls atomic.Int32
	reader := &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
		calls.Add(1)
		return domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}}}, nil
	}}
	model, _ := application.NewModel("prod")
	model.Services = application.ServiceState{
		Status: application.Ready,
		Value:  domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "old-service"}}},
	}
	model, _ = application.Update(model, application.SessionOpened{
		Generation: model.Generation,
		Nodes: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{}, nil
		}},
		Services: reader,
	})

	model, effect := application.Update(model, application.RefreshServices{})

	assert.Equal(t, application.Loading, model.Services.Status)
	assert.Equal(t, "old-service", model.Services.Value.Services[0].Name)
	require.NotNil(t, effect)
	assert.Zero(t, calls.Load())

	message := application.NewRunner(application.Dependencies{}).Run(context.Background(), effect)
	model, effect = application.Update(model, message)

	assert.NotNil(t, effect, "ServicesLoaded now chains into loading events")
	assert.Equal(t, application.Ready, model.Services.Status)
	assert.Equal(t, "etcd", model.Services.Value.Services[0].Name)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRefreshNodesLoadsANewerSnapshot(t *testing.T) {
	var calls atomic.Int32
	reader := &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		calls.Add(1)
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-1", Name: "new-node"}}}, nil
	}}
	model, _ := application.NewModel("prod")
	model.Nodes = application.NodeState{
		Status: application.Ready,
		Value:  domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "old", Name: "old-node"}}},
	}
	model, _ = application.Update(model, application.SessionOpened{
		Generation: model.Generation,
		Nodes:      reader,
	})

	model, effect := application.Update(model, application.RefreshNodes{})

	assert.Equal(t, application.Loading, model.Nodes.Status)
	assert.Equal(t, "old-node", model.Nodes.Value.Nodes[0].Name)
	require.NotNil(t, effect)
	assert.Zero(t, calls.Load())

	message := application.NewRunner(application.Dependencies{}).Run(context.Background(), effect)
	model, effect = application.Update(model, message)

	assert.Nil(t, effect)
	assert.Equal(t, application.Ready, model.Nodes.Status)
	assert.Equal(t, "new-node", model.Nodes.Value.Nodes[0].Name)
	assert.Equal(t, int32(1), calls.Load())
}

func TestServicesLoadedWithNodeProblemsIsPartial(t *testing.T) {
	model := application.Model{
		Generation: 3,
		Nodes:      application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-1"}}}},
	}

	updated, effect := application.Update(model, application.ServicesLoaded{Generation: 3, Services: domain.ServiceSet{
		Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "etcd"}},
		Problems: []domain.ServiceProblem{{Node: "worker-1", Message: "services unavailable"}},
	}})

	assert.NotNil(t, effect, "ServicesLoaded now chains into loading events")
	assert.Equal(t, application.Partial, updated.Services.Status)
	assert.Equal(t, application.Ready, updated.Nodes.Status)
	assert.Len(t, updated.Services.Value.Services, 1)
	assert.Len(t, updated.Services.Value.Problems, 1)
}

func TestServicesFailureDoesNotDiscardSuccessfulNodes(t *testing.T) {
	model := application.Model{
		Generation: 4,
		Nodes:      application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-1"}}}},
		Services:   application.ServiceState{Status: application.Ready, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{{Name: "old"}}}},
	}

	updated, effect := application.Update(model, application.ServicesFailed{Generation: 4, Err: errors.New("discovery unavailable")})

	assert.Nil(t, effect)
	assert.Equal(t, application.Ready, updated.Nodes.Status)
	assert.Equal(t, "cp-1", updated.Nodes.Value.Nodes[0].ID)
	assert.Equal(t, application.Failed, updated.Services.Status)
	assert.Equal(t, "services unavailable", updated.Services.Err)
	assert.Equal(t, "old", updated.Services.Value.Services[0].Name)
}

func TestStaleServicesFailureIsIgnored(t *testing.T) {
	model := application.Model{Generation: 5, Services: application.ServiceState{Status: application.Ready}}

	updated, effect := application.Update(model, application.ServicesFailed{Generation: 4, Err: errors.New("stale")})

	assert.Nil(t, effect)
	assert.Equal(t, model, updated)
}

func TestRunnerLoadsCatalogBeforeOpeningSelectedSession(t *testing.T) {
	var openCalls atomic.Int32
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{
			{Name: "dev", Current: true},
			{Name: "prod"},
		}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(_ context.Context, contextName string) (ports.Session, error) {
		openCalls.Add(1)
		if contextName != "prod" {
			return nil, fmt.Errorf("opened unexpected context %q", contextName)
		}
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "prod-cp-1"}}}, nil
			}},
			ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
				return domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "prod-cp-1", Name: "etcd"}}}, nil
			}},
		}, nil
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})
	t.Cleanup(func() { require.NoError(t, runner.Close()) })

	model, effect := application.NewModel("prod")
	assert.Zero(t, openCalls.Load())

	contextsMessage := runner.Run(context.Background(), effect)
	assert.IsType(t, application.ContextsLoaded{}, contextsMessage)
	assert.Zero(t, openCalls.Load())

	model, effect = application.Update(model, contextsMessage)
	sessionMessage := runner.Run(context.Background(), effect)
	assert.IsType(t, application.SessionOpened{}, sessionMessage)
	assert.Equal(t, int32(1), openCalls.Load())

	model, effect = application.Update(model, sessionMessage)
	nodesMessage := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, nodesMessage)
	servicesMessage := runner.Run(context.Background(), effect)
	model, _ = application.Update(model, servicesMessage)

	assert.Equal(t, application.Ready, model.Nodes.Status)
	assert.Equal(t, "prod-cp-1", model.Nodes.Value.Nodes[0].Name)
	assert.Equal(t, application.Ready, model.Services.Status)
	assert.Len(t, model.Services.Value.Services, 1)
}

func TestRunnerResolvesCurrentContextWithoutOverride(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{
			{Name: "dev"},
			{Name: "prod", Current: true},
		}, nil
	}}
	runner := application.NewRunner(application.Dependencies{ContextCatalog: catalog})

	model, effect := application.NewModel("")
	message := runner.Run(context.Background(), effect)
	model, _ = application.Update(model, message)

	assert.Equal(t, "prod", model.ContextName)
	assert.Equal(t, []domain.ClusterContext{{Name: "dev"}, {Name: "prod", Current: true}}, model.Contexts)
}

func TestRunnerReportsMissingSessionFactory(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
	}}
	runner := application.NewRunner(application.Dependencies{ContextCatalog: catalog})

	model, effect := application.NewModel("prod")
	model, effect = runUpdate(t, runner, model, effect)
	message := runner.Run(context.Background(), effect)

	failure, ok := message.(application.LoadFailed)
	require.True(t, ok)
	assert.Equal(t, model.Generation, failure.Generation)
	assert.ErrorContains(t, failure.Err, "session factory is not configured")
}

func TestRunnerReportsNilSession(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return nil, nil
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})

	model, effect := application.NewModel("prod")
	model, effect = runUpdate(t, runner, model, effect)
	message := runner.Run(context.Background(), effect)

	failure, ok := message.(application.LoadFailed)
	require.True(t, ok)
	assert.Equal(t, model.Generation, failure.Generation)
	assert.ErrorContains(t, failure.Err, "returned no session")
}

func TestRunnerReportsSessionWithoutNodeReader(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{}, nil
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})
	t.Cleanup(func() { require.NoError(t, runner.Close()) })

	model, effect := application.NewModel("prod")
	model, effect = runUpdate(t, runner, model, effect)
	message := runner.Run(context.Background(), effect)

	failure, ok := message.(application.LoadFailed)
	require.True(t, ok)
	assert.Equal(t, model.Generation, failure.Generation)
	assert.ErrorContains(t, failure.Err, "node reader is not configured")
}

func TestRunnerSessionOpenUsesCurrentEffectContext(t *testing.T) {
	openStarted := make(chan context.Context, 1)
	factory := &testkit.FakeSessionFactory{OpenFunc: func(ctx context.Context, _ string) (ports.Session, error) {
		openStarted <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: factory,
	})

	model, effect := application.NewModel("prod")
	model, effect = runUpdate(t, runner, model, effect)
	effectContext, cancelEffect := context.WithCancel(context.Background())
	result := make(chan application.Message, 1)
	go func() {
		result <- runner.Run(effectContext, effect)
	}()
	openContext := waitForContext(t, openStarted)

	cancelEffect()
	waitForSignal(t, openContext.Done(), "session open context cancellation")

	failure, ok := waitForMessage(t, result).(application.LoadFailed)
	require.True(t, ok)
	assert.Equal(t, model.Generation, failure.Generation)
	assert.ErrorIs(t, failure.Err, context.Canceled)
}

func TestRunnerStaleSessionEffectCannotEvictCurrentSession(t *testing.T) {
	currentReader := &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "dev-cp-1"}}}, nil
	}}
	currentSession := &testkit.FakeSession{NodeReader: currentReader}
	var staleOpenCalls atomic.Int32
	factory := &testkit.FakeSessionFactory{OpenFunc: func(_ context.Context, contextName string) (ports.Session, error) {
		switch contextName {
		case "dev":
			return currentSession, nil
		case "prod":
			staleOpenCalls.Add(1)
			return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{
				ListFunc: func(context.Context) (domain.NodeSet, error) {
					return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "prod-cp-1"}}}, nil
				},
			}}, nil
		default:
			return nil, fmt.Errorf("unexpected context %q", contextName)
		}
	}}
	runner := application.NewRunner(application.Dependencies{SessionFactory: factory})
	t.Cleanup(func() { require.NoError(t, runner.Close()) })

	model, _ := application.NewModel("prod")
	model, staleEffect := application.Update(model, application.ContextsLoaded{
		Generation:  model.Generation,
		Contexts:    []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}},
		ContextName: "prod",
	})
	model, currentEffect := application.Update(model, application.SelectContext{Name: "dev"})

	currentMessage := runner.Run(context.Background(), currentEffect)
	currentOpened, ok := currentMessage.(application.SessionOpened)
	require.True(t, ok)
	assert.Equal(t, model.Generation, currentOpened.Generation)

	staleMessage := runner.Run(context.Background(), staleEffect)
	staleFailure, ok := staleMessage.(application.LoadFailed)
	require.True(t, ok)
	assert.Equal(t, uint64(1), staleFailure.Generation)
	assert.ErrorIs(t, staleFailure.Err, context.Canceled)
	assert.Zero(t, staleOpenCalls.Load())
	assert.Zero(t, currentSession.CloseCount())

	model, nodeEffect := application.Update(model, currentMessage)
	nodesMessage := runner.Run(context.Background(), nodeEffect)
	model, _ = application.Update(model, nodesMessage)
	assert.Equal(t, application.Ready, model.Nodes.Status)
	assert.Equal(t, "dev-cp-1", model.Nodes.Value.Nodes[0].Name)
}

func TestRunnerCancelsPreviousGenerationAndRejectsItsResult(t *testing.T) {
	started := make(chan context.Context, 1)
	canceled := make(chan struct{})
	var cancelOnce sync.Once
	var previousReadContext context.Context
	oldReader := &testkit.FakeNodeReader{ListFunc: func(ctx context.Context) (domain.NodeSet, error) {
		started <- ctx
		<-ctx.Done()
		cancelOnce.Do(func() { close(canceled) })
		return domain.NodeSet{}, ctx.Err()
	}}
	newReader := &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "dev-cp-1"}}}, nil
	}}
	oldSession := &testkit.FakeSession{NodeReader: oldReader}
	newSession := &testkit.FakeSession{NodeReader: newReader}

	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(_ context.Context, contextName string) (ports.Session, error) {
		switch contextName {
		case "prod":
			return oldSession, nil
		case "dev":
			if !errors.Is(previousReadContext.Err(), context.Canceled) {
				return nil, errors.New("opened dev before canceling the prod read context")
			}
			if oldSession.CloseCount() != 1 {
				return nil, errors.New("opened dev before closing the prod session")
			}
			return newSession, nil
		default:
			return nil, fmt.Errorf("unexpected context %q", contextName)
		}
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})
	t.Cleanup(func() { require.NoError(t, runner.Close()) })

	model, effect := application.NewModel("prod")
	model, effect = runUpdate(t, runner, model, effect)
	model, effect = runUpdate(t, runner, model, effect)
	require.NotNil(t, effect)

	oldResult := make(chan application.Message, 1)
	go func() {
		oldResult <- runner.Run(context.Background(), effect)
	}()
	previousReadContext = waitForContext(t, started)

	model, effect = application.Update(model, application.SelectContext{Name: "dev"})
	require.Equal(t, uint64(2), model.Generation)
	model, effect = runUpdate(t, runner, model, effect)
	waitForSignal(t, canceled, "generation 1 node read cancellation")
	require.Equal(t, 1, oldSession.CloseCount())

	modelBeforeOldResult := model
	model, staleEffect := application.Update(model, waitForMessage(t, oldResult))
	assert.Equal(t, modelBeforeOldResult, model)
	assert.Nil(t, staleEffect)

	nodesMessage := runner.Run(context.Background(), effect)
	if failure, failed := nodesMessage.(application.LoadFailed); failed {
		t.Fatalf("generation 2 node load failed: %v", failure.Err)
	}
	model, effect = application.Update(model, nodesMessage)
	require.Nil(t, effect)
	require.Equal(t, application.Ready, model.Nodes.Status)
	assert.Equal(t, "dev-cp-1", model.Nodes.Value.Nodes[0].Name)
}

func TestRunnerRejectsSessionThatOpensAfterReplacement(t *testing.T) {
	openStarted := make(chan context.Context, 1)
	openCanceled := make(chan struct{})
	var previousOpenContext context.Context
	oldSession := &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{
		ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "prod-cp-1"}}}, nil
		},
	}}
	newSession := &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{
		ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "dev-cp-1"}}}, nil
		},
	}}

	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "prod", Current: true}, {Name: "dev"}}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(ctx context.Context, contextName string) (ports.Session, error) {
		switch contextName {
		case "prod":
			openStarted <- ctx
			<-ctx.Done()
			close(openCanceled)
			return oldSession, nil
		case "dev":
			if !errors.Is(previousOpenContext.Err(), context.Canceled) {
				return nil, errors.New("opened dev before canceling the prod session open context")
			}
			return newSession, nil
		default:
			return nil, fmt.Errorf("unexpected context %q", contextName)
		}
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})
	t.Cleanup(func() { require.NoError(t, runner.Close()) })

	model, effect := application.NewModel("prod")
	model, effect = runUpdate(t, runner, model, effect)
	oldResult := make(chan application.Message, 1)
	go func() {
		oldResult <- runner.Run(context.Background(), effect)
	}()
	previousOpenContext = waitForContext(t, openStarted)

	model, effect = application.Update(model, application.SelectContext{Name: "dev"})
	model, effect = runUpdate(t, runner, model, effect)
	waitForSignal(t, openCanceled, "generation 1 session open cancellation")

	oldMessage := waitForMessage(t, oldResult)
	oldFailure, ok := oldMessage.(application.LoadFailed)
	require.True(t, ok)
	assert.Equal(t, uint64(1), oldFailure.Generation)
	assert.ErrorIs(t, oldFailure.Err, context.Canceled)
	assert.Equal(t, 1, oldSession.CloseCount())

	modelBeforeOldResult := model
	model, staleEffect := application.Update(model, oldMessage)
	assert.Equal(t, modelBeforeOldResult, model)
	assert.Nil(t, staleEffect)

	nodesMessage := runner.Run(context.Background(), effect)
	if failure, failed := nodesMessage.(application.LoadFailed); failed {
		t.Fatalf("generation 2 node load failed after late session result: %v", failure.Err)
	}
	model, effect = application.Update(model, nodesMessage)
	require.Nil(t, effect)
	require.Equal(t, application.Ready, model.Nodes.Status)
	assert.Equal(t, "dev-cp-1", model.Nodes.Value.Nodes[0].Name)
}

func runUpdate(t *testing.T, runner *application.Runner, model application.Model, effect application.Effect) (application.Model, application.Effect) {
	t.Helper()
	require.NotNil(t, effect)

	return application.Update(model, runner.Run(context.Background(), effect))
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	select {
	case <-signal:
	case <-deadline.C:
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForMessage(t *testing.T, messages <-chan application.Message) application.Message {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	select {
	case message := <-messages:
		return message
	case <-deadline.C:
		t.Fatal("timed out waiting for application message")
		return nil
	}
}

func waitForContext(t *testing.T, contexts <-chan context.Context) context.Context {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	select {
	case ctx := <-contexts:
		return ctx
	case <-deadline.C:
		t.Fatal("timed out waiting for session open context")
		return nil
	}
}

func TestServicesLoadedChainsIntoLoadingEvents(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withEventReader(model, &testkit.FakeEventReader{
		ListFunc: func(context.Context) (domain.EventSet, error) {
			return domain.EventSet{Events: []domain.EventSnapshot{{Node: "cp-1", Kind: "Task", Message: "install START"}}}, nil
		},
	})

	model, effect := application.Update(model, application.ServicesLoaded{Generation: 1, Services: domain.ServiceSet{}})
	require.NotNil(t, effect)
	message := effect(t.Context(), application.Dependencies{})

	loaded, ok := message.(application.EventsLoaded)
	require.True(t, ok)
	assert.Equal(t, uint64(1), loaded.Generation)
	assert.Len(t, loaded.Events.Events, 1)
}

func TestRefreshEventsReloadsFromTheCurrentReader(t *testing.T) {
	calls := 0
	model := application.Model{Generation: 1}
	model = withEventReader(model, &testkit.FakeEventReader{
		ListFunc: func(context.Context) (domain.EventSet, error) {
			calls++
			return domain.EventSet{}, nil
		},
	})

	_, effect := application.Update(model, application.RefreshEvents{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	assert.Equal(t, 1, calls)
}

func TestEventsFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1}

	model, effect := application.Update(model, application.EventsFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Events.Status)
	assert.Equal(t, "events unavailable", model.Events.Err)
}

func TestSelectContextResetsEvents(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Events: application.EventState{
		Status: application.Ready,
		Value:  domain.EventSet{Events: []domain.EventSnapshot{{Node: "cp-1"}}},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.EventState{}, model.Events)
}

// withEventReader seeds a Model's unexported eventReader field via the
// SessionOpened message, since application.Model has no exported setter and
// this test file is package application_test (no access to unexported fields).
func withEventReader(model application.Model, reader ports.EventReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Events: reader})
	return model
}

func TestControlPlaneHostnamesFiltersToControlRoleWithAddressFallback(t *testing.T) {
	nodes := []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
		{Name: "", Addresses: []string{"10.0.0.5"}, Role: domain.NodeRoleControl},
		{Name: "worker-1", Role: domain.NodeRoleWorker},
		{Name: "", Addresses: nil, Role: domain.NodeRoleControl},
	}

	hostnames := application.ControlPlaneHostnamesForTest(nodes)

	assert.Equal(t, []string{"cp-1", "10.0.0.5"}, hostnames)
}

func TestEventsLoadedChainsIntoLoadingEtcdWhenControlPlaneNodesExist(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withNodesAndEtcdReader(model, domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
		{Name: "worker-1", Role: domain.NodeRoleWorker},
	}}, &testkit.FakeEtcdReader{
		ListFunc: func(_ context.Context, controlPlaneNodes []string) (domain.EtcdSet, error) {
			assert.Equal(t, []string{"cp-1"}, controlPlaneNodes)
			return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1"}}}, nil
		},
	})

	model, effect := application.Update(model, application.EventsLoaded{Generation: 1, Events: domain.EventSet{}})
	require.NotNil(t, effect)
	message := effect(t.Context(), application.Dependencies{})

	loaded, ok := message.(application.EtcdLoaded)
	require.True(t, ok)
	assert.Len(t, loaded.Etcd.Members, 1)
}

func TestEventsLoadedDoesNotScheduleEtcdLoadWhenNoControlPlaneNodes(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withNodesAndEtcdReader(model, domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "worker-1", Role: domain.NodeRoleWorker},
	}}, &testkit.FakeEtcdReader{
		ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
			t.Fatal("etcd reader must not be called when there are no control-plane nodes")
			return domain.EtcdSet{}, nil
		},
	})

	_, effect := application.Update(model, application.EventsLoaded{Generation: 1, Events: domain.EventSet{}})

	assert.Nil(t, effect)
}

func TestRefreshEtcdRederivesControlPlaneNodesAtRefreshTime(t *testing.T) {
	calls := [][]string{}
	model := application.Model{Generation: 1}
	model = withNodesAndEtcdReader(model, domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
	}}, &testkit.FakeEtcdReader{
		ListFunc: func(_ context.Context, controlPlaneNodes []string) (domain.EtcdSet, error) {
			calls = append(calls, controlPlaneNodes)
			return domain.EtcdSet{}, nil
		},
	})
	model, _ = application.Update(model, application.NodesLoaded{Generation: 1, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
		{Name: "cp-2", Role: domain.NodeRoleControl},
	}}})

	_, effect := application.Update(model, application.RefreshEtcd{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	require.Len(t, calls, 1)
	assert.Equal(t, []string{"cp-1", "cp-2"}, calls[0], "RefreshEtcd must use the current node list, not one captured earlier")
}

func TestEtcdFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1}

	model, effect := application.Update(model, application.EtcdFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Etcd.Status)
	assert.Equal(t, "etcd unavailable", model.Etcd.Err)
}

func TestSelectContextResetsEtcd(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Etcd: application.EtcdState{
		Status: application.Ready,
		Value:  domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1"}}},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.EtcdState{}, model.Etcd)
}

// withNodesAndEtcdReader seeds Model.Nodes and the unexported etcdReader
// field via public messages, since this test file is package
// application_test and cannot set unexported fields directly (the same
// technique withEventReader already uses for eventReader).
func withNodesAndEtcdReader(model application.Model, nodes domain.NodeSet, reader ports.EtcdReader) application.Model {
	model, _ = application.Update(model, application.NodesLoaded{Generation: model.Generation, Nodes: nodes})
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Etcd: reader})
	return model
}

func TestOpenProcessesLoadsTheRequestedNode(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withProcessReader(model, &testkit.FakeProcessReader{
		ListFunc: func(_ context.Context, node string) (domain.ProcessSet, error) {
			assert.Equal(t, "cp-1", node)
			return domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 1, Command: "init"}}}, nil
		},
	})

	model, effect := application.Update(model, application.OpenProcesses{Node: "cp-1"})
	assert.Equal(t, application.Loading, model.Processes.Status)
	assert.Equal(t, "cp-1", model.Processes.Node)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.ProcessesLoaded)
	require.True(t, ok)
	assert.Len(t, loaded.Processes.Processes, 1)
}

func TestRefreshProcessesRefetchesTheRememberedNode(t *testing.T) {
	calls := []string{}
	model := application.Model{Generation: 1}
	model = withProcessReader(model, &testkit.FakeProcessReader{
		ListFunc: func(_ context.Context, node string) (domain.ProcessSet, error) {
			calls = append(calls, node)
			return domain.ProcessSet{}, nil
		},
	})
	var openEffect application.Effect
	model, openEffect = application.Update(model, application.OpenProcesses{Node: "cp-1"})
	require.NotNil(t, openEffect)
	openEffect(t.Context(), application.Dependencies{})

	_, effect := application.Update(model, application.RefreshProcesses{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	require.Len(t, calls, 2, "OpenProcesses fetches once, RefreshProcesses fetches again")
	assert.Equal(t, "cp-1", calls[1])
}

func TestProcessesFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1, Processes: application.ProcessesState{Node: "cp-1"}}

	model, effect := application.Update(model, application.ProcessesFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Processes.Status)
	assert.Equal(t, "processes unavailable", model.Processes.Err)
	assert.Equal(t, "cp-1", model.Processes.Node, "failure must not lose track of which node was open")
}

func TestSelectContextResetsProcesses(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Processes: application.ProcessesState{
		Status: application.Ready,
		Node:   "cp-1",
		Value:  domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 1}}},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.ProcessesState{}, model.Processes)
}

// withProcessReader seeds a Model's unexported processReader field via the
// SessionOpened message, since application.Model has no exported setter and
// this test file is package application_test (no access to unexported
// fields) — the same technique withEventReader already uses for eventReader.
func withProcessReader(model application.Model, reader ports.ProcessReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Processes: reader})
	return model
}

func TestOpenDisksLoadsTheRequestedNode(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withDiskReader(model, &testkit.FakeDiskReader{
		ListFunc: func(_ context.Context, node string) (domain.DiskSet, error) {
			assert.Equal(t, "cp-1", node)
			return domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "sda"}}}, nil
		},
	})

	model, effect := application.Update(model, application.OpenDisks{Node: "cp-1"})
	assert.Equal(t, application.Loading, model.Disks.Status)
	assert.Equal(t, "cp-1", model.Disks.Node)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.DisksLoaded)
	require.True(t, ok)
	assert.Len(t, loaded.Disks.Disks, 1)
}

func TestRefreshDisksRefetchesTheRememberedNode(t *testing.T) {
	calls := []string{}
	model := application.Model{Generation: 1}
	model = withDiskReader(model, &testkit.FakeDiskReader{
		ListFunc: func(_ context.Context, node string) (domain.DiskSet, error) {
			calls = append(calls, node)
			return domain.DiskSet{}, nil
		},
	})
	var openEffect application.Effect
	model, openEffect = application.Update(model, application.OpenDisks{Node: "cp-1"})
	require.NotNil(t, openEffect)
	openEffect(t.Context(), application.Dependencies{})

	_, effect := application.Update(model, application.RefreshDisks{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	require.Len(t, calls, 2, "OpenDisks fetches once, RefreshDisks fetches again")
	assert.Equal(t, "cp-1", calls[1])
}

func TestDisksFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1, Disks: application.DisksState{Node: "cp-1"}}

	model, effect := application.Update(model, application.DisksFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Disks.Status)
	assert.Equal(t, "disks unavailable", model.Disks.Err)
	assert.Equal(t, "cp-1", model.Disks.Node, "failure must not lose track of which node was open")
}

func TestSelectContextResetsDisks(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Disks: application.DisksState{
		Status: application.Ready,
		Node:   "cp-1",
		Value:  domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "sda"}}},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.DisksState{}, model.Disks)
}

// withDiskReader seeds a Model's unexported diskReader field via the
// SessionOpened message, since application.Model has no exported setter and
// this test file is package application_test (no access to unexported
// fields) — the same technique withProcessReader already uses for processReader.
func withDiskReader(model application.Model, reader ports.DiskReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Disks: reader})
	return model
}

func TestOpenNetworkLoadsTheRequestedNode(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withNetworkReader(model, &testkit.FakeNetworkReader{
		ListFunc: func(_ context.Context, node string) (domain.NetworkSet, error) {
			assert.Equal(t, "cp-1", node)
			return domain.NetworkSet{Links: []domain.LinkSnapshot{{Name: "eth0"}}}, nil
		},
	})

	model, effect := application.Update(model, application.OpenNetwork{Node: "cp-1"})
	assert.Equal(t, application.Loading, model.Network.Status)
	assert.Equal(t, "cp-1", model.Network.Node)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.NetworkLoaded)
	require.True(t, ok)
	assert.Len(t, loaded.Network.Links, 1)
}

// withNetworkReader seeds a Model's unexported networkReader field via the
// SessionOpened message, since application.Model has no exported setter and
// this test file is package application_test (no access to unexported
// fields) — the same technique withDiskReader already uses for diskReader.
func withNetworkReader(model application.Model, reader ports.NetworkReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Network: reader})
	return model
}

func TestRefreshNetworkRefetchesTheRememberedNode(t *testing.T) {
	calls := []string{}
	model := application.Model{Generation: 1}
	model = withNetworkReader(model, &testkit.FakeNetworkReader{
		ListFunc: func(_ context.Context, node string) (domain.NetworkSet, error) {
			calls = append(calls, node)
			return domain.NetworkSet{}, nil
		},
	})
	var openEffect application.Effect
	model, openEffect = application.Update(model, application.OpenNetwork{Node: "cp-1"})
	require.NotNil(t, openEffect)
	openEffect(t.Context(), application.Dependencies{})

	_, effect := application.Update(model, application.RefreshNetwork{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	require.Len(t, calls, 2, "OpenNetwork fetches once, RefreshNetwork fetches again")
	assert.Equal(t, "cp-1", calls[1])
}

func TestNetworkFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1, Network: application.NetworkState{Node: "cp-1"}}

	model, effect := application.Update(model, application.NetworkFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Network.Status)
	assert.Equal(t, "network unavailable", model.Network.Err)
	assert.Equal(t, "cp-1", model.Network.Node, "failure must not lose track of which node was open")
}

func TestSelectContextResetsNetwork(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Network: application.NetworkState{
		Status: application.Ready,
		Node:   "cp-1",
		Value:  domain.NetworkSet{Links: []domain.LinkSnapshot{{Name: "eth0"}}},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.NetworkState{}, model.Network)
}

func TestOpenResourceBrowserWithoutKindLoadsKindsOnly(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withResourceReaders(model, &testkit.FakeResourceKindReader{
		ListFunc: func(context.Context) (domain.ResourceKindSet, error) {
			return domain.ResourceKindSet{Kinds: []domain.ResourceKindSnapshot{{Type: "MachineStatuses.runtime.talos.dev", DisplayType: "MachineStatus"}}}, nil
		},
	}, nil)

	model, effect := application.Update(model, application.OpenResourceBrowser{})
	assert.Equal(t, application.Loading, model.ResourceBrowser.KindsStatus)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.ResourceKindsLoaded)
	require.True(t, ok)
	assert.Len(t, loaded.Kinds.Kinds, 1)
}

func withResourceReaders(model application.Model, kinds ports.ResourceKindReader, instances ports.ResourceInstanceReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ResourceKinds: kinds, Resources: instances})
	return model
}

func TestSelectResourceKindLoadsInstancesForTheChosenKind(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withResourceReaders(model, nil, &testkit.FakeResourceInstanceReader{
		ListFunc: func(_ context.Context, node, kind string) (domain.ResourceInstanceSet, error) {
			assert.Equal(t, "cp-1", node)
			assert.Equal(t, "MachineStatus", kind)
			return domain.ResourceInstanceSet{Instances: []domain.ResourceInstanceSnapshot{{ID: "machine"}}}, nil
		},
	})

	model, effect := application.Update(model, application.SelectResourceKind{Kind: "MachineStatus", Node: "cp-1"})
	assert.Equal(t, application.Loading, model.ResourceBrowser.InstancesStatus)
	assert.Equal(t, "MachineStatus", model.ResourceBrowser.SelectedKind)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.ResourceInstancesLoaded)
	require.True(t, ok)
	assert.Len(t, loaded.Instances.Instances, 1)
}

func TestOpenResourceInstanceLoadsTheSelectedKindAndNode(t *testing.T) {
	model := application.Model{Generation: 1, ResourceBrowser: application.ResourceBrowserState{SelectedKind: "MachineStatus", SelectedNode: "cp-1"}}
	model = withResourceReaders(model, nil, &testkit.FakeResourceInstanceReader{
		GetFunc: func(_ context.Context, node, kind, id string) (domain.ResourceInstanceSnapshot, error) {
			assert.Equal(t, "cp-1", node)
			assert.Equal(t, "MachineStatus", kind)
			assert.Equal(t, "machine", id)
			return domain.ResourceInstanceSnapshot{ID: id, YAML: "spec: {}"}, nil
		},
	})

	model, effect := application.Update(model, application.OpenResourceInstance{ID: "machine"})
	assert.Equal(t, application.Loading, model.ResourceBrowser.DetailStatus)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.ResourceInstanceLoaded)
	require.True(t, ok)
	assert.Equal(t, "spec: {}", loaded.Instance.YAML)
}

func TestResourceKindsFailedPreservesNothingToLose(t *testing.T) {
	model := application.Model{Generation: 1}

	model, effect := application.Update(model, application.ResourceKindsFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.ResourceBrowser.KindsStatus)
}

func TestResourceInstancesFailedPreservesSelectedKindAndNode(t *testing.T) {
	model := application.Model{Generation: 1, ResourceBrowser: application.ResourceBrowserState{SelectedKind: "MachineStatus", SelectedNode: "cp-1"}}

	model, effect := application.Update(model, application.ResourceInstancesFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.ResourceBrowser.InstancesStatus)
	assert.Equal(t, "MachineStatus", model.ResourceBrowser.SelectedKind, "failure must not lose track of which kind was open")
	assert.Equal(t, "cp-1", model.ResourceBrowser.SelectedNode)
}

func TestSelectContextResetsResourceBrowser(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", ResourceBrowser: application.ResourceBrowserState{
		KindsStatus: application.Ready, SelectedKind: "MachineStatus",
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.ResourceBrowserState{}, model.ResourceBrowser)
}

func TestSessionOpenResolvesKubernetesAndFetchesAfterEtcdLoads(t *testing.T) {
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
			}},
			ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) { return domain.ServiceSet{}, nil }},
			EventReader:   &testkit.FakeEventReader{ListFunc: func(context.Context) (domain.EventSet, error) { return domain.EventSet{}, nil }},
			EtcdReader:    &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) { return domain.EtcdSet{}, nil }},
		}, nil
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
		KubernetesResolver: &testkit.FakeKubernetesResolver{ResolveFunc: func(_ context.Context, talosContext string) (ports.KubernetesNodeReader, error) {
			assert.Equal(t, "prod", talosContext)
			return &testkit.FakeKubernetesNodeReader{ListFunc: func(context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
				return map[string]domain.KubernetesNodeSnapshot{"cp-1": {KubeletVersion: "v1.31.0"}}, nil
			}}, nil
		}},
	})
	t.Cleanup(func() { require.NoError(t, runner.Close()) })

	model, effect := application.NewModel("prod")
	contextsMessage := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, contextsMessage)
	sessionMessage := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, sessionMessage)
	nodesMessage := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, nodesMessage)
	servicesMessage := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, servicesMessage)
	eventsMessage := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, eventsMessage)
	etcdMessage := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, etcdMessage)
	require.NotNil(t, effect)
	kubernetesMessage := runner.Run(context.Background(), effect)
	model, _ = application.Update(model, kubernetesMessage)

	assert.Equal(t, application.Ready, model.Kubernetes.Status)
	assert.Equal(t, "v1.31.0", model.Kubernetes.Nodes["cp-1"].KubeletVersion)
	assert.NotNil(t, model.Nodes.Value.Nodes[0].KubernetesNode, "the eager chain must also have merged correlation onto the node")
}

func TestEventsLoadedFetchesKubernetesNodesWhenNoControlPlaneNodesExist(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withKubernetesReader(model, &testkit.FakeKubernetesNodeReader{
		ListFunc: func(context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
			return map[string]domain.KubernetesNodeSnapshot{}, nil
		},
	})

	_, effect := application.Update(model, application.EventsLoaded{Generation: 1, Events: domain.EventSet{}})

	require.NotNil(t, effect)
	message := effect(t.Context(), application.Dependencies{})
	_, ok := message.(application.KubernetesNodesLoaded)
	assert.True(t, ok)
}

func TestKubernetesNodesFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1, Kubernetes: application.KubernetesState{Available: true}}

	model, effect := application.Update(model, application.KubernetesNodesFailed{Generation: 1, Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Kubernetes.Status)
	assert.True(t, model.Kubernetes.Available, "a fetch failure must not clear Available — the association itself still holds")
}

func TestRefreshKubernetesNodesDoesNothingWhenUnavailable(t *testing.T) {
	model := application.Model{Generation: 1}

	_, effect := application.Update(model, application.RefreshKubernetesNodes{})

	assert.Nil(t, effect)
}

func TestMergeKubernetesCorrelationSetsKubernetesNodeAndReadiness(t *testing.T) {
	model := application.Model{
		Generation: 1,
		Nodes: application.NodeState{Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1"},
			{ID: "n2", Name: "unmatched"},
		}}},
	}
	model = withKubernetesReader(model, &testkit.FakeKubernetesNodeReader{
		ListFunc: func(context.Context) (map[string]domain.KubernetesNodeSnapshot, error) {
			return map[string]domain.KubernetesNodeSnapshot{
				"cp-1": {Conditions: []domain.KubernetesCondition{{Type: "Ready", Status: "True"}}},
			}, nil
		},
	})

	model, _ = application.Update(model, application.KubernetesNodesLoaded{Generation: 1, Nodes: map[string]domain.KubernetesNodeSnapshot{
		"cp-1": {Conditions: []domain.KubernetesCondition{{Type: "Ready", Status: "True"}}},
	}})

	require.NotNil(t, model.Nodes.Value.Nodes[0].KubernetesNode)
	assert.Equal(t, domain.KubernetesReady, model.Nodes.Value.Nodes[0].Kubernetes)
	assert.Nil(t, model.Nodes.Value.Nodes[1].KubernetesNode, "an unmatched node's KubernetesNode must stay nil")
}

func withKubernetesReader(model application.Model, reader ports.KubernetesNodeReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, KubernetesNodes: reader})
	return model
}

func TestRequestActionComputesControlPlaneQuorumWarning(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
		{Name: "cp-2", Role: domain.NodeRoleControl},
		{Name: "cp-3", Role: domain.NodeRoleControl},
	}}}
	model.Etcd = application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1"}, {Hostname: "cp-2"}, {Hostname: "cp-3"},
	}}}

	got, effect := application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"cp-1", "cp-2"}})

	require.NotNil(t, got.PendingAction)
	assert.Equal(t, []string{"cp-1", "cp-2"}, got.PendingAction.Targets)
	assert.Contains(t, got.PendingAction.Warning, "below quorum")
	assert.Nil(t, effect)
}

func TestRequestActionCountsAlreadyUnhealthyEtcdMembersTowardQuorumLoss(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
		{Name: "cp-2", Role: domain.NodeRoleControl},
		{Name: "cp-3", Role: domain.NodeRoleControl},
	}}}
	// cp-1 is already unhealthy (status unknown) before any action is
	// requested; rebooting the additional, currently-healthy cp-2 should
	// still be flagged as dropping etcd below quorum (1 remaining of 3, need
	// 2), not silently degrade to the bare control-plane message.
	model.Etcd = application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", StatusKnown: false},
		{Hostname: "cp-2", StatusKnown: true},
		{Hostname: "cp-3", StatusKnown: true},
	}}}

	got, _ := application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"cp-2"}})

	require.NotNil(t, got.PendingAction)
	assert.Contains(t, got.PendingAction.Warning, "below quorum")
}

func TestRequestActionNoWarningForWorkerOnlyTargets(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "worker-1", Role: domain.NodeRoleWorker},
	}}}

	got, _ := application.Update(model, application.RequestAction{Kind: application.ActionShutdown, Targets: []string{"worker-1"}})

	require.NotNil(t, got.PendingAction)
	assert.Empty(t, got.PendingAction.Warning)
}

func TestRequestActionWarnsUnknownQuorumWhenEtcdNotLoaded(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
	}}}

	got, _ := application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"cp-1"}})

	require.NotNil(t, got.PendingAction)
	assert.Contains(t, got.PendingAction.Warning, "unknown")
}

func TestRequestActionIsInertWhenWritesDisabled(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = false
	model.Nodes = application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl},
	}}}

	got, effect := application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"cp-1"}})

	assert.Nil(t, got.PendingAction, "RequestAction must be inert at the reducer level when WritesEnabled is false, regardless of what the TUI layer sent")
	assert.Nil(t, effect)
}

func TestCancelPendingActionClearsWithoutEffect(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.PendingAction = &application.PendingAction{Kind: application.ActionReboot, Targets: []string{"cp-1"}}

	got, effect := application.Update(model, application.CancelPendingAction{})

	assert.Nil(t, got.PendingAction)
	assert.Nil(t, effect)
}

func TestConfirmPendingActionClearsPendingLeavingEffectsToCaller(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.PendingAction = &application.PendingAction{Kind: application.ActionReboot, Targets: []string{"cp-1"}}

	got, effect := application.Update(model, application.ConfirmPendingAction{})

	assert.Nil(t, got.PendingAction)
	assert.Nil(t, effect)
}

func TestConfirmPendingActionSetsActionTotalFromTargetCount(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.PendingAction = &application.PendingAction{Kind: application.ActionReboot, Targets: []string{"cp-1", "cp-2", "worker-1"}}

	got, effect := application.Update(model, application.ConfirmPendingAction{})

	assert.Nil(t, got.PendingAction)
	assert.Equal(t, 3, got.ActionTotal)
	assert.Nil(t, effect)
}

func TestSelectContextClearsPendingActionAndResults(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.PendingAction = &application.PendingAction{Kind: application.ActionReboot, Targets: []string{"cp-1"}}
	model.ActionResults = []application.ActionResult{{Target: "cp-1"}}

	got, _ := application.Update(model, application.SelectContext{Name: "dev"})

	assert.Nil(t, got.PendingAction)
	assert.Empty(t, got.ActionResults)
}

func TestActionSucceededAppendsResultForCurrentGeneration(t *testing.T) {
	model, _ := application.NewModel("prod")

	got, effect := application.Update(model, application.ActionSucceeded{Generation: model.Generation, Target: "cp-1"})

	require.Len(t, got.ActionResults, 1)
	assert.Equal(t, "cp-1", got.ActionResults[0].Target)
	assert.Empty(t, got.ActionResults[0].Err)
	assert.Nil(t, effect)
}

func TestActionFailedAppendsErrorResultForCurrentGeneration(t *testing.T) {
	model, _ := application.NewModel("prod")

	got, _ := application.Update(model, application.ActionFailed{Generation: model.Generation, Target: "cp-1", Err: errors.New("unreachable")})

	require.Len(t, got.ActionResults, 1)
	assert.Equal(t, "unreachable", got.ActionResults[0].Err)
}

func TestActionResultIgnoredFromOlderGeneration(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.Generation = 2

	got, _ := application.Update(model, application.ActionSucceeded{Generation: 1, Target: "cp-1"})

	assert.Empty(t, got.ActionResults)
}
