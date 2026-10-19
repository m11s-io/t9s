package tui

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tuiLogStream struct {
	batches []domain.LogBatch
	closes  atomic.Int32
	next    func(context.Context) (domain.LogBatch, error)
}

func (s *tuiLogStream) Next(ctx context.Context) (domain.LogBatch, error) {
	if s.next != nil {
		return s.next(ctx)
	}
	batch := s.batches[0]
	s.batches = s.batches[1:]
	return batch, nil
}
func (s *tuiLogStream) Close() error { s.closes.Add(1); return nil }

type tuiLogReader struct {
	stream ports.ServiceLogStream
	open   func(context.Context, domain.LogRequest) (ports.ServiceLogStream, error)
}

func (r tuiLogReader) Open(ctx context.Context, request domain.LogRequest) (ports.ServiceLogStream, error) {
	if r.open != nil {
		return r.open(ctx, request)
	}
	return r.stream, nil
}

func runStreamCommand(t *testing.T, model directModel, command tea.Cmd) (directModel, tea.Cmd) {
	t.Helper()
	require.NotNil(t, command)
	message := command()
	return updateDirect(model, message)
}

func TestDirectServiceLogsOpenNextAndPreserveCleanEOF(t *testing.T) {
	stream := &tuiLogStream{batches: []domain.LogBatch{{Lines: []string{"service started"}}, {Lines: []string{"finished"}, EOF: true}}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{LogReader: tuiLogReader{stream: stream}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	command := model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"})
	model, command = runStreamCommand(t, model, command)
	model.logs = model.logs.update(keyPress('s'))
	model.logs = model.logs.update(keyPress('w'))
	model.logs = model.logs.update(keyPress('/'))
	for _, key := range []rune("finished") {
		model.logs = model.logs.update(keyPress(key))
	}
	model.logs = model.logs.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model, command = runStreamCommand(t, model, command)
	model, command = runStreamCommand(t, model, command)
	require.Nil(t, command)
	assert.Equal(t, []string{"service started", "finished"}, model.logs.state.Lines)
	assert.Equal(t, []string{"finished"}, model.logs.visibleLines())
	assert.False(t, model.logs.following)
	assert.True(t, model.logs.wrap)
	assert.True(t, model.logs.state.EOF)
	assert.Equal(t, int32(1), stream.closes.Load())
}

type tuiDmesgStream struct{ closes atomic.Int32 }

func (s *tuiDmesgStream) Next(context.Context) (domain.DmesgBatch, error) {
	return domain.DmesgBatch{Lines: []string{"kernel"}}, io.EOF
}
func (s *tuiDmesgStream) Close() error { s.closes.Add(1); return nil }

type tuiDmesgReader struct {
	stream ports.DmesgStream
	open   func(context.Context, domain.DmesgRequest) (ports.DmesgStream, error)
}

func (r tuiDmesgReader) Open(ctx context.Context, request domain.DmesgRequest) (ports.DmesgStream, error) {
	if r.open != nil {
		return r.open(ctx, request)
	}
	return r.stream, nil
}

func TestDirectDmesgNextEOFClosesOnceAndRetainsTerminalLines(t *testing.T) {
	stream := &tuiDmesgStream{}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{DmesgReader: tuiDmesgReader{stream: stream}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	command := model.openDirectDmesg("node-a")
	model, command = runStreamCommand(t, model, command)
	model, command = runStreamCommand(t, model, command)
	require.Nil(t, command)
	assert.Equal(t, []string{"kernel"}, model.dmesg.state.Lines)
	assert.True(t, model.dmesg.state.EOF)
	assert.Equal(t, int32(1), stream.closes.Load())
}

func TestDirectLogViewEscapeCancelsNextAndClosesStreamOnce(t *testing.T) {
	stream := &tuiLogStream{batches: []domain.LogBatch{{Lines: []string{"unused"}}}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{LogReader: tuiLogReader{stream: stream}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	command := model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"})
	model, command = runStreamCommand(t, model, command)
	require.NotNil(t, command)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, viewNodes, model.views.top().Kind)
	assert.ErrorIs(t, model.logStream.request.ctx.Err(), context.Canceled)
	assert.Equal(t, int32(1), stream.closes.Load())
}

func TestDirectLateLogOpenAfterEscapeIsDisposedOnce(t *testing.T) {
	stream := &tuiLogStream{batches: []domain.LogBatch{{Lines: []string{"unused"}}}}
	entered := make(chan struct{})
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{LogReader: tuiLogReader{open: func(ctx context.Context, _ domain.LogRequest) (ports.ServiceLogStream, error) {
			close(entered)
			<-ctx.Done()
			return stream, nil
		}}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	open := model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"})
	result := make(chan tea.Msg, 1)
	go func() { result <- open() }()
	<-entered
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	message := <-result
	model, _ = updateDirect(model, message)
	assert.Equal(t, viewNodes, model.views.top().Kind)
	assert.Equal(t, int32(1), stream.closes.Load())
}

func TestDirectLogNextCancellationClosesStreamAndPreservesPauseSettings(t *testing.T) {
	entered := make(chan struct{})
	stream := &tuiLogStream{next: func(ctx context.Context) (domain.LogBatch, error) {
		close(entered)
		<-ctx.Done()
		return domain.LogBatch{Lines: []string{"last frame"}}, ctx.Err()
	}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{LogReader: tuiLogReader{stream: stream}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	open := model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"})
	model, next := runStreamCommand(t, model, open)
	model.logs = model.logs.update(keyPress('s'))
	model.logs = model.logs.update(keyPress('w'))
	paused := model.logs.following
	result := make(chan tea.Msg, 1)
	go func() { result <- next() }()
	<-entered
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	<-result
	assert.False(t, paused)
	assert.False(t, model.logs.following)
	assert.True(t, model.logs.wrap)
	assert.Equal(t, int32(1), stream.closes.Load())
}

func TestDirectLogStreamRetentionAndClear(t *testing.T) {
	lines := make([]string, 5001)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d", i)
	}
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	request, _ := model.requests.begin("service-logs", model.generation())
	model.logStream = directLogStreamState{request: request, status: application.Ready}
	model.views = model.views.push(viewFrame{Kind: viewServiceLogs, Label: "api@node-a > logs"})
	model, _ = updateDirect(model, directServiceLogBatch{request: request.stamp, batch: domain.LogBatch{Lines: lines}})
	assert.Len(t, model.logs.state.Lines, 5000)
	assert.Equal(t, "line-1", model.logs.state.Lines[0])
	model, clear := updateDirect(model, keyPress('C'))
	require.NotNil(t, clear)
	model, _ = updateDirect(model, clear())
	assert.Empty(t, model.logs.state.Lines)
}

func TestDirectServiceLogReconnectUsesOriginalServiceIdentity(t *testing.T) {
	var targets []domain.LogRequest
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{LogReader: tuiLogReader{open: func(_ context.Context, request domain.LogRequest) (ports.ServiceLogStream, error) {
			targets = append(targets, request)
			return &tuiLogStream{batches: []domain.LogBatch{{Lines: []string{"line"}, EOF: true}}}, nil
		}}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model, command := runStreamCommand(t, model, model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"}))
	model, _ = runStreamCommand(t, model, command)
	model, command = updateDirect(model, keyPress('r'))
	model, command = runStreamCommand(t, model, command)
	require.Len(t, targets, 2)
	assert.Equal(t, domain.LogRequest{Node: "node-a", Service: "api"}, targets[0])
	assert.Equal(t, targets[0], targets[1])
	assert.Equal(t, viewServiceLogs, model.views.top().Kind)
	assert.NotNil(t, command)
}

func TestDirectPaletteNavigationClosesLogStream(t *testing.T) {
	stream := &tuiLogStream{batches: []domain.LogBatch{{Lines: []string{"line"}}}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{LogReader: tuiLogReader{stream: stream}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	model := newDirectModel(context.Background(), client, LaunchOptions{})
	model.session, model.sessionCtx = session, context.Background()
	model, command := runStreamCommand(t, model, model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"}))
	model, _ = runStreamCommand(t, model, command)
	model.runClusterCommand("nodes")
	assert.Equal(t, viewNodes, model.views.top().Kind)
	assert.ErrorIs(t, model.logStream.request.ctx.Err(), context.Canceled)
	assert.Equal(t, int32(1), stream.closes.Load())
}

func TestDirectPaletteNavigationClosesStreamBehindHelp(t *testing.T) {
	for _, kind := range []viewKind{viewServiceLogs, viewDmesg, viewEvents, viewClusterHealth} {
		t.Run(frameLabel(kind), func(t *testing.T) {
			model := newDirectModel(context.Background(), nil, LaunchOptions{})
			var request directRequest
			var closeCount func() int32
			switch kind {
			case viewServiceLogs:
				request, _ = model.requests.begin("service-logs", model.generation())
				stream := &tuiLogStream{}
				closeCount = func() int32 { return stream.closes.Load() }
				model.logStream = directLogStreamState{request: request, stream: stream}
			case viewDmesg:
				request, _ = model.requests.begin("dmesg", model.generation())
				stream := &tuiDmesgStream{}
				closeCount = func() int32 { return stream.closes.Load() }
				model.dmesgStream = directDmesgStreamState{request: request, stream: stream}
			case viewEvents:
				request, _ = model.requests.begin("cluster-events-follow", model.generation())
				stream := &directEventStreamFake{}
				closeCount = func() int32 { return int32(stream.closed) }
				model.eventFollow = directEventFollowState{request: request, stream: stream, following: true}
			case viewClusterHealth:
				request, _ = model.requests.begin("cluster-health", model.generation())
				stream := &directHealthStreamFake{}
				closeCount = func() int32 { return int32(stream.closed) }
				model.healthStream = directHealthStreamState{request: request, stream: stream}
			}
			model.views = newViewStack(viewFrame{Kind: viewNodes, Label: "nodes"}).push(viewFrame{Kind: kind, Label: frameLabel(kind)})
			model, _ = updateDirect(model, keyPress('?'))
			assert.True(t, model.requests.current(request.stamp), "help retains the underlying stream request")

			model, _ = runDirectPaletteCommand(t, model, "nodes")
			assert.Equal(t, viewNodes, model.views.top().Kind)
			assert.False(t, model.requests.current(request.stamp), "navigation that removes the stream frame cancels its request")
			assert.Equal(t, int32(1), closeCount())
			switch kind {
			case viewServiceLogs:
				assert.Nil(t, model.logStream.stream)
			case viewDmesg:
				assert.Nil(t, model.dmesgStream.stream)
			case viewEvents:
				assert.False(t, model.eventFollow.following)
				assert.Nil(t, model.eventFollow.stream)
			case viewClusterHealth:
				assert.Nil(t, model.healthStream.stream)
			}
		})
	}
}

func TestDirectPendingLogAndDmesgOpenSurvivesHelpAndLateRemovedOpenClosesOnce(t *testing.T) {
	for _, kind := range []viewKind{viewServiceLogs, viewDmesg} {
		for _, exit := range []string{"help back", "help palette navigation"} {
			t.Run(frameLabel(kind)+"/"+exit, func(t *testing.T) {
				entered := make(chan struct{})
				release := make(chan struct{})
				var logStream *tuiLogStream
				var dmesgStream *tuiDmesgStream
				openSession := func(context.Context, string) (ports.Session, error) {
					adapter := &testkit.FakeSession{}
					if kind == viewServiceLogs {
						logStream = &tuiLogStream{}
						adapter.LogReader = tuiLogReader{open: func(context.Context, domain.LogRequest) (ports.ServiceLogStream, error) {
							close(entered)
							<-release
							return logStream, nil
						}}
					} else {
						dmesgStream = &tuiDmesgStream{}
						adapter.DmesgReader = tuiDmesgReader{open: func(context.Context, domain.DmesgRequest) (ports.DmesgStream, error) {
							close(entered)
							<-release
							return dmesgStream, nil
						}}
					}
					return adapter, nil
				}
				client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: openSession}})
				session, err := client.Open(context.Background(), "prod", application.Options{})
				require.NoError(t, err)
				model := newDirectModel(context.Background(), client, LaunchOptions{})
				model.session, model.sessionCtx = session, context.Background()
				var open tea.Cmd
				if kind == viewServiceLogs {
					open = model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"})
				} else {
					open = model.openDirectDmesg("node-a")
				}
				result := make(chan tea.Msg, 1)
				go func() { result <- open() }()
				<-entered
				model, _ = updateDirect(model, keyPress('?'))
				if exit != "help back" {
					model, _ = runDirectPaletteCommand(t, model, "nodes")
				}
				close(release)
				opened := <-result
				model, next := updateDirect(model, opened)
				if exit == "help back" {
					assert.Equal(t, viewHelp, model.views.top().Kind, "opened stream is applied while help remains on top")
					model, _ = updateDirect(model, keyPress('q'))
					assert.Equal(t, kind, model.views.top().Kind)
					assert.NotNil(t, next, "retained owner frame accepts the opened stream and continues reading")
					if kind == viewServiceLogs {
						assert.NotNil(t, model.logStream.stream)
						assert.Equal(t, application.Ready, model.logStream.status)
					} else {
						assert.NotNil(t, model.dmesgStream.stream)
						assert.Equal(t, application.Ready, model.dmesgStream.status)
					}
				} else if kind == viewServiceLogs {
					assert.Equal(t, int32(1), logStream.closes.Load(), "late result for a removed owner is disposed once")
				} else {
					assert.Equal(t, int32(1), dmesgStream.closes.Load(), "late result for a removed owner is disposed once")
				}
				_ = session.Close()
			})
		}
	}
}

func frameLabel(kind viewKind) string {
	switch kind {
	case viewServiceLogs:
		return "logs"
	case viewDmesg:
		return "dmesg"
	case viewEvents:
		return "events"
	case viewClusterHealth:
		return "health"
	default:
		return "unknown"
	}
}

func TestDirectLogStreamClosesOnContextSwitchAndQuit(t *testing.T) {
	for _, exit := range []string{"context switch", "quit"} {
		t.Run(exit, func(t *testing.T) {
			stream := &tuiLogStream{batches: []domain.LogBatch{{Lines: []string{"line"}}}}
			client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
				return &testkit.FakeSession{LogReader: tuiLogReader{stream: stream}}, nil
			}}})
			session, err := client.Open(context.Background(), "prod", application.Options{})
			require.NoError(t, err)
			sessionCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			model := newDirectModel(context.Background(), client, LaunchOptions{})
			model.session, model.sessionCtx, model.sessionCancel = session, sessionCtx, cancel
			model, command := runStreamCommand(t, model, model.openDirectServiceLogs(domain.ServiceSnapshot{Node: "node-a", Name: "api"}))
			model, _ = runStreamCommand(t, model, command)
			if exit == "context switch" {
				model.beginSessionOpen("staging")
			} else {
				model.cleanup(tea.Quit())()
			}
			assert.ErrorIs(t, sessionCtx.Err(), context.Canceled)
			assert.ErrorIs(t, model.logStream.request.ctx.Err(), context.Canceled)
			assert.Equal(t, int32(1), stream.closes.Load())
		})
	}
}

func TestDirectLogReconnectKeyIsTypedIntoActiveFilter(t *testing.T) {
	for _, kind := range []viewKind{viewServiceLogs, viewDmesg} {
		t.Run(map[viewKind]string{viewServiceLogs: "logs", viewDmesg: "dmesg"}[kind], func(t *testing.T) {
			model := newDirectModel(context.Background(), nil, LaunchOptions{})
			model.session = &application.Session{}
			model.sessionCtx = context.Background()
			model.views = model.views.replaceRoot(viewFrame{Kind: kind, Label: "stream"})
			model.logs.filtering = kind == viewServiceLogs
			model.dmesg.filtering = kind == viewDmesg
			updated, command := updateDirect(model, keyPress('r'))
			require.Nil(t, command)
			if kind == viewServiceLogs {
				assert.Equal(t, "r", updated.logs.filter)
				updated, command = updateDirect(updated, keyPress('C'))
				assert.Nil(t, command)
				assert.Equal(t, "rC", updated.logs.filter)
				assert.Empty(t, updated.logs.state.Lines)
			} else {
				assert.Equal(t, "r", updated.dmesg.filter)
				updated, command = updateDirect(updated, keyPress('C'))
				assert.Nil(t, command)
				assert.Equal(t, "rC", updated.dmesg.filter)
				assert.Empty(t, updated.dmesg.state.Lines)
			}
		})
	}
}

func TestDirectServiceDetailLogsUsePinnedFrameService(t *testing.T) {
	opened := domain.ServiceSnapshot{Node: "node-a", Name: "api"}
	other := domain.ServiceSnapshot{Node: "node-b", Name: "worker"}
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.session = &application.Session{}
	model.sessionCtx = context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{opened, other}}}
	model.services = newServicesModel().normalizeSelection(model.cluster.services.Value.Services, 1)
	model.services.filter = "work"
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServiceDetail, Label: "api@node-a", ServiceKey: serviceKey(opened)})
	model, command := updateDirect(model, keyPress('l'))
	require.NotNil(t, command)
	assert.Equal(t, viewServiceLogs, model.views.top().Kind)
	assert.Equal(t, domain.LogRequest{Node: opened.Node, Service: opened.Name}, model.logStream.target)
}

func TestDirectServiceDetailLogsDoNotRetargetWhenPinnedServiceDisappears(t *testing.T) {
	opened := domain.ServiceSnapshot{Node: "node-a", Name: "api"}
	other := domain.ServiceSnapshot{Node: "node-b", Name: "worker"}
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.session = &application.Session{}
	model.sessionCtx = context.Background()
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{other}}}
	model.services = newServicesModel().normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServiceDetail, Label: "api@node-a", ServiceKey: serviceKey(opened)})
	model, command := updateDirect(model, keyPress('l'))
	assert.Nil(t, command)
	assert.Equal(t, viewServiceDetail, model.views.top().Kind)
	assert.Contains(t, ansi.Strip(model.View().Content), "Service unavailable")
}

func TestDirectLogViewportScrollAndResizeKeepPosition(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%02d", i+1)
	}
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServiceLogs, Label: "api@node-a > logs"})
	model.logs = model.logs.setStreamState(application.Ready, lines, "", false)
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 60, Height: 14})
	model, _ = updateDirect(model, keyPress('G'))
	_ = model.View()
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyUp})
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "line-99")
	assert.NotContains(t, content, "line-01")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	content = ansi.Strip(model.View().Content)
	assert.Contains(t, content, "line-100")
	assert.NotContains(t, content, "line-01")
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 60, Height: 18})
	content = ansi.Strip(model.View().Content)
	assert.Contains(t, content, "line-92", "resize keeps the stored scroll offset")
	assert.NotContains(t, content, "line-01")
}
