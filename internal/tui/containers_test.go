package tui

import (
	"context"
	"strings"
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

type tuiContainerReader struct {
	requests []domain.ContainerRequest
	set      domain.ContainerSet
}

func (r *tuiContainerReader) List(_ context.Context, request domain.ContainerRequest) (domain.ContainerSet, error) {
	r.requests = append(r.requests, request)
	return domain.ContainerSet{Node: request.Node, Namespace: request.Namespace, Containers: append([]domain.ContainerSnapshot(nil), r.set.Containers...)}, nil
}

func (r *tuiContainerReader) OpenImages(context.Context, domain.ImageRequest) (ports.ImageStream, error) {
	return &tuiImageStream{}, nil
}

type tuiImageStream struct{ closed bool }

func (s *tuiImageStream) Next(context.Context) (domain.ImageBatch, error) {
	return domain.ImageBatch{EOF: true}, nil
}
func (s *tuiImageStream) Close() error { s.closed = true; return nil }

func TestContainersReadSelectedNodeAndCycleNamespaceWithoutStaleResult(t *testing.T) {
	reader := &tuiContainerReader{set: domain.ContainerSet{Containers: []domain.ContainerSnapshot{{Namespace: "system", ID: "sys-1", Name: "system-row"}}}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{ContainerReader: reader}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()

	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-a-id", Name: "node-a"}, {ID: "node-b-id", Name: "node-b"}})
	model.session, model.sessionCtx = session, context.Background()
	model, command := updateDirect(model, keyPress('c'))
	require.Equal(t, viewContainers, model.views.top().Kind)
	model, _ = applyDirectCommand(t, model, command)
	assert.Contains(t, model.View().Content, "system-row")
	require.Equal(t, []domain.ContainerRequest{{Node: "node-a", Namespace: domain.ContainerNamespaceSystem}}, reader.requests)

	old, _ := model.requests.begin(directContainerSlot("node-a-id"), model.generation())
	model, command = updateDirect(model, keyPress('n'))
	require.Equal(t, domain.ContainerNamespaceCRI, model.views.top().ContainerNamespace)
	assert.ErrorIs(t, old.ctx.Err(), context.Canceled)
	model, _ = updateDirect(model, directContainersLoaded{request: old.stamp, nodeID: "node-a-id", namespace: domain.ContainerNamespaceSystem, values: domain.ContainerSet{Containers: []domain.ContainerSnapshot{{Namespace: "system", ID: "stale", Name: "stale-system"}}}})
	assert.NotContains(t, model.View().Content, "stale-system")
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, domain.ContainerRequest{Node: "node-a", Namespace: domain.ContainerNamespaceCRI}, reader.requests[1])
	assert.Contains(t, model.View().Content, "system-row")
	assert.Contains(t, model.View().Content, "cri")
}

func TestContainerCPUTimeIsDisplayedAsDurationAndKeysStayReadOnly(t *testing.T) {
	row := domain.ContainerSnapshot{Namespace: "system", ID: "one", Name: "api", MemoryKnown: true, MemoryBytes: 10, CPUTimeKnown: true, CPUTime: time.Second}
	view := renderContainerRows(140, []domain.ContainerSnapshot{row}, 0)
	assert.Contains(t, view, "1s")
	assert.NotContains(t, view, "%")
	for _, hint := range actionHints(viewContainers, true) {
		assert.NotContains(t, []string{"R", "X", "S", "T", "restart", "stop"}, hint.Key)
	}
}

func TestContainerDetailFindsCRIContainerByRuntimeNamespace(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{
		Kind: viewContainers, NodeID: "node-id", NodeTarget: "worker-1",
		ContainerNamespace: domain.ContainerNamespaceCRI,
	})
	model.containers = newContainersModel(domain.ContainerNamespaceCRI, false).setContainers(domain.ContainerSet{Containers: []domain.ContainerSnapshot{{
		Namespace: "k8s.io", ID: "runtime-id", Name: "cri-container",
	}}}, nil)
	model, _ = updateDirect(model, keyPress('d'))
	require.Equal(t, viewContainerDetail, model.views.top().Kind)
	assert.Equal(t, "k8s.io", model.views.top().ContainerRuntimeNamespace)

	content := model.View().Content
	assert.Contains(t, content, "cri-container")
	assert.NotContains(t, content, "unavailable")
}

func TestContainerResultsSurviveHelpOverlay(t *testing.T) {
	t.Run("container list", func(t *testing.T) {
		model := newModel(context.Background(), nil, LaunchOptions{})
		model.splash = false
		model.views = model.views.replaceRoot(viewFrame{Kind: viewContainers, NodeID: "node-id", NodeTarget: "worker-1", ContainerNamespace: domain.ContainerNamespaceSystem})
		model.containers = newContainersModel(domain.ContainerNamespaceSystem, false)
		request, _ := model.requests.begin(directContainerSlot("node-id"), model.generation())
		model, _ = updateDirect(model, keyPress('?'))

		model, _ = updateDirect(model, directContainersLoaded{request: request.stamp, nodeID: "node-id", namespace: domain.ContainerNamespaceSystem,
			values: domain.ContainerSet{Node: "worker-1", Namespace: domain.ContainerNamespaceSystem, Containers: []domain.ContainerSnapshot{{Namespace: "system", ID: "runtime-id", Name: "result-under-help"}}},
		})
		model, _ = updateDirect(model, keyPress('q'))

		require.Len(t, model.containers.values.Containers, 1)
		assert.Equal(t, "result-under-help", model.containers.values.Containers[0].Name)
	})

	t.Run("image stream", func(t *testing.T) {
		model := newModel(context.Background(), nil, LaunchOptions{})
		model.splash = false
		model.views = model.views.replaceRoot(viewFrame{Kind: viewContainers, NodeID: "node-id", NodeTarget: "worker-1", ContainerNamespace: domain.ContainerNamespaceCRI, ImageMode: true})
		model.containers = newContainersModel(domain.ContainerNamespaceCRI, true)
		request, _ := model.requests.begin(directContainerSlot("node-id"), model.generation())
		stream := &tuiImageStream{}
		model, _ = updateDirect(model, keyPress('?'))

		model, next := updateDirect(model, directImagesOpened{request: request, nodeID: "node-id", namespace: domain.ContainerNamespaceCRI, stream: stream})
		require.NotNil(t, next, "the active image stream should continue while help is on top")
		model, _ = updateDirect(model, directImagesBatch{request: request, nodeID: "node-id", namespace: domain.ContainerNamespaceCRI, stream: stream,
			batch: domain.ImageBatch{Images: []domain.ImageSnapshot{{Reference: "image-under-help"}}, EOF: true},
		})
		model, _ = updateDirect(model, keyPress('q'))

		require.Len(t, model.containers.images, 1)
		assert.Equal(t, "image-under-help", model.containers.images[0].Reference)
	})
}

func TestImageDetailShowsCapturedReferenceDigestAndSize(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewContainers, NodeID: "node-id", NodeTarget: "worker-1", ContainerNamespace: domain.ContainerNamespaceCRI, ImageMode: true})
	model.containers = newContainersModel(domain.ContainerNamespaceCRI, true)
	reference := "registry.example:5000/team/very-long-image-name-that-must-wrap:release-2026"
	digest := "sha256:" + strings.Repeat("a", 64)
	model.containers.images = []domain.ImageSnapshot{{Reference: reference, Digest: digest, SizeBytes: 2048}}
	model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 80, Height: 24})

	model, _ = updateDirect(model, keyPress('d'))

	require.Equal(t, viewContainerDetail, model.views.top().Kind)
	assert.True(t, model.views.top().ImageMode)
	content := ansi.Strip(model.View().Content)
	joined := strings.Join(strings.Fields(strings.ReplaceAll(content, "│", " ")), "")
	assert.Contains(t, joined, reference)
	assert.Contains(t, joined, digest)
	assert.Contains(t, content, formatBytes(2048))
	for _, line := range strings.Split(content, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 80)
	}
}
