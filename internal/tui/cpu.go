package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type cpuModel struct {
	state     application.CPUState
	filter    string
	filtering bool
	offset    int
}

func newCPUModel(state application.CPUState) cpuModel           { return cpuModel{state: state} }
func (m cpuModel) setState(state application.CPUState) cpuModel { m.state = state; return m }
func (m cpuModel) update(message tea.KeyPressMsg) cpuModel {
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
func (m cpuModel) rows() []string {
	if m.state.Status == application.Loading || m.state.Status == application.Idle {
		return []string{"Sampling CPU… (two samples, one second apart)"}
	}
	if m.state.Status == application.Failed {
		return []string{fallback(m.state.Err)}
	}
	value := m.state.Value
	interval := "unknown"
	if value.Interval > 0 {
		interval = value.Interval.String()
	}
	sampledAt := "unknown"
	if !value.SampledAt.IsZero() {
		sampledAt = value.SampledAt.UTC().Format(time.RFC3339Nano)
	}
	running, blocked := "unknown", "unknown"
	if value.ProcessCountsKnown {
		running = fmt.Sprint(value.RunningProcesses)
		blocked = fmt.Sprint(value.BlockedProcesses)
	}
	rows := []string{"CPU UTILIZATION (sampled; r takes a new sample)", "Sample time UTC: " + sampledAt, "Sample interval: " + interval, "Running processes: " + running, "Blocked processes: " + blocked, cpuUsageRow("All CPUs", value.Aggregate)}
	if len(value.Cores) == 0 {
		rows = append(rows, "Per-core utilization: unknown")
	}
	for _, core := range value.Cores {
		rows = append(rows, cpuUsageRow(fmt.Sprintf("CPU%d", core.ID), core.CPUUtilization))
	}
	if m.state.Err != "" {
		rows = append(rows, "Problem: "+m.state.Err)
	}
	for _, problem := range value.Problems {
		rows = append(rows, "Problem: "+problem)
	}
	return rows
}
func cpuUsageRow(label string, value domain.CPUUtilization) string {
	if !value.Known {
		return label + ": busy unknown | I/O wait unknown"
	}
	return fmt.Sprintf("%s: %.1f%% busy | %.1f%% I/O wait", label, value.BusyPercent, value.IOWaitPercent)
}
func (m cpuModel) visibleRows() []string {
	rows := m.rows()
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return rows
	}
	filtered := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row), query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}
func (m cpuModel) clampOffset(height int) cpuModel {
	m.offset = min(max(0, m.offset), max(0, len(m.visibleRows())-max(1, height)))
	return m
}
func (m cpuModel) viewSized(size contentSize) string {
	rows := m.visibleRows()
	if len(rows) == 0 {
		return "No matching CPU data"
	}
	m = m.clampOffset(size.Height)
	end := min(len(rows), m.offset+max(0, size.Height))
	return strings.Join(rows[m.offset:end], "\n")
}
