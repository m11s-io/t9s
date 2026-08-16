package tui_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/m11s-io/t9s/internal/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialViewDoesNotOpenSession(t *testing.T) {
	var catalogCalls atomic.Int32
	var sessionCalls atomic.Int32
	openStarted := make(chan struct{}, 1)
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		catalogCalls.Add(1)
		return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(ctx context.Context, _ string) (ports.Session, error) {
		sessionCalls.Add(1)
		openStarted <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})
	model, _ := application.NewModel("")

	view := tui.New(model, runner).View().Content

	for _, text := range []string{
		"t9s",
		"Context:",
		"Inspect Talos clusters",
		"Loading nodes…",
	} {
		assert.Contains(t, view, text)
	}
	assert.Zero(t, catalogCalls.Load())
	assert.Zero(t, sessionCalls.Load())
	select {
	case <-openStarted:
		t.Fatal("initial View started the session factory")
	default:
	}
}

func TestServicesViewDoesNotInvokeTheServiceReader(t *testing.T) {
	var serviceCalls atomic.Int32
	runner := application.NewRunner(application.Dependencies{
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return &testkit.FakeSession{ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
				serviceCalls.Add(1)
				return domain.ServiceSet{}, nil
			}}}, nil
		}},
	})
	model := tui.New(application.Model{Services: application.ServiceState{
		Status: application.Ready,
		Value:  domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "node-alpha", Name: "etcd"}}},
	}}, runner)

	for _, message := range []tea.KeyPressMsg{
		{Code: ':', Text: ":"}, {Code: 's', Text: "s"}, {Code: 'e', Text: "e"}, {Code: 'r', Text: "r"},
		{Code: 'v', Text: "v"}, {Code: 'i', Text: "i"}, {Code: 'c', Text: "c"}, {Code: 'e', Text: "e"},
		{Code: 's', Text: "s"}, {Code: tea.KeyEnter},
	} {
		model, _ = model.Update(message)
	}

	assert.Contains(t, model.View().Content, "node-alpha")
	assert.Zero(t, serviceCalls.Load())
}

func TestServicesCommandRefreshesTheServiceSnapshot(t *testing.T) {
	var serviceCalls atomic.Int32
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return &testkit.FakeSession{
				NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
					return domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1"}}}, nil
				}},
				ServiceReader: &testkit.FakeServiceReader{ListFunc: func(context.Context) (domain.ServiceSet, error) {
					call := serviceCalls.Add(1)
					return domain.ServiceSet{Services: []domain.ServiceSnapshot{{Node: "cp-1", Name: "service-" + strconv.Itoa(int(call))}}}, nil
				}},
			}, nil
		}},
	})
	applicationModel, _ := application.NewModel("")
	model := tui.New(applicationModel, runner)

	command := startupApplicationCommand(t, model.Init())
	model, command = model.Update(command())
	model, command = model.Update(command())
	model, command = model.Update(command())
	model, _ = model.Update(command())
	require.Equal(t, int32(1), serviceCalls.Load())

	for _, message := range []tea.KeyPressMsg{
		{Code: ':', Text: ":"}, {Code: 's', Text: "s"}, {Code: 'e', Text: "e"}, {Code: 'r', Text: "r"},
		{Code: 'v', Text: "v"}, {Code: 'i', Text: "i"}, {Code: 'c', Text: "c"}, {Code: 'e', Text: "e"},
		{Code: 's', Text: "s"},
	} {
		model, _ = model.Update(message)
	}

	model, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	require.NotNil(t, command)
	assert.Equal(t, int32(1), serviceCalls.Load())
	model, _ = model.Update(command())
	assert.Equal(t, int32(2), serviceCalls.Load())
	assert.Contains(t, model.View().Content, "service-2")
}

func TestEffectsRunOnlyInsideCommands(t *testing.T) {
	var catalogCalls atomic.Int32
	var sessionCalls atomic.Int32
	var nodeCalls atomic.Int32
	catalog := &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
		catalogCalls.Add(1)
		return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
	}}
	factory := &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		sessionCalls.Add(1)
		return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			nodeCalls.Add(1)
			return domain.NodeSet{}, nil
		}}}, nil
	}}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: catalog,
		SessionFactory: factory,
	})
	applicationModel, _ := application.NewModel("")
	model := tui.New(applicationModel, runner)

	command := startupApplicationCommand(t, model.Init())
	assert.NotNil(t, command)
	assert.Zero(t, catalogCalls.Load())

	contextsMessage := command()
	assert.Equal(t, int32(1), catalogCalls.Load())
	assert.Zero(t, sessionCalls.Load())

	model, command = model.Update(contextsMessage)
	assert.NotNil(t, command)
	assert.Zero(t, sessionCalls.Load())

	sessionMessage := command()
	assert.Equal(t, int32(1), sessionCalls.Load())
	assert.Zero(t, nodeCalls.Load())

	model, command = model.Update(sessionMessage)
	assert.NotNil(t, command)
	assert.Zero(t, nodeCalls.Load())

	command()
	assert.Equal(t, int32(1), nodeCalls.Load())

	_, quit := model.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	require.NotNil(t, quit)
	assert.IsType(t, tea.QuitMsg{}, quit())
}

func startupApplicationCommand(t *testing.T, command tea.Cmd) tea.Cmd {
	t.Helper()
	require.NotNil(t, command)
	message := command()
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		return func() tea.Msg { return message }
	}
	require.NotEmpty(t, batch)
	return batch[0]
}

func TestFailureMessageRendersInVisibleShell(t *testing.T) {
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return nil, errors.New("talosconfig is missing")
		}},
	})
	applicationModel, _ := application.NewModel("")
	model := tui.New(applicationModel, runner)

	message := startupApplicationCommand(t, model.Init())()
	model, _ = model.Update(message)

	assert.Contains(t, model.View().Content, "talosconfig is missing")
}

func TestQuitKeysReturnQuitCommand(t *testing.T) {
	applicationModel, _ := application.NewModel("")
	for name, message := range map[string]tea.KeyPressMsg{
		"q":      {Text: "q", Code: 'q'},
		"ctrl-c": {Code: 'c', Mod: tea.ModCtrl},
	} {
		t.Run(name, func(t *testing.T) {
			model := tui.New(applicationModel, application.NewRunner(application.Dependencies{}))

			_, command := model.Update(message)

			assert.NotNil(t, command)
			assert.IsType(t, tea.QuitMsg{}, command())
		})
	}
}

func TestQuitCancelsBlockedCatalogEffect(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(ctx context.Context) ([]domain.ClusterContext, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}},
	})
	applicationModel, _ := application.NewModel("")
	model := tui.New(applicationModel, runner)
	effectResult := make(chan tea.Msg, 1)

	go func() { effectResult <- startupApplicationCommand(t, model.Init())() }()
	waitForSignal(t, started, "catalog start")

	_, quit := model.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	require.NotNil(t, quit)
	assert.IsType(t, tea.QuitMsg{}, quit())
	waitForSignal(t, canceled, "catalog cancellation")
	waitForMessage(t, effectResult, "catalog effect result")
}

func TestParentContextCancellationStopsBlockedCatalogEffect(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	parent, cancel := context.WithCancel(context.Background())
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(ctx context.Context) ([]domain.ClusterContext, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}},
	})
	applicationModel, _ := application.NewModel("")
	model := tui.NewWithContext(parent, applicationModel, runner)
	commands := model.Init()()
	batch, ok := commands.(tea.BatchMsg)
	require.True(t, ok)
	require.Len(t, batch, 3)
	effectResult := make(chan tea.Msg, 1)
	shutdownResult := make(chan tea.Msg, 1)

	go func() { effectResult <- batch[0]() }()
	go func() { shutdownResult <- batch[2]() }()
	waitForSignal(t, started, "catalog start")

	cancel()
	waitForSignal(t, canceled, "catalog cancellation")
	waitForMessage(t, effectResult, "catalog effect result")
	message := waitForMessage(t, shutdownResult, "context shutdown message")
	_, quit := model.Update(message)
	require.NotNil(t, quit)
	assert.IsType(t, tea.QuitMsg{}, quit())
}

func TestQuitClosesSessionInsideCommand(t *testing.T) {
	closed := make(chan struct{})
	session := &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{}, nil
		}},
		CloseFunc: func() error {
			close(closed)
			return nil
		},
	}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return session, nil
		}},
	})
	applicationModel, _ := application.NewModel("")
	model := tui.New(applicationModel, runner)

	model, command := model.Update(startupApplicationCommand(t, model.Init())())
	model, _ = model.Update(command())
	_, quit := model.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})

	assert.Zero(t, session.CloseCount())
	require.NotNil(t, quit)
	assert.IsType(t, tea.QuitMsg{}, quit())
	waitForSignal(t, closed, "session close")
}

func TestRuntimeErrorFallbackCancelsEffectsAndClosesSession(t *testing.T) {
	inputFailed := errors.New("terminal input failed")
	readStarted := make(chan struct{})
	readCanceled := make(chan struct{})
	closed := make(chan struct{})
	session := &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(ctx context.Context) (domain.NodeSet, error) {
			close(readStarted)
			<-ctx.Done()
			close(readCanceled)
			return domain.NodeSet{}, ctx.Err()
		}},
		CloseFunc: func() error {
			close(closed)
			return nil
		},
	}
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return session, nil
		}},
	})
	applicationModel, _ := application.NewModel("")
	model, cleanup := tui.NewWithCleanup(t.Context(), applicationModel, runner)
	var output bytes.Buffer
	program := tea.NewProgram(model,
		tea.WithInput(&errorAfterSignalReader{signal: readStarted, err: inputFailed}),
		tea.WithOutput(&output),
		tea.WithWindowSize(120, 40),
	)

	_, err := program.Run()

	require.ErrorContains(t, err, inputFailed.Error())
	assert.Zero(t, session.CloseCount())
	select {
	case <-readCanceled:
		t.Fatal("program runtime error canceled detached application effect before fallback")
	default:
	}

	require.NotNil(t, cleanup)
	cleanup()
	waitForSignal(t, readCanceled, "node read cancellation")
	waitForSignal(t, closed, "session close")
	cleanup()
	assert.Equal(t, 1, session.CloseCount())
}

type errorAfterSignalReader struct {
	signal <-chan struct{}
	err    error
}

func (r *errorAfterSignalReader) Read([]byte) (int, error) {
	<-r.signal
	return 0, r.err
}

var _ io.Reader = (*errorAfterSignalReader)(nil)

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForMessage(t *testing.T, messages <-chan tea.Msg, description string) tea.Msg {
	t.Helper()
	select {
	case message := <-messages:
		return message
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}
