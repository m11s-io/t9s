package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeProgressShowsRowsAndKeepsRequestAliveUntilCompletion(t *testing.T) {
	m := newModel(context.Background(), nil, LaunchOptions{})
	request, _ := m.requests.begin("cluster-nodes-services", m.generation())
	updates := make(chan tea.Msg, 1)
	m, command := updateDirect(m, directNodeProgress{request: request.stamp, ctx: request.ctx, updates: updates, nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "a", Name: "worker", Stage: "Loading"}}}})
	assert.False(t, m.splash)
	assert.Contains(t, m.View().Content, "worker")
	assert.True(t, m.requests.current(request.stamp))
	require.NotNil(t, command)
	updates <- directNodeServicesLoaded{request: request.stamp, nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "a", Name: "worker", Stage: "running"}}}}
	m, _ = updateDirect(m, command())
	assert.Equal(t, "running", m.cluster.nodes.Value.Nodes[0].Stage)
	assert.False(t, m.requests.current(request.stamp))
}

func TestNodeProgressRejectsSupersededRequest(t *testing.T) {
	m := newModel(context.Background(), nil, LaunchOptions{})
	old, _ := m.requests.begin("cluster-nodes-services", m.generation())
	m.requests.begin("cluster-nodes-services", m.generation())
	m, command := updateDirect(m, directNodeProgress{request: old.stamp, nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "old", Name: "old"}}}})
	assert.True(t, m.splash)
	assert.Empty(t, m.cluster.nodes.Value.Nodes)
	assert.Nil(t, command)
}
