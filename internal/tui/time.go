package tui

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type timeModel struct {
	status      directLoadStatus
	node        string
	value       domain.TimeSnapshot
	err         string
	checkStatus directLoadStatus
	check       domain.TimeCheckResult
	checkErr    string
	filter      string
	filtering   bool
	offset      int
}

func newDirectTimeModel() timeModel { return timeModel{status: directLoading} }
func (m timeModel) setReadResult(node string, value domain.TimeSnapshot, err error) timeModel {
	m.node, m.value, m.err = node, value, ""
	switch {
	case err != nil:
		m.status, m.err = directFailed, "Time status unavailable"
		if timeSnapshotHasValues(value) {
			m.status, m.err = directPartial, "Some time status is unavailable"
		}
	case len(value.Problems) > 0:
		m.status, m.err = directPartial, "Some time status is unavailable"
	default:
		m.status = directReady
	}
	return m
}
func (m timeModel) setCheckResult(node, server string, value domain.TimeCheckResult, err error) timeModel {
	m.check = value
	m.check.Server = server
	m.checkErr = ""
	switch {
	case err != nil && errors.Is(err, ports.ErrTimePermissionDenied):
		m.checkStatus, m.checkErr = directFailed, "time check permission denied"
	case err != nil:
		m.checkStatus, m.checkErr = directFailed, "time check unavailable"
		if timeSampleHasValues(value.TimeSample) {
			m.checkStatus, m.checkErr = directPartial, "Some time check data is unavailable"
		}
	case len(value.Problems) > 0:
		m.checkStatus, m.checkErr = directPartial, "Some time check data is unavailable"
	default:
		m.checkStatus = directReady
	}
	return m
}
func timeSnapshotHasValues(value domain.TimeSnapshot) bool {
	return value.SyncKnown || timeSampleHasValues(value.TimeSample) || len(value.Problems) > 0
}
func timeSampleHasValues(value domain.TimeSample) bool {
	return value.Server != "" || value.LocalTimeValid || value.RemoteTimeValid || value.DifferenceValid || len(value.Problems) > 0
}
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
	switch m.status {
	case directLoading, directLoadStatus(""):
		rows = append(rows, "Loading time status…")
	case directFailed:
		rows = append(rows, m.err)
	default:
		synced := "unknown"
		if m.value.SyncKnown {
			synced = "no"
			if m.value.Synced {
				synced = "yes"
			}
			if m.value.SyncDisabled {
				synced += " (synchronization disabled)"
			}
		}
		rows = append(rows, "Synchronized: "+synced)
		rows = append(rows, timeSampleRows(m.value.TimeSample)...)
		if m.err != "" {
			rows = append(rows, "Problem: "+m.err)
		}
	}
	rows = append(rows, "", "SERVER CHECK (explicit)")
	switch m.checkStatus {
	case directLoading:
		rows = append(rows, "Checking "+m.check.Server+"…")
	case directFailed:
		rows = append(rows, "Server: "+m.check.Server, m.checkErr)
	case directReady, directPartial:
		rows = append(rows, timeSampleRows(m.check.TimeSample)...)
		if m.checkErr != "" {
			rows = append(rows, "Problem: "+m.checkErr)
		}
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

func (m timeModel) filteringActive() bool { return m.filtering || m.filter != "" }
