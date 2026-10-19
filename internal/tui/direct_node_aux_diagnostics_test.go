package tui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectSocketsMountsAndMemoryReadAndRefreshThroughSession(t *testing.T) {
	reads := map[string]int{}
	var gotTargets []string
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NetstatReader: &testkit.FakeNetstatReader{ListFunc: func(_ context.Context, node string) (domain.SocketSet, error) {
				reads["sockets"]++
				gotTargets = append(gotTargets, node)
				return domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp", LocalAddress: fmt.Sprintf("127.0.0.1:%d", reads["sockets"]), ProcessName: "daemon"}}}, nil
			}},
			MountReader: &testkit.FakeMountReader{ListFunc: func(_ context.Context, node string) (domain.MountSet, error) {
				reads["mounts"]++
				gotTargets = append(gotTargets, node)
				return domain.MountSet{Mounts: []domain.MountSnapshot{{Filesystem: fmt.Sprintf("/dev/disk%d", reads["mounts"]), MountedOn: "/data"}}}, nil
			}},
			MemoryReader: &testkit.FakeMemoryReader{ListFunc: func(_ context.Context, node string) (domain.MemorySnapshot, error) {
				reads["memory"]++
				gotTargets = append(gotTargets, node)
				return domain.MemorySnapshot{TotalBytes: 1024, UsedBytes: uint64(reads["memory"]) * 128}, nil
			}},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()

	for _, test := range []struct {
		key     rune
		kind    viewKind
		scope   string
		initial string
		refresh string
	}{
		{key: 's', kind: viewNetstat, scope: "sockets", initial: "127.0.0.1:1", refresh: "127.0.0.1:2"},
		{key: 'm', kind: viewMounts, scope: "mounts", initial: "/dev/disk1", refresh: "/dev/disk2"},
		{key: 'f', kind: viewMemory, scope: "memory", initial: "128 B", refresh: "256 B"},
	} {
		t.Run(test.scope, func(t *testing.T) {
			model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
			model.session, model.sessionCtx = session, context.Background()
			model, command := updateDirect(model, keyPress(test.key))
			require.Equal(t, test.kind, model.views.top().Kind)
			require.NotNil(t, command)
			assert.Equal(t, directLoading, directAuxStatus(model, test.kind))
			model, _ = applyDirectCommand(t, model, command)
			assert.Contains(t, model.View().Content, test.initial)

			model, command = updateDirect(model, keyPress('r'))
			require.NotNil(t, command)
			model, _ = applyDirectCommand(t, model, command)
			assert.Equal(t, directReady, directAuxStatus(model, test.kind))
			assert.Contains(t, model.View().Content, test.refresh)
			assert.Equal(t, 2, reads[test.scope])
		})
	}
	assert.Equal(t, []string{"worker-1", "worker-1", "worker-1", "worker-1", "worker-1", "worker-1"}, gotTargets)
}

func TestDirectAuxDiagnosticPartialFailureIsNormalizedAndKeepsValues(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.views = model.views.push(viewFrame{Kind: viewNetstat, Label: "worker-1 > netstat", NodeID: "worker-id", NodeTarget: "worker-1"})
	request, _ := model.requests.begin(directDiagnosticSlot("worker-id", "sockets"), model.generation())
	model, _ = updateDirect(model, directSocketsLoaded{request: request.stamp, nodeID: "worker-id", node: "worker-1", sockets: domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp", LocalAddress: "kept"}}}, err: errors.New("secret transport detail")})
	assert.Equal(t, directPartial, model.netstat.status)
	assert.Contains(t, model.View().Content, "kept")
	assert.Contains(t, model.View().Content, "Some socket data is unavailable")
	assert.NotContains(t, model.View().Content, "secret transport detail")
}

func TestDirectAuxDiagnosticPartialFailureAcrossAllScopes(t *testing.T) {
	for _, test := range []struct {
		kind  viewKind
		scope string
		row   string
	}{
		{kind: viewNetstat, scope: "sockets", row: "kept-socket"},
		{kind: viewMounts, scope: "mounts", row: "kept-mount"},
		{kind: viewMemory, scope: "memory", row: "1.0 KiB"},
	} {
		t.Run(test.scope, func(t *testing.T) {
			model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
			model.views = model.views.push(viewFrame{Kind: test.kind, Label: test.scope, NodeID: "worker-id", NodeTarget: "worker-1"})
			request, _ := model.requests.begin(directDiagnosticSlot("worker-id", test.scope), model.generation())
			model = applyAuxPartial(model, test.kind, request.stamp)
			assert.Equal(t, directPartial, directAuxStatus(model, test.kind))
			assert.Contains(t, model.View().Content, test.row)
			assert.Contains(t, model.View().Content, "PARTIAL")
			assert.NotContains(t, model.View().Content, "private transport")
		})
	}
}

func TestDirectAuxDiagnosticRejectsStaleRefreshAndCancelsOnExit(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.views = model.views.push(viewFrame{Kind: viewMounts, Label: "worker-1 > mounts", NodeID: "worker-id", NodeTarget: "worker-1"})
	old, _ := model.requests.begin(directDiagnosticSlot("worker-id", "mounts"), model.generation())
	newer, _ := model.requests.begin(directDiagnosticSlot("worker-id", "mounts"), model.generation())
	model, _ = updateDirect(model, directMountsLoaded{request: old.stamp, nodeID: "worker-id", node: "worker-1", mounts: domain.MountSet{Mounts: []domain.MountSnapshot{{Filesystem: "stale"}}}})
	assert.NotContains(t, model.View().Content, "stale")
	model, _ = updateDirect(model, directMountsLoaded{request: newer.stamp, nodeID: "worker-id", node: "worker-1", mounts: domain.MountSet{Mounts: []domain.MountSnapshot{{Filesystem: "fresh"}}}})
	assert.Contains(t, model.View().Content, "fresh")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.False(t, model.requests.current(newer.stamp))
}

func TestDirectAuxDiagnosticEmptyAndFailedResults(t *testing.T) {
	for _, test := range []struct {
		kind   viewKind
		scope  string
		empty  string
		failed string
	}{
		{kind: viewNetstat, scope: "sockets", empty: "No sockets", failed: "Socket data is unavailable"},
		{kind: viewMounts, scope: "mounts", empty: "No mounts", failed: "Mount data is unavailable"},
		{kind: viewMemory, scope: "memory", empty: "No memory data", failed: "Memory data is unavailable"},
	} {
		t.Run(test.scope, func(t *testing.T) {
			model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
			model.views = model.views.push(viewFrame{Kind: test.kind, Label: test.scope, NodeID: "worker-id", NodeTarget: "worker-1"})
			request, _ := model.requests.begin(directDiagnosticSlot("worker-id", test.scope), model.generation())
			model = applyAuxDiagnostic(model, test.kind, request.stamp, false)
			assert.Equal(t, directReady, directAuxStatus(model, test.kind))
			assert.Contains(t, model.View().Content, test.empty)

			request, _ = model.requests.begin(directDiagnosticSlot("worker-id", test.scope), model.generation())
			model = applyAuxDiagnostic(model, test.kind, request.stamp, true)
			assert.Equal(t, directFailed, directAuxStatus(model, test.kind))
			assert.Contains(t, model.View().Content, test.failed)
			assert.NotContains(t, model.View().Content, "private transport")
		})
	}
}

func TestDirectAuxDiagnosticFilterQAndPaletteNavigation(t *testing.T) {
	for _, kind := range []viewKind{viewNetstat, viewMounts, viewMemory} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
			model.views = model.views.push(viewFrame{Kind: kind, Label: "diagnostic", NodeID: "worker-id", NodeTarget: "worker-1"})
			model, _ = updateDirect(model, keyPress('/'))
			model, _ = updateDirect(model, keyPress('q'))
			filter, filtering := directAuxFilter(model, kind)
			assert.Equal(t, "q", filter)
			assert.True(t, filtering)
			model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
			model, _ = updateDirect(model, keyPress(':'))
			assert.True(t, model.palette.active)
		})
	}
}

func TestDirectAuxDiagnosticRejectsAtoBtoAResult(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-a", Name: "worker-a"}, {ID: "node-b", Name: "worker-b"}})
	model.views = model.views.push(viewFrame{Kind: viewNetstat, Label: "worker-a > netstat", NodeID: "node-a", NodeTarget: "worker-a"})
	old, _ := model.requests.begin(directDiagnosticSlot("node-a", "sockets"), model.generation())
	model.generationCounter++
	model.views = model.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"}).push(viewFrame{Kind: viewNetstat, Label: "worker-b > netstat", NodeID: "node-b", NodeTarget: "worker-b"})
	_, _ = model.requests.begin(directDiagnosticSlot("node-b", "sockets"), model.generation())
	model.generationCounter++
	model.views = model.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"}).push(viewFrame{Kind: viewNetstat, Label: "worker-a > netstat", NodeID: "node-a", NodeTarget: "worker-a"})
	newer, _ := model.requests.begin(directDiagnosticSlot("node-a", "sockets"), model.generation())
	model, _ = updateDirect(model, directSocketsLoaded{request: old.stamp, nodeID: "node-a", node: "worker-a", sockets: domain.SocketSet{Sockets: []domain.SocketSnapshot{{LocalAddress: "stale-a"}}}})
	assert.NotContains(t, model.View().Content, "stale-a")
	model, _ = updateDirect(model, directSocketsLoaded{request: newer.stamp, nodeID: "node-a", node: "worker-a", sockets: domain.SocketSet{Sockets: []domain.SocketSnapshot{{LocalAddress: "fresh-a"}}}})
	assert.Contains(t, model.View().Content, "fresh-a")
}

func applyAuxDiagnostic(model model, kind viewKind, stamp directRequestStamp, failed bool) model {
	var message tea.Msg
	switch kind {
	case viewNetstat:
		result := directSocketsLoaded{request: stamp, nodeID: "worker-id", node: "worker-1"}
		if failed {
			result.err = errors.New("private transport detail")
		}
		message = result
	case viewMounts:
		result := directMountsLoaded{request: stamp, nodeID: "worker-id", node: "worker-1"}
		if failed {
			result.err = errors.New("private transport detail")
		}
		message = result
	default:
		result := directMemoryLoaded{request: stamp, nodeID: "worker-id", node: "worker-1"}
		if failed {
			result.err = errors.New("private transport detail")
		}
		message = result
	}
	updated, _ := updateDirect(model, message)
	return updated
}

func applyAuxPartial(model model, kind viewKind, stamp directRequestStamp) model {
	var message tea.Msg
	switch kind {
	case viewNetstat:
		message = directSocketsLoaded{request: stamp, nodeID: "worker-id", node: "worker-1", sockets: domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp", LocalAddress: "kept-socket"}}}, err: errors.New("private transport detail")}
	case viewMounts:
		message = directMountsLoaded{request: stamp, nodeID: "worker-id", node: "worker-1", mounts: domain.MountSet{Mounts: []domain.MountSnapshot{{Filesystem: "kept-mount", MountedOn: "/data"}}}, err: errors.New("private transport detail")}
	default:
		message = directMemoryLoaded{request: stamp, nodeID: "worker-id", node: "worker-1", memory: domain.MemorySnapshot{TotalBytes: 1024, UsedBytes: 256}, err: errors.New("private transport detail")}
	}
	updated, _ := updateDirect(model, message)
	return updated
}

func directAuxFilter(model model, kind viewKind) (string, bool) {
	switch kind {
	case viewNetstat:
		return model.netstat.filter, model.netstat.filtering
	case viewMounts:
		return model.mounts.filter, model.mounts.filtering
	default:
		return model.memory.filter, model.memory.filtering
	}
}

func directAuxStatus(model model, kind viewKind) directLoadStatus {
	switch kind {
	case viewNetstat:
		return model.netstat.status
	case viewMounts:
		return model.mounts.status
	default:
		return model.memory.status
	}
}
