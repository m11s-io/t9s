package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestSocketChangesHighlightStateAndNewSocketsAfterBaseline(t *testing.T) {
	m := newModel(context.Background(), nil, LaunchOptions{})
	m.netstat = m.netstat.setResult("worker", domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp", LocalAddress: "a:1", RemoteAddress: "b:2", State: "SYN_SENT"}}}, nil)
	m, _ = updateDirect(m, keyPress('j'))
	assert.Empty(t, m.netstat.changes.deadlines)
	m.netstat = m.netstat.setResult("worker", domain.SocketSet{Sockets: []domain.SocketSnapshot{{Protocol: "tcp", LocalAddress: "a:1", RemoteAddress: "b:2", State: "ESTABLISHED"}, {Protocol: "tcp", LocalAddress: "c:3", State: "LISTEN"}}}, nil)
	m, _ = updateDirect(m, keyPress('j'))
	assert.Len(t, m.netstat.changes.deadlines, 2)
	content := m.netstat.viewSized(contentSize{Width: 120, Height: 10})
	assert.Contains(t, content, "38;2;255;165;0")
	m, _ = updateDirect(m, rowChangesExpired{generation: m.generation(), at: time.Now().Add(time.Hour)})
	assert.Empty(t, m.netstat.changes.deadlines)
}

func TestAutomaticRefreshIncludesSocketsAndPausesForFilter(t *testing.T) {
	m := newModel(context.Background(), nil, LaunchOptions{})
	m.session = &application.Session{}
	m.sessionCtx = context.Background()
	m.views = m.views.push(viewFrame{Kind: viewNetstat, NodeID: "worker-id", NodeTarget: "worker"})
	m, _ = updateDirect(m, nodeAutoRefreshTickMsg{})
	assert.Contains(t, m.requests.slots, "node:worker-id:sockets")
	m.requests.cancelAll()
	m.netstat.filtering = true
	m, _ = updateDirect(m, nodeAutoRefreshTickMsg{})
	assert.Empty(t, m.requests.slots)
}

func TestAutomaticRefreshIncludesClusterSummaryAndNonFollowingEvents(t *testing.T) {
	for _, kind := range []viewKind{viewOverview, viewProblems, viewEvents} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			m := newModel(context.Background(), nil, LaunchOptions{})
			m.session = &application.Session{}
			m.sessionCtx = context.Background()
			m.views = m.views.replaceRoot(viewFrame{Kind: kind})
			m, _ = updateDirect(m, nodeAutoRefreshTickMsg{})
			if kind == viewEvents {
				assert.Contains(t, m.requests.slots, "cluster-events")
			} else {
				assert.Contains(t, m.requests.slots, "cluster-nodes-services")
				assert.Contains(t, m.requests.slots, "cluster-etcd")
			}
			m.requests.cancelAll()
		})
	}
}

func TestRepeatedEventsFlashOnlyTheNewOccurrence(t *testing.T) {
	m := newModel(context.Background(), nil, LaunchOptions{})
	event := domain.EventSnapshot{Node: "worker", Kind: "Sequence", Message: "ready"}
	m.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: []domain.EventSnapshot{event}}}
	m, _ = updateDirect(m, keyPress('j'))
	m.cluster.events.Value.Events = append(m.cluster.events.Value.Events, event)
	m, _ = updateDirect(m, keyPress('j'))
	assert.Len(t, m.events.changes.deadlines, 1)
	assert.Contains(t, m.events.changes.deadlines, "worker\x00Sequence\x00ready\x001")
	rows := m.events.visibleEvents(eventRows(m.cluster.events.Value))
	rendered := renderEventRows(120, rows[1:], rows, -1, m.events.changes)
	assert.Contains(t, rendered, "38;2;255;165;0")
}

func TestAutomaticRefreshDoesNotSupersedePendingRead(t *testing.T) {
	for _, kind := range []viewKind{viewNodes, viewCPU, viewNetstat, viewResourceInstances} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			m := newModel(context.Background(), nil, LaunchOptions{})
			m.session = &application.Session{}
			m.sessionCtx = context.Background()
			m.views = m.views.replaceRoot(viewFrame{Kind: kind, NodeID: "worker", NodeTarget: "worker", ResourceKind: "test"})
			m, _ = updateDirect(m, nodeAutoRefreshTickMsg{})
			first := m.requests.serial
			m, _ = updateDirect(m, nodeAutoRefreshTickMsg{})
			assert.Equal(t, first, m.requests.serial, "automatic ticks must let pending reads finish")
			m.requests.cancelAll()
		})
	}
}
