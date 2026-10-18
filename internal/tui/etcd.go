package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type etcdModel struct {
	filter      string
	filtering   bool
	selectedKey string
	control     listControl
}

func newEtcdModel() etcdModel { return etcdModel{} }

func etcdMemberKey(member domain.EtcdMemberSnapshot) string {
	if member.MemberID != 0 {
		return "id:" + strconv.FormatUint(member.MemberID, 10)
	}
	return "hostname:" + member.Hostname
}

func (m etcdModel) normalizeSelection(all []domain.EtcdMemberSnapshot, closest int) etcdModel {
	visible := m.visibleMembers(all)
	m.control.SetCount(len(visible))
	if len(visible) == 0 {
		m.control.SetCursor(0)
		if !etcdMemberExists(all, m.selectedKey) {
			m.selectedKey = ""
		}
		return m
	}
	for index, member := range visible {
		if etcdMemberKey(member) == m.selectedKey {
			m.control.SetCursor(index)
			return m
		}
	}
	m.control.SetCursor(min(max(closest, 0), len(visible)-1))
	if !etcdMemberExists(all, m.selectedKey) {
		m.selectedKey = etcdMemberKey(visible[m.control.Cursor()])
	}
	return m
}

func etcdMemberExists(members []domain.EtcdMemberSnapshot, key string) bool {
	if key == "" {
		return false
	}
	for _, member := range members {
		if etcdMemberKey(member) == key {
			return true
		}
	}
	return false
}

func (m etcdModel) rememberSelection(all []domain.EtcdMemberSnapshot) etcdModel {
	visible := m.visibleMembers(all)
	if len(visible) > 0 {
		m.selectedKey = etcdMemberKey(visible[m.control.Cursor()])
	}
	return m
}

func (m etcdModel) startFilter(value string, all []domain.EtcdMemberSnapshot) etcdModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(all, m.control.Cursor())
}

func (m etcdModel) update(message tea.KeyPressMsg, all []domain.EtcdMemberSnapshot) etcdModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(all, m.control.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(all, m.control.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(all, m.control.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(all, m.control.Cursor())
	case "up", "k":
		m.control.MoveUp(1)
		m = m.rememberSelection(all)
	case "down", "j":
		m.control.MoveDown(1)
		m = m.rememberSelection(all)
	case "g":
		m.control.GotoTop()
		m = m.rememberSelection(all)
	case "G":
		m.control.GotoBottom()
		m = m.rememberSelection(all)
	}
	return m
}

func (m etcdModel) selected(all []domain.EtcdMemberSnapshot) (domain.EtcdMemberSnapshot, bool) {
	members := m.visibleMembers(all)
	cursor := m.control.Cursor()
	if len(members) == 0 || cursor < 0 || cursor >= len(members) {
		return domain.EtcdMemberSnapshot{}, false
	}
	return members[cursor], true
}

func (m etcdModel) visibleMembers(members []domain.EtcdMemberSnapshot) []domain.EtcdMemberSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return members
	}
	filtered := make([]domain.EtcdMemberSnapshot, 0, len(members))
	for _, member := range members {
		if strings.Contains(strings.ToLower(member.Hostname), query) {
			filtered = append(filtered, member)
		}
	}
	return filtered
}

func (m etcdModel) view(width int, members []domain.EtcdMemberSnapshot) string {
	contents := renderEtcdTable(width, m.visibleMembers(members), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m etcdModel) viewSized(size contentSize, state directEtcdState) string {
	members := m.visibleMembers(state.Value.Members)
	if state.Status == directFailed && len(state.Value.Members) == 0 {
		return fallback(state.Err)
	}
	if len(members) == 0 {
		if state.Status == directLoading {
			return "Loading etcd members…"
		}
		if state.Status == directFailed && len(state.Value.Members) == 0 {
			return fallback(state.Err)
		}
		message := "No etcd members"
		if m.filter != "" {
			message = "No etcd members match " + m.filter
		}
		if state.Status == directPartial {
			partialMessage := state.Err
			if partialMessage == "" {
				partialMessage = "Some etcd data is unavailable"
			}
			return "ETCD PARTIAL: " + partialMessage + "\n" + message
		}
		return message
	}
	rowCapacity := max(0, size.Height-1)
	if state.Status == directPartial {
		rowCapacity = max(0, rowCapacity-1)
	}
	start, end := resourceWindow(len(members), m.control.Cursor(), rowCapacity)
	contents := renderEtcdTable(size.Width, members[start:end], m.control.Cursor()-start)
	if state.Status == directPartial {
		partialMessage := state.Err
		if partialMessage == "" {
			partialMessage = "Some etcd data is unavailable"
		}
		contents = "ETCD PARTIAL: " + partialMessage + "\n" + contents
	}
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

// legacyViewSized adapts reducer state for the legacy root until Task 18.
func (m etcdModel) legacyViewSized(size contentSize, state application.EtcdState) string {
	direct := directEtcdState{Value: state.Value}
	switch state.Status {
	case application.Loading, application.Idle:
		direct.Status = directLoading
	case application.Ready:
		direct.Status = directReady
	case application.Partial:
		direct.Status = directPartial
		direct.Err = "Some etcd data is unavailable"
	case application.Failed:
		direct.Status = directFailed
		direct.Err = "Etcd data is unavailable"
	}
	return m.viewSized(size, direct)
}

func renderEtcd(width int, state application.EtcdState) string {
	model := newEtcdModel()
	return model.view(width, state.Value.Members)
}

const defaultEtcdWidth = 120

var etcdColumns = []tableColumn[domain.EtcdMemberSnapshot]{
	{header: "MEMBER", minWidth: 14, grow: true, value: func(member domain.EtcdMemberSnapshot) string { return fallback(member.Hostname) }},
	{header: "ROLE", minWidth: 8, value: etcdRole},
	{header: "DB SIZE", minWidth: 10, value: etcdDBSize},
	{header: "RAFT INDEX", minWidth: 12, value: etcdRaftIndex},
	{header: "ERRORS", minWidth: 10, value: etcdErrors},
	{header: "ALARMS", minWidth: 10, value: etcdAlarms},
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

func etcdAlarms(member domain.EtcdMemberSnapshot) string {
	if len(member.Alarms) == 0 {
		return "-"
	}
	return strings.Join(member.Alarms, "; ")
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
	writeTableCells(&header, etcdHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, member := range members {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, etcdRowValues(member), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func etcdColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultEtcdWidth
	}
	return calculateColumnWidths(width, etcdColumns)
}

func etcdHeaders() []string {
	return tableHeaders(etcdColumns)
}

func etcdRowValues(member domain.EtcdMemberSnapshot) []string {
	return tableRowValues(member, etcdColumns)
}
