package tui

import (
	"context"
	"errors"
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

func TestDirectEventsFilterSelectionAndPaletteNavigation(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.width, model.height = 120, 30
	events := []domain.EventSnapshot{
		{Node: "node-a", Kind: "Sequence", Message: "boot started", ObservedAt: time.Unix(10, 0)},
		{Node: "node-b", Kind: "ServiceState", Message: "etcd ready", ObservedAt: time.Unix(20, 0)},
	}
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: events}}
	model.events = newEventsModel().normalizeSelection(events, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	assert.Contains(t, ansi.Strip(model.View().Content), "Follow")

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

func TestDirectEventsFollowToggleIsIsolatedFromFilterInput(t *testing.T) {
	stream := &directEventStreamFake{started: make(chan struct{}, 1)}
	session, _ := directStreamSession(t, &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }}, nil)
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.session, model.sessionCtx = session, context.Background()
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})

	model, command := updateDirect(model, keyPress('s'))
	require.NotNil(t, command, "s starts FOLLOW")
	model, _ = updateDirect(model, command())
	require.True(t, model.eventFollow.following)
	assert.Contains(t, ansi.Strip(model.View().Content), "EVENTS FOLLOW")

	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress('s'))
	assert.Equal(t, "s", model.events.filter)
	assert.True(t, model.eventFollow.following, "s while filtering is query input and leaves follow active")
	assert.Contains(t, ansi.Strip(model.View().Content), "EVENTS FOLLOW")

	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = updateDirect(model, keyPress('s'))
	assert.False(t, model.eventFollow.following)
	assert.Contains(t, ansi.Strip(model.View().Content), "EVENTS PAUSED")
	assert.Equal(t, 1, stream.closed)
}

func TestDirectEventsKeepSelectedIdentityAcrossRefreshAndClearWhenMissing(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	first := domain.EventSnapshot{Node: "node-a", Kind: "Sequence", Message: "first", ObservedAt: time.Unix(10, 0)}
	selected := domain.EventSnapshot{Node: "node-b", Kind: "ServiceState", Message: "selected", ObservedAt: time.Unix(20, 0)}
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: []domain.EventSnapshot{first, selected}}}
	model.events = newEventsModel().normalizeSelection(model.cluster.events.Value.Events, 1)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	request, _ := model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: request.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{selected, first}}})
	assert.Equal(t, eventKey(selected), model.events.selectedKey)

	refreshedSelected := selected
	refreshedSelected.ObservedAt = time.Unix(30, 0)
	refreshedFirst := first
	refreshedFirst.ObservedAt = time.Unix(40, 0)
	request, _ = model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: request.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{refreshedFirst, refreshedSelected}}})
	assert.Equal(t, eventKey(refreshedSelected), model.events.selectedKey)

	request, _ = model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: request.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{refreshedFirst}}})
	assert.NotEqual(t, eventKey(refreshedSelected), model.events.selectedKey)
	assert.Equal(t, eventKey(refreshedFirst), model.events.selectedKey)
}

func TestEventKeysIgnoreObservationTimeAndDisambiguateDuplicatesByOrder(t *testing.T) {
	first := domain.EventSnapshot{Node: "node-a", Kind: "Sequence", Message: "same", ObservedAt: time.Unix(10, 0)}
	second := first
	second.ObservedAt = time.Unix(20, 0)
	keys := eventKeys([]domain.EventSnapshot{first, second})
	assert.Len(t, keys, 2)
	assert.NotEqual(t, keys[0], keys[1])

	first.ObservedAt = time.Unix(30, 0)
	second.ObservedAt = time.Unix(40, 0)
	refreshedKeys := eventKeys([]domain.EventSnapshot{first, second})
	assert.Equal(t, keys, refreshedKeys)
}

func TestDirectEventsIgnoresStaleRefreshResults(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: []domain.EventSnapshot{{Node: "node-a", Message: "current"}}}}
	oldRequest, _ := model.requests.begin("cluster-events", model.generation())
	newRequest, _ := model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: oldRequest.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{{Node: "node-b", Message: "stale"}}}})
	assert.Equal(t, "current", model.cluster.events.Value.Events[0].Message)
	model, _ = updateDirect(model, directEventsLoaded{request: newRequest.stamp, events: domain.EventSet{Events: []domain.EventSnapshot{{Node: "node-c", Message: "fresh"}}}})
	assert.Equal(t, "fresh", model.cluster.events.Value.Events[0].Message)
}

func TestDirectEventsPartialProblemsSurviveWithoutAggregateError(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
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

func TestDirectEventsPartialWarningStaysVisibleWhenFilterHidesProblems(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.cluster.nodes = directNodeState{Status: directReady, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "healthy", Name: "healthy", Health: domain.HealthHealthy}}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	request, _ := model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{request: request.stamp, events: domain.EventSet{
		Events:   []domain.EventSnapshot{{Node: "healthy", Kind: "Sequence", Message: "healthy event"}},
		Problems: []domain.EventProblem{{Node: "failed", Message: "stream unavailable"}},
	}})
	rows := eventRows(model.cluster.events.Value)
	model.events = model.events.update(keyPress('/'), rows)
	for _, key := range []rune{'h', 'e', 'a', 'l', 't', 'h', 'y'} {
		model.events = model.events.update(keyPress(key), rows)
	}
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "Health:    Healthy")
	assert.Contains(t, content, "EVENTS PARTIAL")
	assert.Contains(t, content, "healthy event")
	assert.NotContains(t, content, "StreamError")
	model.events = model.events.update(tea.KeyPressMsg{Code: tea.KeyEnter}, rows)
	content = ansi.Strip(model.View().Content)
	assert.Contains(t, content, "EVENTS PARTIAL")
	assert.Contains(t, content, "/healthy")
}

func TestDirectEventsAggregateErrorKeepsUsefulRowsVisible(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	request, _ := model.requests.begin("cluster-events", model.generation())
	model, _ = updateDirect(model, directEventsLoaded{
		request: request.stamp,
		events:  domain.EventSet{Events: []domain.EventSnapshot{{Node: "node-a", Kind: "Sequence", Message: "useful event"}}},
		err:     errors.New("partial read"),
	})
	assert.Equal(t, directPartial, model.cluster.events.Status)
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "EVENTS PARTIAL")
	assert.Contains(t, content, "useful event")
}

func TestDirectEventsRefreshUsesOnlyEventsRequestSlot(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
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
			model := newModel(context.Background(), nil, LaunchOptions{})
			model.splash = false
			model, _ = runDirectPalette(t, model, alias)
			assert.Equal(t, viewEvents, model.views.top().Kind)
		})
	}
}

func TestDirectEventsContextResetClearsFilterAndSelection(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.events.filter = "old"
	model.events.filtering = true
	model.events.selectedKey = "old identity"
	model.beginSessionOpen("next")
	assert.Empty(t, model.events.filter)
	assert.False(t, model.events.filtering)
	assert.Empty(t, model.events.selectedKey)
}

func runDirectPalette(t *testing.T, model model, value string) (model, tea.Cmd) {
	t.Helper()
	model.splash = false
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
