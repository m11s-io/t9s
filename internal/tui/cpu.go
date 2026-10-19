package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
)

type cpuModel struct {
	status    directLoadStatus
	node      string
	value     domain.CPUSnapshot
	err       string
	filter    string
	filtering bool
	offset    int
}

func newDirectCPUModel() cpuModel { return cpuModel{status: directLoading} }
func (m cpuModel) setResult(node string, value domain.CPUSnapshot, err error) cpuModel {
	m.node, m.value, m.err = node, value, ""
	switch {
	case err != nil && errors.Is(err, context.Canceled):
		m.status, m.err = directFailed, "CPU sample canceled"
		if value.ProcessCountsKnown || len(value.Cores) > 0 || value.Aggregate.Known {
			m.status = directPartial
		}
	case err != nil:
		m.status, m.err = directFailed, "CPU sample unavailable"
		if value.ProcessCountsKnown || len(value.Cores) > 0 || value.Aggregate.Known {
			m.status = directPartial
		}
	case len(value.Problems) > 0 || !value.Aggregate.Known:
		m.status, m.err = directPartial, "CPU data is incomplete"
	default:
		m.status = directReady
	}
	return m
}
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
	if m.status == directLoading || m.status == directLoadStatus("") {
		return []string{"Sampling CPU… (two samples, one second apart)"}
	}
	if m.status == directFailed {
		return []string{fallback(m.err)}
	}
	value := m.value
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
	if m.err != "" {
		rows = append(rows, "Problem: "+m.err)
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

func (m cpuModel) filteringActive() bool { return m.filtering || m.filter != "" }
