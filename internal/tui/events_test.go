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
	model := newEventsModel(application.EventState{Status: application.Ready})
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No events", rendered)
}

func TestEventsFilterMatchesNodeKindAndMessage(t *testing.T) {
	events := newEventsModel(eventsTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('e'), keyPress('t'), keyPress('c'), keyPress('d'),
		{Code: tea.KeyEnter},
	} {
		events = events.update(message)
	}

	rendered := events.view(120)

	assert.Contains(t, rendered, "etcd")
	assert.NotContains(t, rendered, "boot START")
}
