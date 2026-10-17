package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type timeModel struct {
	state     application.TimeState
	filter    string
	filtering bool
	offset    int
}

func newTimeModel(state application.TimeState) timeModel           { return timeModel{state: state} }
func (m timeModel) setState(state application.TimeState) timeModel { m.state = state; return m }
func (m timeModel) update(message tea.KeyPressMsg) timeModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
		case "enter":
			m.filtering = false
		case "backspace":
			m.filter = trimLastRune(m.filter)
		default:
			m.filter += printableText(message.Text)
		}
		m.offset = 0
		return m
	}
	switch key {
	case "/":
		m.filtering = true
		m.offset = 0
	case "up", "k":
		m.offset = max(0, m.offset-1)
	case "down", "j":
		m.offset++
	case "g":
		m.offset = 0
	case "G":
		m.offset = len(m.rows())
	}
	return m
}
func (m timeModel) rows() []string {
	rows := []string{"TIME STATUS"}
	switch m.state.Status {
	case application.Loading, application.Idle:
		rows = append(rows, "Loading time status…")
	case application.Failed:
		rows = append(rows, m.state.Err)
	default:
		synced := "unknown"
		if m.state.Value.SyncKnown {
			synced = "no"
			if m.state.Value.Synced {
				synced = "yes"
			}
			if m.state.Value.SyncDisabled {
				synced += " (synchronization disabled)"
			}
		}
		rows = append(rows, "Synchronized: "+synced)
		rows = append(rows, timeSampleRows(m.state.Value.TimeSample)...)
	}
	rows = append(rows, "", "SERVER CHECK (explicit)")
	switch m.state.CheckStatus {
	case application.Loading:
		rows = append(rows, "Checking "+m.state.Check.Server+"…")
	case application.Failed:
		rows = append(rows, "Server: "+m.state.Check.Server, m.state.CheckErr)
	case application.Ready, application.Partial:
		rows = append(rows, timeSampleRows(m.state.Check.TimeSample)...)
	default:
		rows = append(rows, "Press c to check a server")
	}
	return rows
}
func timeSampleRows(sample domain.TimeSample) []string {
	difference := "unknown"
	if sample.DifferenceValid {
		difference = sample.Difference.String()
		if sample.Difference >= 0 {
			difference = "+" + difference
		}
	}
	server := sample.Server
	if server == "" {
		server = "unknown"
	}
	rows := []string{"Server: " + server, "Node UTC: " + formatTimeSample(sample.LocalTime, sample.LocalTimeValid), "Server UTC: " + formatTimeSample(sample.RemoteTime, sample.RemoteTimeValid), "sampled difference (server - node): " + difference}
	for _, problem := range sample.Problems {
		rows = append(rows, "Problem: "+problem)
	}
	return rows
}
func formatTimeSample(value time.Time, valid bool) string {
	if !valid {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339Nano)
}
func (m timeModel) visibleRows() []string {
	rows := m.rows()
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query != "" {
		filtered := []string{}
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row), query) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	return rows
}

func (m timeModel) clampOffset(height int) timeModel {
	m.offset = min(max(0, m.offset), max(0, len(m.visibleRows())-max(1, height)))
	return m
}

func (m timeModel) viewSized(size contentSize) string {
	rows := m.visibleRows()
	if len(rows) == 0 {
		return "No matching time data"
	}
	m = m.clampOffset(size.Height)
	end := min(len(rows), m.offset+max(0, size.Height))
	return strings.Join(rows[m.offset:end], "\n")
}
