package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func eventsTestState() application.EventState {
	return application.EventState{
		Status: application.Ready,
		Value: domain.EventSet{Events: []domain.EventSnapshot{
			{Node: "node-cp-1", Kind: "Sequence", Message: "boot START", ObservedAt: time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)},
			{Node: "node-worker-1", Kind: "ServiceState", Message: "etcd: RUNNING — member is healthy", ObservedAt: time.Date(2026, time.August, 15, 10, 0, 5, 0, time.UTC)},
		}},
	}
}

func TestEventsRenderSemanticColumns(t *testing.T) {
	rendered := renderEvents(120, eventsTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"NODE", "KIND", "MESSAGE", "OBSERVED"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "node-cp-1")
	assert.Contains(t, ansi.Strip(lines[1]), "boot START")
	assert.Contains(t, ansi.Strip(lines[1]), "2026-08-15T10:00:00Z")
}

func TestEventsRenderEmptyState(t *testing.T) {
	model := newEventsModel()
	rendered := model.legacyViewSized(contentSize{Width: 80, Height: 10}, application.EventState{Status: application.Ready})
	assert.Equal(t, "No events", rendered)
}

func TestEventsFilterMatchesNodeKindAndMessage(t *testing.T) {
	state := eventsTestState()
	rows := eventRows(state.Value)
	events := newEventsModel().normalizeSelection(rows, 0)
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('e'), keyPress('t'), keyPress('c'), keyPress('d'),
		{Code: tea.KeyEnter},
	} {
		events = events.update(message, rows)
	}

	rendered := events.view(120, rows)

	assert.Contains(t, rendered, "etcd")
	assert.NotContains(t, rendered, "boot START")
}

func TestEventsFollowKeyAndFilterIsolation(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Generation: 1, Events: eventsTestState()}, application.NewRunner(application.Dependencies{}))
	root.splash = false
	root.views = root.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	root, _ = updateRoot(root, keyPress('s'))
	assert.Contains(t, root.View().Content, "FOLLOW", "s should start read-only event following")
	root, _ = updateRoot(root, keyPress('/'))
	root, _ = updateRoot(root, keyPress('s'))
	assert.Equal(t, "s", root.events.filter)
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})
	root, _ = updateRoot(root, keyPress('s'))
	assert.Contains(t, root.View().Content, "PAUSED")
}

func TestEventsFollowStopsOnPaletteNavigation(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Generation: 1, Events: eventsTestState()}, application.NewRunner(application.Dependencies{}))
	root.splash = false
	root.views = root.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	root, _ = updateRoot(root, keyPress('s'))
	root = enterCommand(t, root, "nodes")
	assert.False(t, root.application.Events.Following)
	root = enterCommand(t, root, "events")
	assert.False(t, root.application.Events.Following, "returning to events must not restart a stream")
}

func TestEventsFollowFailuresAreFilterableRows(t *testing.T) {
	state := eventsTestState()
	state.FollowAttempted = true
	state.Following = true
	state.Value.Problems = []domain.EventProblem{{Node: "failed-node", Message: "event stream unavailable"}}
	rows := eventRows(state.Value)
	events := newEventsModel().startFilter("failed-node", rows)
	rendered := events.legacyViewSized(contentSize{Width: 100, Height: 10}, state)
	assert.Contains(t, rendered, "failed-node")
	assert.Contains(t, rendered, "StreamError")
	assert.NotContains(t, rendered, "boot START")
}

func TestEventsFollowKeepsNewestRowsVisible(t *testing.T) {
	state := eventsTestState()
	rows := eventRows(state.Value)
	events := newEventsModel().normalizeSelection(rows, 0)
	state.Following = true
	state.FollowAttempted = true
	for range 20 {
		state.Value.Events = append(state.Value.Events, domain.EventSnapshot{Node: "cp-1", Message: "middle"})
	}
	state.Value.Events = append(state.Value.Events, domain.EventSnapshot{Node: "cp-1", Message: "newest"})
	events = events.syncLegacySelection(state)
	rendered := events.legacyViewSized(contentSize{Width: 100, Height: 5}, state)
	assert.Contains(t, rendered, "newest")
	assert.NotContains(t, rendered, "boot START")
}

func TestEventsFollowFailuresDoNotHideHealthyTraffic(t *testing.T) {
	state := eventsTestState()
	state.Following = true
	state.FollowAttempted = true
	state.Value.Events = append(state.Value.Events, domain.EventSnapshot{Node: "healthy", Message: "latest event"})
	for range 10 {
		state.Value.Problems = append(state.Value.Problems, domain.EventProblem{Node: "failed", Message: "unavailable"})
	}
	events := newEventsModel().syncLegacySelection(state)
	rendered := events.legacyViewSized(contentSize{Width: 100, Height: 5}, state)
	assert.Contains(t, rendered, "latest event")
	events = events.startFilter("StreamError", eventRows(state.Value))
	assert.Contains(t, events.legacyViewSized(contentSize{Width: 100, Height: 5}, state), "failed")
}
