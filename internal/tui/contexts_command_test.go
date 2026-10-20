package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandInputOpensOverContextsAndEscRestoresPicker(t *testing.T) {
	m := readyDirectRootModel(t)
	m.cluster.contexts = []domain.ClusterContext{{Name: "prod"}, {Name: "stage"}}
	m.contexts = newContextsModel(m.cluster.contexts, "prod")
	m.nodes.filtering = true // Underlying view must not block commands in the picker.
	m, _ = updateDirect(m, keyPress(':'))
	require.True(t, m.palette.active)
	assert.True(t, m.contexts.active)
	cursor := m.contexts.control.Cursor()
	for _, key := range "jq" {
		m, _ = updateDirect(m, keyPress(key))
	}
	assert.Equal(t, "jq", m.palette.input.Value())
	assert.Equal(t, cursor, m.contexts.control.Cursor())
	assert.True(t, m.contexts.active, "typing q must not close the picker")
	m, _ = updateDirect(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.False(t, m.palette.active)
	assert.True(t, m.contexts.active)
	assert.Equal(t, cursor, m.contexts.control.Cursor())
}

func TestCommandFromContextsNavigatesToRequestedView(t *testing.T) {
	m := readyDirectRootModel(t)
	m.contexts = newContextsModel([]domain.ClusterContext{{Name: "prod"}}, "prod")
	m, _ = updateDirect(m, keyPress(':'))
	require.True(t, m.palette.active)
	for _, key := range "services" {
		m, _ = updateDirect(m, keyPress(key))
	}
	m, command := updateDirect(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	m, _ = updateDirect(m, command())
	assert.False(t, m.palette.active)
	assert.False(t, m.contexts.active)
	assert.Equal(t, viewServices, m.views.top().Kind)
}
