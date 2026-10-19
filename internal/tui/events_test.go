package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func eventsTestState() directEventState {
	return directEventState{
		Status: directReady,
		Value: domain.EventSet{Events: []domain.EventSnapshot{
			{Node: "node-cp-1", Kind: "Sequence", Message: "boot START", ObservedAt: time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)},
			{Node: "node-worker-1", Kind: "ServiceState", Message: "etcd: RUNNING — member is healthy", ObservedAt: time.Date(2026, time.August, 15, 10, 0, 5, 0, time.UTC)},
		}},
	}
}

func TestEventsRenderSemanticColumns(t *testing.T) {
	rendered := renderEventTable(120, eventsTestState().Value.Events, 0)
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

func TestEventsObservedColumnStaysNearMessage(t *testing.T) {
	header := ansi.Strip(strings.Split(renderEventTable(120, eventsTestState().Value.Events, 0), "\n")[0])
	message := strings.Index(header, "MESSAGE")
	observed := strings.Index(header, "OBSERVED")

	assert.GreaterOrEqual(t, message, 0)
	assert.Less(t, observed, 90, "the empty part of MESSAGE should not push OBSERVED to the far edge")
	assert.Less(t, observed-message, 45)
}

func TestEventsRenderEmptyState(t *testing.T) {
	model := newEventsModel()
	rendered := model.viewSized(contentSize{Width: 80, Height: 10}, directEventState{Status: directReady})
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
