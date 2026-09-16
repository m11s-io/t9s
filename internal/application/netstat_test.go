package application_test

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenNetstatLoadsTheRequestedNode(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withNetstatReader(model, &testkit.FakeNetstatReader{
		ListFunc: func(_ context.Context, node string) (domain.SocketSet, error) {
			assert.Equal(t, "cp-1", node)
			return domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp", State: "listen"}}}, nil
		},
	})

	model, effect := application.Update(model, application.OpenNetstat{Node: "cp-1"})
	assert.Equal(t, application.Loading, model.Netstat.Status)
	assert.Equal(t, "cp-1", model.Netstat.Node)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.NetstatLoaded)
	require.True(t, ok)
	assert.Len(t, loaded.Sockets.Sockets, 1)
}

func TestRefreshNetstatRefetchesTheRememberedNode(t *testing.T) {
	calls := []string{}
	model := application.Model{Generation: 1}
	model = withNetstatReader(model, &testkit.FakeNetstatReader{
		ListFunc: func(_ context.Context, node string) (domain.SocketSet, error) {
			calls = append(calls, node)
			return domain.SocketSet{}, nil
		},
	})
	var openEffect application.Effect
	model, openEffect = application.Update(model, application.OpenNetstat{Node: "cp-1"})
	require.NotNil(t, openEffect)
	openEffect(t.Context(), application.Dependencies{})

	_, effect := application.Update(model, application.RefreshNetstat{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	require.Len(t, calls, 2, "OpenNetstat fetches once, RefreshNetstat fetches again")
	assert.Equal(t, "cp-1", calls[1])
}

func TestNetstatLoadedIgnoresResultForAnotherNode(t *testing.T) {
	model := application.Model{Generation: 1, Netstat: application.SocketState{Status: application.Loading, Node: "cp-2"}}

	model, _ = application.Update(model, application.NetstatLoaded{Generation: 1, Node: "cp-1", Sockets: domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp"}}}})

	assert.Equal(t, application.Loading, model.Netstat.Status, "a late result for a previously opened node must not overwrite the current node")
	assert.Empty(t, model.Netstat.Value.Sockets)
}

func TestNetstatFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1, Netstat: application.SocketState{Node: "cp-1"}}

	model, effect := application.Update(model, application.NetstatFailed{Generation: 1, Node: "cp-1", Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Netstat.Status)
	assert.Equal(t, "netstat unavailable", model.Netstat.Err)
	assert.Equal(t, "cp-1", model.Netstat.Node, "failure must not lose track of which node was open")
}

func TestSelectContextResetsNetstat(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Netstat: application.SocketState{
		Status: application.Ready,
		Node:   "cp-1",
		Value:  domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp"}}},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.SocketState{}, model.Netstat)
}

// withNetstatReader seeds a Model's unexported netstatReader field via the
// SessionOpened message, since application.Model has no exported setter and
// this test file is package application_test (no access to unexported fields).
func withNetstatReader(model application.Model, reader ports.NetstatReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Netstat: reader})
	return model
}

func TestSessionOpenedWiresDiagnosticReaders(t *testing.T) {
	model := application.Model{Generation: 1}

	model, effect := application.Update(model, application.SessionOpened{
		Generation: 1,
		Nodes:      &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) { return domain.NodeSet{}, nil }},
		Dmesg: &testkit.FakeDmesgReader{OpenFunc: func(context.Context, domain.DmesgRequest) (ports.DmesgStream, error) {
			return &testkit.FakeDmesgStream{}, nil
		}},
		Netstat: &testkit.FakeNetstatReader{ListFunc: func(context.Context, string) (domain.SocketSet, error) {
			return domain.SocketSet{}, nil
		}},
		Mounts: &testkit.FakeMountReader{ListFunc: func(context.Context, string) (domain.MountSet, error) {
			return domain.MountSet{}, nil
		}},
		Memory: &testkit.FakeMemoryReader{ListFunc: func(context.Context, string) (domain.MemorySnapshot, error) {
			return domain.MemorySnapshot{}, nil
		}},
	})
	require.NotNil(t, effect)

	model, effect = application.Update(model, application.OpenNetstat{Node: "cp-1"})
	require.NotNil(t, effect)
	message := effect(t.Context(), application.Dependencies{})
	_, ok := message.(application.NetstatLoaded)
	assert.True(t, ok, "the Netstat reader must be wired from SessionOpened")

	model, effect = application.Update(model, application.OpenMounts{Node: "cp-1"})
	require.NotNil(t, effect)
	message = effect(t.Context(), application.Dependencies{})
	_, ok = message.(application.MountsLoaded)
	assert.True(t, ok, "the Mount reader must be wired from SessionOpened")

	model, effect = application.Update(model, application.OpenMemory{Node: "cp-1"})
	require.NotNil(t, effect)
	message = effect(t.Context(), application.Dependencies{})
	_, ok = message.(application.MemoryLoaded)
	assert.True(t, ok, "the Memory reader must be wired from SessionOpened")

	_, effect = application.Update(model, application.OpenDmesg{Request: domain.DmesgRequest{Node: "cp-1"}})
	require.NotNil(t, effect)
}
