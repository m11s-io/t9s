package tui

import (
	"context"
	"fmt"
	"strings"
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

func TestDiskUsageOpensForSelectedMount(t *testing.T) {
	reader := &tuiDiskUsageReader{stream: &tuiDiskUsageStream{}}
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.sessionCtx = context.Background()
	model.session = tuiDiskUsageSession(t, reader)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewMounts, NodeID: "worker-id", NodeTarget: "worker-1"})
	model.mounts = newDirectMountsModel().setResult("worker-1", domain.MountSet{Mounts: []domain.MountSnapshot{
		{MountedOn: "/mnt/first"},
		{MountedOn: "/mnt/selected"},
	}}, nil)
	model.mounts.control.SetCursor(1)

	model, command := updateDirect(model, keyPress('u'))
	require.Equal(t, viewDiskUsage, model.views.top().Kind)
	require.NotNil(t, command)
	opened := command()
	model, next := updateDirect(model, opened)
	require.NotNil(t, next)
	assert.Equal(t, domain.DiskUsageRequest{Node: "worker-1", Path: "/mnt/selected"}, reader.request)
	assert.Equal(t, "/mnt/selected", model.diskUsage.path)
}

func TestDiskUsageViewExitClosesOpenStream(t *testing.T) {
	stream := &tuiDiskUsageStream{}
	reader := &tuiDiskUsageReader{stream: stream}
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.sessionCtx = context.Background()
	model.session = tuiDiskUsageSession(t, reader)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewMounts, NodeID: "worker-id", NodeTarget: "worker-1"})
	model.mounts = newDirectMountsModel().setResult("worker-1", domain.MountSet{Mounts: []domain.MountSnapshot{{MountedOn: "/var"}}}, nil)

	model, open := updateDirect(model, keyPress('u'))
	model, _ = updateDirect(model, open())
	require.NotNil(t, model.diskUsage.stream)
	model, _ = updateDirect(model, keyPress('q'))
	assert.Equal(t, viewMounts, model.views.top().Kind)
	assert.True(t, stream.closed)
}

func TestDiskUsageKeepsEarlierEntryErrorsThroughCleanEOF(t *testing.T) {
	model := newDiskUsageModel("worker-1", "/var")

	assert.False(t, model.accept(domain.DiskUsageBatch{Rows: []domain.DiskUsageEntry{{Path: "/var/cache", Error: "permission denied"}}}, nil))
	assert.True(t, model.accept(domain.DiskUsageBatch{EOF: true}, nil))

	assert.Equal(t, directPartial, model.status)
	assert.Contains(t, model.viewSized(contentSize{Width: 100, Height: 10}), "SCAN PARTIAL")
}

func TestDiskUsageStreamContinuesUnderHelpOverlay(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewDiskUsage, NodeID: "node-id", NodeTarget: "worker-1", MountPath: "/var"})
	model.diskUsage = newDiskUsageModel("worker-1", "/var")
	request, _ := model.requests.begin(directDiskUsageSlot("node-id", "/var"), model.generation())
	stream := &tuiDiskUsageStream{}
	model, _ = updateDirect(model, keyPress('?'))

	model, next := updateDirect(model, directDiskUsageOpened{request: request, nodeID: "node-id", node: "worker-1", path: "/var", stream: stream})
	require.NotNil(t, next, "the active stream should continue while help is on top")
	model, _ = updateDirect(model, directDiskUsageBatch{request: request, nodeID: "node-id", node: "worker-1", path: "/var", stream: stream,
		batch: domain.DiskUsageBatch{Rows: []domain.DiskUsageEntry{{Path: "/var/cache", SizeBytes: 42}}, EOF: true},
	})
	model, _ = updateDirect(model, keyPress('q'))

	require.Len(t, model.diskUsage.rows, 1)
	assert.Equal(t, "/var/cache", model.diskUsage.rows[0].Path)
}

func TestDiskUsageCommandNavigationCancelsScan(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.views = model.views.replaceRoot(viewFrame{Kind: viewDiskUsage, NodeID: "node-id", NodeTarget: "worker-1", MountPath: "/var"})
	model.diskUsage = newDiskUsageModel("worker-1", "/var")
	request, _ := model.requests.beginWithContext(directDiskUsageSlot("node-id", "/var"), model.generation(), context.Background())
	stream := &tuiDiskUsageStream{}
	model.diskUsage.attach(stream)

	model.runClusterCommand("nodes")

	assert.ErrorIs(t, request.ctx.Err(), context.Canceled)
	assert.True(t, stream.closed)
	assert.Nil(t, model.diskUsage.stream)
}

func TestDiskUsagePartialBottomRowRemainsVisibleAtNormalTerminalHeight(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewDiskUsage, NodeID: "node-id", NodeTarget: "worker-1", MountPath: "/var"})
	model.diskUsage = newDiskUsageModel("worker-1", "/var")
	rows := make([]domain.DiskUsageEntry, 50)
	for index := range rows {
		rows[index] = domain.DiskUsageEntry{Path: fmt.Sprintf("/var/entry-%02d", index), SizeBytes: int64(index)}
	}
	model.diskUsage.accept(domain.DiskUsageBatch{Rows: rows, Err: "some entries unavailable", EOF: true}, nil)
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 100, Height: 24})
	model, _ = updateDirect(model, keyPress('G'))

	view := ansi.Strip(model.View().Content)
	assert.Contains(t, view, "/var/entry-49")
	for _, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 100)
	}
}

func tuiDiskUsageSession(t *testing.T, reader ports.DiskUsageReader) *application.Session {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{
		OpenFunc: func(context.Context, string) (ports.Session, error) {
			return &testkit.FakeSession{DiskUsageReader: reader}, nil
		},
	}})
	session, err := client.Open(context.Background(), "cluster", application.Options{})
	require.NoError(t, err)
	return session
}

type tuiDiskUsageReader struct {
	request domain.DiskUsageRequest
	stream  ports.DiskUsageStream
}

func (r *tuiDiskUsageReader) Open(_ context.Context, request domain.DiskUsageRequest) (ports.DiskUsageStream, error) {
	r.request = request
	return r.stream, nil
}

type tuiDiskUsageStream struct{ closed bool }

func (s *tuiDiskUsageStream) Next(context.Context) (domain.DiskUsageBatch, error) {
	return domain.DiskUsageBatch{EOF: true}, nil
}

func (s *tuiDiskUsageStream) Close() error {
	s.closed = true
	return nil
}
