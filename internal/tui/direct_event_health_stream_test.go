package tui

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type directEventStreamFake struct {
	batches []domain.EventSet
	closed  int
	started chan struct{}
}

func (s *directEventStreamFake) Next(ctx context.Context) (domain.EventSet, error) {
	if err := ctx.Err(); err != nil {
		return domain.EventSet{}, err
	}
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return domain.EventSet{}, ctx.Err()
	}
	if len(s.batches) == 0 {
		return domain.EventSet{}, io.EOF
	}
	batch := s.batches[0]
	s.batches = s.batches[1:]
	return batch, nil
}

func TestDirectFollowedEventsCancellationAndLateOpenCloseExactlyOnce(t *testing.T) {
	for _, exit := range []string{"palette navigation", "context switch", "quit"} {
		t.Run("blocked next on "+exit, func(t *testing.T) {
			stream := &directEventStreamFake{started: make(chan struct{}, 1)}
			session, _ := directStreamSession(t, &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }}, nil)
			model := newModel(context.Background(), nil, LaunchOptions{})
			model.session, model.sessionCtx = session, context.Background()
			model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
			model, command := updateDirect(model, keyPress('s'))
			model, command = runStreamCommand(t, model, command)
			done := make(chan tea.Msg, 1)
			go func() { done <- command() }()
			<-stream.started
			switch exit {
			case "palette navigation":
				model, _ = runDirectPaletteCommand(t, model, "nodes")
			case "context switch":
				model.cluster.contexts = []domain.ClusterContext{{Name: "staging"}}
				model, _ = updateDirect(model, directSelectContext{name: "staging"})
			case "quit":
				model, command = updateDirect(model, keyPress('q'))
				require.NotNil(t, command)
				command()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("canceled Next did not return")
			}
			assert.Equal(t, 1, stream.closed)
		})
	}

	t.Run("late open is disposed once", func(t *testing.T) {
		model := newModel(context.Background(), nil, LaunchOptions{})
		model.session, model.sessionCtx = &application.Session{}, context.Background()
		model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
		model, command := updateDirect(model, keyPress('s'))
		require.NotNil(t, command)
		request := model.eventFollow.request.stamp
		model.closeDirectEventFollow()
		stream := &directEventStreamFake{}
		model, _ = updateDirect(model, directEventFollowOpened{request: request, stream: stream})
		assert.Equal(t, 1, stream.closed)
	})
}
func (s *directEventStreamFake) Close() error { s.closed++; return nil }

type directHealthStreamFake struct {
	progress []domain.ClusterHealthProgress
	closed   int
}

func (s *directHealthStreamFake) Next(ctx context.Context) (domain.ClusterHealthProgress, error) {
	if err := ctx.Err(); err != nil {
		return domain.ClusterHealthProgress{}, err
	}
	if len(s.progress) == 0 {
		return domain.ClusterHealthProgress{EOF: true}, nil
	}
	item := s.progress[0]
	s.progress = s.progress[1:]
	return item, nil
}
func (s *directHealthStreamFake) Close() error { s.closed++; return nil }

func directStreamSession(t *testing.T, event ports.EventReader, health ports.ClusterHealthReader) (*application.Session, *application.Client) {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{EventReader: event, ClusterHealthReader: health}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	return session, client
}

func TestDirectFollowedEventsUpdatesSharedSnapshotAndStopsOnCleanEOF(t *testing.T) {
	stream := &directEventStreamFake{batches: []domain.EventSet{{Events: []domain.EventSnapshot{{Node: "node-a", Kind: "Sequence", Message: "new event"}}}}}
	session, _ := directStreamSession(t, &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }}, nil)
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	prior := make([]domain.EventSnapshot, 2000)
	for index := range prior {
		prior[index] = domain.EventSnapshot{Node: "node-a", Kind: "Sequence", Message: "old"}
	}
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: prior}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	model, command := updateDirect(model, keyPress('s'))
	require.NotNil(t, command, "s starts the direct event follow")
	model, command = runStreamCommand(t, model, command)
	model, command = runStreamCommand(t, model, command)
	model, command = runStreamCommand(t, model, command)
	assert.Nil(t, command, "clean EOF ends the follow loop")
	assert.Len(t, model.cluster.events.Value.Events, 2000)
	assert.Equal(t, "new event", model.cluster.events.Value.Events[1999].Message)
	assert.True(t, model.eventFollow.attempted)
	assert.False(t, model.eventFollow.following)
	assert.Equal(t, 1, stream.closed)
}

func TestDirectFollowedEventsKeepNewestRowsVisibleAfterOverflow(t *testing.T) {
	batch := make([]domain.EventSnapshot, 20)
	for index := range batch {
		batch[index] = domain.EventSnapshot{Node: "cp-1", Kind: "Sequence", Message: fmt.Sprintf("event-%02d", index)}
	}
	stream := &directEventStreamFake{batches: []domain.EventSet{{Events: batch}}}
	session, _ := directStreamSession(t, &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }}, nil)
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model.width, model.height = 100, 12
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: []domain.EventSnapshot{{Node: "cp-1", Kind: "Sequence", Message: "boot START"}}}}
	model.events = model.events.normalizeSelection(model.cluster.events.Value.Events, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	model, command := updateDirect(model, keyPress('s'))
	model, command = runStreamCommand(t, model, command)
	model, command = runStreamCommand(t, model, command)
	assert.NotNil(t, command, "follow remains active until it receives EOF")
	assert.Equal(t, eventKey(batch[len(batch)-1]), model.events.selectedKey)
	assert.Equal(t, len(batch), model.events.control.Cursor())
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "event-19")
	assert.NotContains(t, content, "boot START")
}

func TestDirectClusterHealthStreamPinsExactNodeRequestAndKeepsFailedVerdict(t *testing.T) {
	stream := &directHealthStreamFake{progress: []domain.ClusterHealthProgress{{Message: "control plane reachable"}, {Message: "cluster degraded", Err: "unhealthy members", EOF: true}}}
	var got domain.ClusterHealthRequest
	health := &testkit.FakeClusterHealthReader{OpenFunc: func(_ context.Context, request domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		got = request
		return stream, nil
	}}
	session, _ := directStreamSession(t, nil, health)
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Role: domain.NodeRoleControl, Name: "cp-name", Addresses: []string{"not-an-ip", "10.0.0.1"}},
		{Role: domain.NodeRoleWorker, Name: "worker-name", Addresses: []string{"10.0.0.2"}},
	}}}
	model, command := runDirectPaletteCommand(t, model, "healthcheck 30s")
	for command != nil {
		model, command = runStreamCommand(t, model, command)
	}
	assert.Equal(t, []string{"10.0.0.1"}, got.ControlPlaneNodes)
	assert.Equal(t, []string{"10.0.0.2"}, got.WorkerNodes)
	assert.Equal(t, 30*time.Second, got.WaitTimeout)
	assert.Equal(t, []string{"control plane reachable", "cluster degraded"}, model.healthcheck.state.Lines)
	assert.Equal(t, "unhealthy members", model.healthcheck.state.Err)
	assert.False(t, model.healthcheck.state.VerdictReady)
	assert.Equal(t, 1, stream.closed)
	assert.Contains(t, model.healthcheck.viewSized(contentSize{Width: 80, Height: 8}), "unhealthy members")
}

func TestDirectClusterHealthHealthyEOFShowsVerdictAndClosesStream(t *testing.T) {
	stream := &directHealthStreamFake{progress: []domain.ClusterHealthProgress{{Message: "all members reachable"}, {EOF: true}}}
	session, _ := directStreamSession(t, nil, &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		return stream, nil
	}})
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model, command := runDirectPaletteCommand(t, model, "healthcheck")
	for command != nil {
		model, command = runStreamCommand(t, model, command)
	}

	assert.True(t, model.healthcheck.state.VerdictReady)
	assert.Contains(t, ansi.Strip(model.View().Content), "cluster is healthy")
	assert.Equal(t, 1, stream.closed)
}

func TestDirectClusterHealthFilterShortcutsAndResizeUseStoredDisplayState(t *testing.T) {
	progress := make([]domain.ClusterHealthProgress, 12)
	for index := range progress {
		progress[index].Message = "progress line"
	}
	progress[len(progress)-1] = domain.ClusterHealthProgress{Message: "finished", EOF: true}
	stream := &directHealthStreamFake{progress: progress}
	health := &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		return stream, nil
	}}
	session, _ := directStreamSession(t, nil, health)
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model.width, model.height = 100, 20
	model, command := runDirectPaletteCommand(t, model, "healthcheck")
	for command != nil {
		model, command = runStreamCommand(t, model, command)
	}
	model, _ = updateDirect(model, keyPress('G'))
	model, _ = updateDirect(model, keyPress('k'))
	before := model.healthcheck.viewport.YOffset()
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 100, Height: 14})
	assert.Equal(t, before, model.healthcheck.viewport.YOffset())

	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune{'r', 'C'} {
		model, _ = updateDirect(model, keyPress(key))
	}
	assert.Equal(t, "rC", model.healthcheck.filter)
	assert.Equal(t, 12, len(model.healthcheck.state.Lines), "filter shortcuts must not clear while filtering")
}

func TestDirectHealthcheckReconnectRebuildsPinnedRequestFromCurrentNodes(t *testing.T) {
	var requests []domain.ClusterHealthRequest
	health := &testkit.FakeClusterHealthReader{OpenFunc: func(_ context.Context, request domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		requests = append(requests, request)
		return &directHealthStreamFake{}, nil
	}}
	session, _ := directStreamSession(t, nil, health)
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model.cluster.nodes = directNodeState{Status: directLoading}
	model, command := runDirectPaletteCommand(t, model, "healthcheck 45s")
	for command != nil {
		model, command = runStreamCommand(t, model, command)
	}
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Role: domain.NodeRoleControl, Addresses: []string{"10.1.0.1"}},
		{Role: domain.NodeRoleWorker, Addresses: []string{"10.1.0.2"}},
	}}}
	model, command = updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	for command != nil {
		model, command = runStreamCommand(t, model, command)
	}
	require.Len(t, requests, 2)
	assert.Equal(t, []string{"10.1.0.1"}, requests[1].ControlPlaneNodes)
	assert.Equal(t, []string{"10.1.0.2"}, requests[1].WorkerNodes)
	assert.Equal(t, 45*time.Second, requests[1].WaitTimeout)
}

func TestDirectHealthcheckColonKeyStaysInActiveFilter(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewClusterHealth, Label: "healthcheck"})
	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress(':'))
	assert.False(t, model.palette.active)
	assert.True(t, model.healthcheck.filtering)
	assert.Equal(t, ":", model.healthcheck.filter)
}

func runDirectPaletteCommand(t *testing.T, model model, value string) (model, tea.Cmd) {
	t.Helper()
	model, _ = updateDirect(model, keyPress(':'))
	for _, key := range []rune(value) {
		model, _ = updateDirect(model, keyPress(key))
	}
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		return model, nil
	}
	return updateDirect(model, command())
}
