package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectEventsFilterSelectionAndPaletteNavigation(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.width, model.height = 120, 30
	events := []domain.EventSnapshot{
		{Node: "node-a", Kind: "Sequence", Message: "boot started", ObservedAt: time.Unix(10, 0)},
		{Node: "node-b", Kind: "ServiceState", Message: "etcd ready", ObservedAt: time.Unix(20, 0)},
	}
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: events}}
	model.events = newEventsModel().normalizeSelection(events, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	assert.NotContains(t, ansi.Strip(model.View().Content), "Follow")

	model, _ = updateDirect(model, keyPress('j'))
	assert.Equal(t, eventKey(events[1]), model.events.selectedKey)
	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune{'e', 't', 'c', 'd'} {
		model, _ = updateDirect(model, keyPress(key))
	}
	assert.Contains(t, model.View().Content, "/etcd")
	assert.Contains(t, model.View().Content, "etcd ready")
	assert.NotContains(t, model.View().Content, "boot started")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, "etcd", model.events.filter)
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune{'m', 'i', 's', 's', 'i', 'n', 'g'} {
		model, _ = updateDirect(model, keyPress(key))
	}
	assert.Contains(t, ansi.Strip(model.View().Content), "/missing")
	assert.Contains(t, ansi.Strip(model.View().Content), "No events")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "/missing")
	model, _ = updateDirect(model, keyPress(':'))
	assert.True(t, model.palette.active)
	model, command := runDirectPalette(t, model, "nodes")
	require.NotNil(t, command)
	assert.Equal(t, viewNodes, model.views.top().Kind)
}

func TestDirectEventsKeepSelectedIdentityAcrossRefreshAndClearWhenMissing(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	first := domain.EventSnapshot{Node: "node-a", Kind: "Sequence", Message: "first", ObservedAt: time.Unix(10, 0)}
	selected := domain.EventSnapshot{Node: "node-b", Kind: "ServiceState", Message: "selected", ObservedAt: time.Unix(20, 0)}
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: []domain.EventSnapshot{first, selected}}}
	model.events = newEventsModel().normalizeSelection(model.cluster.events.Value.Events, 1)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	request, _ := model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: request.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{selected, first}}})
	assert.Equal(t, eventKey(selected), model.events.selectedKey)

	request, _ = model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: request.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{first}}})
	assert.NotEqual(t, eventKey(selected), model.events.selectedKey)
	assert.Equal(t, eventKey(first), model.events.selectedKey)
}

func TestDirectEventsIgnoresStaleRefreshResults(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: []domain.EventSnapshot{{Node: "node-a", Message: "current"}}}}
	oldRequest, _ := model.requests.begin("cluster-events", model.generation())
	newRequest, _ := model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: oldRequest.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{{Node: "node-b", Message: "stale"}}}})
	assert.Equal(t, "current", model.cluster.events.Value.Events[0].Message)
	model, _ = updateDirect(model, directEventsLoaded{request: newRequest.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{{Node: "node-c", Message: "fresh"}}}})
	assert.Equal(t, "fresh", model.cluster.events.Value.Events[0].Message)
}

func TestDirectEventsPartialProblemsSurviveWithoutAggregateError(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	request, _ := model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: request.stamp, events: domain.EventSet{
		Events:   []domain.EventSnapshot{{Node: "healthy", Kind: "Sequence", Message: "still visible"}},
		Problems: []domain.EventProblem{{Node: "failed", Message: "stream unavailable"}},
	}})
	assert.Equal(t, directPartial, model.cluster.events.Status)
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "still visible")
	assert.Contains(t, content, "failed")
	assert.Contains(t, content, "StreamError")
}

func TestDirectEventsRefreshUsesOnlyEventsRequestSlot(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.session = &application.Session{}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	model, command := updateDirect(model, keyPress('r'))
	assert.NotNil(t, command)
	assert.Contains(t, model.requests.slots, "cluster-events")
	assert.NotContains(t, model.requests.slots, "cluster-nodes-services")
}

func TestDirectEventsAliasesRouteFromPalette(t *testing.T) {
	for _, alias := range []string{"events", "ev"} {
		t.Run(alias, func(t *testing.T) {
			model := newDirectModel(context.Background(), nil, LaunchOptions{})
			model.splash = false
			model, _ = runDirectPalette(t, model, alias)
			assert.Equal(t, viewEvents, model.views.top().Kind)
		})
	}
}

func TestDirectEventsContextResetClearsFilterAndSelection(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.events.filter = "old"
	model.events.filtering = true
	model.events.selectedKey = "old identity"
	model.beginSessionOpen("next")
	assert.Empty(t, model.events.filter)
	assert.False(t, model.events.filtering)
	assert.Empty(t, model.events.selectedKey)
}

func runDirectPalette(t *testing.T, model directModel, value string) (directModel, tea.Cmd) {
	t.Helper()
	if !model.palette.active {
		model, _ = updateDirect(model, keyPress(':'))
	}
	for _, key := range []rune(value) {
		model, _ = updateDirect(model, keyPress(key))
	}
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if command != nil {
		model, _ = updateDirect(model, command())
	}
	return model, command
}
