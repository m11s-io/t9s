package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
)

const rowChangeDuration = 2 * time.Second

type rowChangesExpired struct {
	generation uint64
	at         time.Time
}

// Update observes the final snapshot so polling, enrichment and streamed health
// updates all use the same comparison, after stale results have been rejected.
func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if expiry, ok := message.(rowChangesExpired); ok {
		if expiry.generation == m.generation() {
			m.nodes.expireRows(expiry.at)
			for _, tracker := range m.tableTrackers() {
				for id, deadline := range tracker.deadlines {
					if !expiry.at.Before(deadline) {
						delete(tracker.deadlines, id)
					}
				}
			}
		}
		return m, nil
	}
	previousGeneration := m.generation()
	m.palette.resize(m.width)
	next, command := m.update(message)
	updated := next.(model)
	updated.palette.resize(updated.width)
	if updated.palette.active != m.palette.active {
		updated.syncDirectStreamViewports()
		updated.resizeDirectResourceDetail()
		updated.time = updated.time.clampOffset(updated.timeViewportHeight())
	}
	if updated.generation() != previousGeneration {
		updated.nodes.rowValues = nil
		updated.nodes.changedRows = nil
		for _, tracker := range updated.tableTrackers() {
			*tracker = tableChanges{}
		}
	}

	now := time.Now()
	changed := updated.observeTables(now)
	if tableReady(updated.cluster.nodes.Status) {
		changed = updated.nodes.observeRows(updated.cluster.nodes.Value.Nodes, now) || changed
	}
	if !changed {
		return updated, command
	}
	generation := updated.generation()
	expiry := tea.Tick(rowChangeDuration, func(at time.Time) tea.Msg {
		return rowChangesExpired{generation: generation, at: at}
	})
	return updated, tea.Batch(command, expiry)
}

// Compare full displayed values, before truncation; observation timestamps and
// other invisible metadata should never cause a row to flash.
func (m *nodesModel) observeRows(nodes []domain.NodeSnapshot, now time.Time) bool {
	values := make(map[string][]string, len(nodes))
	changed := false
	if m.changedRows == nil {
		m.changedRows = make(map[string]time.Time)
	}
	for _, node := range nodes {
		if node.ID == "" {
			continue
		}
		row := nodeRowValues(node)
		values[node.ID] = row
		if m.rowValues != nil && !slices.Equal(m.rowValues[node.ID], row) {
			m.changedRows[node.ID] = now.Add(rowChangeDuration)
			changed = true
		}
	}
	for id := range m.changedRows {
		if _, exists := values[id]; !exists {
			delete(m.changedRows, id)
		}
	}
	m.rowValues = values
	return changed
}

func (m *nodesModel) expireRows(now time.Time) {
	for id, deadline := range m.changedRows {
		if !now.Before(deadline) {
			delete(m.changedRows, id)
		}
	}
}
