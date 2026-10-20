package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestNodeChangesTrackIdentityAndExpire(t *testing.T) {
	now := time.Now()
	m := newNodesModel()
	nodes := []domain.NodeSnapshot{{ID: "a", Version: "v1"}, {ID: "b", Version: "v1"}}
	m.observeRows(nodes, now)
	assert.Empty(t, m.changedRows)
	m.observeRows([]domain.NodeSnapshot{nodes[1], nodes[0]}, now)
	assert.Empty(t, m.changedRows, "reordering must not flash")
	nodes[0].Version = "v2"
	m.observeRows(nodes, now)
	assert.Contains(t, m.changedRows, "a")
	assert.NotContains(t, m.changedRows, "b")
	m.observeRows(nodes, now.Add(time.Second))
	m.expireRows(now.Add(2 * time.Second))
	assert.Empty(t, m.changedRows, "unchanged data must not prolong the flash")
	nodes[0].Version = "v3"
	m.observeRows(nodes, now.Add(3*time.Second))
	nodes[0].Version = "v4"
	m.observeRows(nodes, now.Add(4*time.Second))
	m.expireRows(now.Add(5 * time.Second))
	assert.Contains(t, m.changedRows, "a", "old expiry must not clear a newer change")
	m.observeRows(nil, now.Add(5*time.Second))
	assert.Empty(t, m.changedRows, "removed rows must be forgotten")
}

func TestNodeChangeHighlightRenderingPreservesSelection(t *testing.T) {
	nodes := []domain.NodeSnapshot{{ID: "a", Name: "worker"}}
	changes := map[string]time.Time{"a": time.Now().Add(time.Second)}
	normal := renderNodeTable(120, nodes, -1, nil)
	changed := renderNodeTable(120, nodes, -1, nil, changes)
	assert.NotEqual(t, normal, changed)
	assert.Contains(t, changed, "38;2;255;165;0")
	assert.Equal(t, renderNodeTable(120, nodes, 0, nil), renderNodeTable(120, nodes, 0, nil, changes))
}

func TestDirectNodeChangeSchedulesExpiryAndRejectsStaleTimer(t *testing.T) {
	m := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "a", Name: "worker", Version: "v1"}})
	m, _ = updateDirect(m, keyPress('j'))
	assert.Empty(t, m.nodes.changedRows)
	request, _ := m.requests.begin("cluster-nodes-services", m.generation())
	var command tea.Cmd
	m, command = updateDirect(m, directNodeServicesLoaded{request: request.stamp, nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "a", Name: "worker", Version: "v2"}}}})
	assert.NotNil(t, command, "data changes must schedule a redraw after expiry")
	assert.Contains(t, m.nodes.changedRows, "a")
	m, _ = updateDirect(m, rowChangesExpired{generation: m.generation() + 1, at: time.Now().Add(time.Hour)})
	assert.Contains(t, m.nodes.changedRows, "a")
	m, _ = updateDirect(m, rowChangesExpired{generation: m.generation(), at: time.Now().Add(time.Hour)})
	assert.Empty(t, m.nodes.changedRows)
}

func TestNodeChangesIgnoreRowsWithoutStableIdentity(t *testing.T) {
	m := newNodesModel()
	nodes := []domain.NodeSnapshot{{Name: "a"}, {Name: "b"}}
	m.observeRows(nodes, time.Now())
	assert.False(t, m.observeRows(nodes, time.Now()))
	assert.Empty(t, m.changedRows)
}

func TestInitialNodeLoadDismissesSplashWithoutHighlightTimer(t *testing.T) {
	m := newModel(context.Background(), nil, LaunchOptions{})
	request, _ := m.requests.begin("cluster-nodes-services", m.generation())
	assert.True(t, m.splash)
	m, command := updateDirect(m, directNodeServicesLoaded{request: request.stamp, nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "a", Name: "worker", Version: "v1"}}}})
	assert.False(t, m.splash, "the first node result must immediately dismiss startup")
	assert.Nil(t, command, "initial load must not wait for or schedule a highlight timer")
	assert.Empty(t, m.nodes.changedRows)
	assert.Contains(t, m.View().Content, "worker")
}
