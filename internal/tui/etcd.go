package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/table"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type etcdModel struct {
	state     application.EtcdState
	filter    string
	filtering bool
	table     table.Model
}

func newEtcdModel(state application.EtcdState) etcdModel {
	return (etcdModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m etcdModel) setState(state application.EtcdState) etcdModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m etcdModel) startFilter(value string) etcdModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.table.Cursor())
}

func (m etcdModel) update(message tea.KeyPressMsg) etcdModel {
	key := message.Keystroke()
	// Terminals using the Kitty keyboard protocol (e.g. Ghostty) report
	// Shift+g as the base lowercase key plus a separate Shift modifier, so
	// Keystroke() yields "shift+g" instead of "G". message.Text is always
	// correctly-cased regardless of protocol; normalize once here so the
	// "G" case below matches under both encodings.
	if key == "shift+g" || message.Text == "G" {
		key = "G"
	}
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(m.table.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(m.table.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(m.table.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(m.table.Cursor())
	case "up", "k":
		m.table.MoveUp(1)
	case "down", "j":
		m.table.MoveDown(1)
	case "g":
		m.table.GotoTop()
	case "G":
		m.table.GotoBottom()
	}
	return m
}

func (m etcdModel) normalizeSelection(closest int) etcdModel {
	members := m.visibleMembers()
	m.table.SetRows(make([]table.Row, len(members)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(members)-1)))
	return m
}

func (m etcdModel) selected() (domain.EtcdMemberSnapshot, bool) {
	members := m.visibleMembers()
	cursor := m.table.Cursor()
	if len(members) == 0 || cursor < 0 || cursor >= len(members) {
		return domain.EtcdMemberSnapshot{}, false
	}
	return members[cursor], true
}

func (m etcdModel) visibleMembers() []domain.EtcdMemberSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Members
	}
	filtered := make([]domain.EtcdMemberSnapshot, 0, len(m.state.Value.Members))
	for _, member := range m.state.Value.Members {
		if strings.Contains(strings.ToLower(member.Hostname), query) {
			filtered = append(filtered, member)
		}
	}
	return filtered
}

func (m etcdModel) view(width int) string {
	contents := renderEtcdTable(width, m.visibleMembers(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m etcdModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	members := m.visibleMembers()
	if len(members) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading etcd members…"
		}
		return "No etcd members"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(members), m.table.Cursor(), rowCapacity)
	return renderEtcdTable(size.Width, members[start:end], m.table.Cursor()-start)
}

func renderEtcd(width int, state application.EtcdState) string {
	return newEtcdModel(state).view(width)
}

const defaultEtcdWidth = 120

type etcdColumn struct {
	header   string
	minWidth int
	value    func(domain.EtcdMemberSnapshot) string
	grow     bool
}

var etcdColumns = []etcdColumn{
	{header: "MEMBER", minWidth: 14, grow: true, value: func(member domain.EtcdMemberSnapshot) string { return fallback(member.Hostname) }},
	{header: "ROLE", minWidth: 8, value: etcdRole},
	{header: "DB SIZE", minWidth: 10, value: etcdDBSize},
	{header: "RAFT INDEX", minWidth: 12, value: etcdRaftIndex},
	{header: "ERRORS", minWidth: 10, value: etcdErrors},
}

func etcdRole(member domain.EtcdMemberSnapshot) string {
	if !member.StatusKnown {
		return "?"
	}
	if member.IsLeader {
		return "Leader"
	}
	if member.IsLearner {
		return "Learner"
	}
	return "Follower"
}

func etcdDBSize(member domain.EtcdMemberSnapshot) string {
	if !member.StatusKnown {
		return "-"
	}
	return formatBytes(member.DBSize)
}

func etcdRaftIndex(member domain.EtcdMemberSnapshot) string {
	if !member.StatusKnown {
		return "-"
	}
	return fmt.Sprintf("%d", member.RaftIndex)
}

func etcdErrors(member domain.EtcdMemberSnapshot) string {
	if !member.StatusKnown {
		return "?"
	}
	if len(member.Errors) == 0 {
		return "-"
	}
	return strings.Join(member.Errors, "; ")
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func renderEtcdTable(width int, members []domain.EtcdMemberSnapshot, selectedIndex int) string {
	widths := etcdColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeEtcdCells(&header, etcdHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, member := range members {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeEtcdCells(&row, etcdRowValues(member), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func etcdColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultEtcdWidth
	}
	widths := make([]int, len(etcdColumns))
	used := selectionWidth + columnSpacing*(len(etcdColumns)-1)
	growIndex := 0
	for index, column := range etcdColumns {
		widths[index] = column.minWidth
		used += column.minWidth
		if column.grow {
			growIndex = index
		}
	}
	if remaining := width - used; remaining > 0 {
		widths[growIndex] += remaining
	}
	return widths
}

func etcdHeaders() []string {
	headers := make([]string, len(etcdColumns))
	for index, column := range etcdColumns {
		headers[index] = column.header
	}
	return headers
}

func etcdRowValues(member domain.EtcdMemberSnapshot) []string {
	values := make([]string, len(etcdColumns))
	for index, column := range etcdColumns {
		values[index] = column.value(member)
	}
	return values
}

func writeEtcdCells(output *strings.Builder, values []string, widths []int) {
	for index, value := range values {
		if index > 0 {
			output.WriteByte(' ')
		}
		cell := ansi.Truncate(value, widths[index], "…")
		output.WriteString(cell)
		if padding := widths[index] - ansi.StringWidth(cell); padding > 0 && index < len(values)-1 {
			output.WriteString(strings.Repeat(" ", padding))
		}
	}
}
