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

func TestOpenMemoryLoadsTheRequestedNode(t *testing.T) {
	model := application.Model{Generation: 1}
	model = withMemoryReader(model, &testkit.FakeMemoryReader{
		ListFunc: func(_ context.Context, node string) (domain.MemorySnapshot, error) {
			assert.Equal(t, "cp-1", node)
			return domain.MemorySnapshot{TotalBytes: 1000, AvailableBytes: 250, UsedBytes: 750}, nil
		},
	})

	model, effect := application.Update(model, application.OpenMemory{Node: "cp-1"})
	assert.Equal(t, application.Loading, model.Memory.Status)
	assert.Equal(t, "cp-1", model.Memory.Node)
	require.NotNil(t, effect)

	message := effect(t.Context(), application.Dependencies{})
	loaded, ok := message.(application.MemoryLoaded)
	require.True(t, ok)
	assert.Equal(t, "cp-1", loaded.Node)
	assert.Equal(t, uint64(750), loaded.Memory.UsedBytes)
}

func TestRefreshMemoryRefetchesTheRememberedNode(t *testing.T) {
	calls := []string{}
	model := application.Model{Generation: 1}
	model = withMemoryReader(model, &testkit.FakeMemoryReader{
		ListFunc: func(_ context.Context, node string) (domain.MemorySnapshot, error) {
			calls = append(calls, node)
			return domain.MemorySnapshot{}, nil
		},
	})
	var openEffect application.Effect
	model, openEffect = application.Update(model, application.OpenMemory{Node: "cp-1"})
	require.NotNil(t, openEffect)
	openEffect(t.Context(), application.Dependencies{})

	_, effect := application.Update(model, application.RefreshMemory{})
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	require.Len(t, calls, 2, "OpenMemory fetches once, RefreshMemory fetches again")
	assert.Equal(t, "cp-1", calls[1])
}

func TestMemoryLoadedIgnoresResultForAnotherNode(t *testing.T) {
	model := application.Model{Generation: 1, Memory: application.MemoryState{Status: application.Loading, Node: "cp-2"}}

	model, _ = application.Update(model, application.MemoryLoaded{Generation: 1, Node: "cp-1", Memory: domain.MemorySnapshot{TotalBytes: 1000}})

	assert.Equal(t, application.Loading, model.Memory.Status, "a late result for a previously opened node must not overwrite the current node")
	assert.Equal(t, uint64(0), model.Memory.Value.TotalBytes)
}

func TestMemoryFailedSetsFailedStatus(t *testing.T) {
	model := application.Model{Generation: 1, Memory: application.MemoryState{Node: "cp-1"}}

	model, effect := application.Update(model, application.MemoryFailed{Generation: 1, Node: "cp-1", Err: assert.AnError})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.Memory.Status)
	assert.Equal(t, "memory unavailable", model.Memory.Err)
	assert.Equal(t, "cp-1", model.Memory.Node, "failure must not lose track of which node was open")
}

func TestMemoryFailedIgnoresResultForAnotherNode(t *testing.T) {
	model := application.Model{Generation: 1, Memory: application.MemoryState{Status: application.Loading, Node: "cp-2"}}

	model, _ = application.Update(model, application.MemoryFailed{Generation: 1, Node: "cp-1", Err: assert.AnError})

	assert.Equal(t, application.Loading, model.Memory.Status, "a stale failure for another node must not fail the current view")
}

func TestSelectContextResetsMemory(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Memory: application.MemoryState{
		Status: application.Ready,
		Node:   "cp-1",
		Value:  domain.MemorySnapshot{TotalBytes: 1000},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.MemoryState{}, model.Memory)
}

func withMemoryReader(model application.Model, reader ports.MemoryReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Memory: reader})
	return model
}
