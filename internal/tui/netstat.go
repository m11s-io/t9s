package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/domain"
)

type netstatModel struct {
	changes   tableChanges
	status    directLoadStatus
	values    domain.SocketSet
	node      string
	err       string
	filter    string
	filtering bool
	control   listControl
}

func newDirectNetstatModel() netstatModel { return netstatModel{status: directLoading} }

func (m netstatModel) setResult(node string, values domain.SocketSet, err error) netstatModel {
	previousIndex := m.control.Cursor()
	m.values, m.node, m.err, m.status = values, node, "", directReady
	if err != nil {
		m.status, m.err = directFailed, "Socket data is unavailable"
		if len(values.Sockets) > 0 {
			m.status, m.err = directPartial, "Some socket data is unavailable"
		}
	}
	return m.normalizeSelection(previousIndex)
}

func (m netstatModel) update(message tea.KeyPressMsg) netstatModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter = ""
			m.filtering = false
			return m.normalizeSelection(m.control.Cursor())
		case "enter":
			m.filtering = false
			return m
		case "backspace":
			m.filter = trimLastRune(m.filter)
			return m.normalizeSelection(m.control.Cursor())
		}
		if text := printableText(message.Text); text != "" {
			m.filter += text
			return m.normalizeSelection(m.control.Cursor())
		}
		return m
	}

	switch key {
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m = m.normalizeSelection(m.control.Cursor())
	case "up", "k":
		m.control.MoveUp(1)
	case "down", "j":
		m.control.MoveDown(1)
	case "g":
		m.control.GotoTop()
	case "G":
		m.control.GotoBottom()
	}
	return m
}

func (m netstatModel) normalizeSelection(closest int) netstatModel {
	sockets := m.visibleSockets()
	m.control.SetCount(len(sockets))
	m.control.SetCursor(min(max(closest, 0), max(0, len(sockets)-1)))
	return m
}

func (m netstatModel) visibleSockets() []domain.SocketSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.values.Sockets
	}
	filtered := make([]domain.SocketSnapshot, 0, len(m.values.Sockets))
	for _, socket := range m.values.Sockets {
		if socketMatches(socket, query) {
			filtered = append(filtered, socket)
		}
	}
	return filtered
}

func socketMatches(socket domain.SocketSnapshot, query string) bool {
	for _, value := range []string{socket.Protocol, socket.State, socket.LocalAddress, socket.RemoteAddress, socket.ProcessName} {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (m netstatModel) view(width int) string {
	contents := renderNetstatTable(width, m.visibleSockets(), m.control.Cursor(), m.changes)
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m netstatModel) viewSized(size contentSize) string {
	if m.status == directFailed {
		return fallback(m.err)
	}
	sockets := m.visibleSockets()
	if len(sockets) == 0 {
		if m.status == directLoading || m.status == directLoadStatus("") {
			return "Loading sockets…"
		}
		if m.status == directPartial {
			return fallback(m.err)
		}
		return "No sockets"
	}
	warningRows := 0
	if m.status == directPartial {
		warningRows = 1
	}
	rowCapacity := max(0, size.Height-1-warningRows)
	start, end := resourceWindow(len(sockets), m.control.Cursor(), rowCapacity)
	content := renderNetstatRows(size.Width, sockets[start:end], sockets, m.control.Cursor()-start, m.changes)
	if warningRows > 0 {
		content = "SOCKETS PARTIAL: " + fallback(m.err) + "\n" + content
	}
	return content
}

const defaultNetstatWidth = 120

func socketProcessLabel(socket domain.SocketSnapshot) string {
	label := fallback(socket.ProcessName)
	if socket.PID != 0 {
		label += fmt.Sprintf(":%d", socket.PID)
	}
	return label
}

var socketColumns = []tableColumn[domain.SocketSnapshot]{
	{header: "PROTO", minWidth: 5, value: func(s domain.SocketSnapshot) string { return fallback(s.Protocol) }},
	{header: "STATE", minWidth: 5, value: func(s domain.SocketSnapshot) string { return fallback(s.State) }},
	{header: "LOCAL", minWidth: 5, value: func(s domain.SocketSnapshot) string { return fallback(s.LocalAddress) }},
	{header: "REMOTE", minWidth: 6, value: func(s domain.SocketSnapshot) string { return fallback(s.RemoteAddress) }},
	{header: "PROCESS", minWidth: 7, value: socketProcessLabel},
}

func renderNetstatTable(width int, sockets []domain.SocketSnapshot, selectedIndex int, changes ...tableChanges) string {
	return renderNetstatRows(width, sockets, sockets, selectedIndex, changes...)
}

func renderNetstatRows(width int, rows, widthValues []domain.SocketSnapshot, selectedIndex int, changes ...tableChanges) string {
	widths := netstatColumnWidths(width, widthValues)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString(" ")
	writeTableCells(&header, netstatHeaders(), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))

	for index, socket := range rows {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString(" ")
		writeTableCells(&row, netstatRowValues(socket), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, changedRowSkin(socketChangeKey(socket), changes)))
	}

	return output.String()
}

func netstatColumnWidths(width int, sockets []domain.SocketSnapshot) []int {
	if width <= 0 {
		width = defaultNetstatWidth
	}
	return contentColumnWidths(width, socketColumns, sockets)
}

func netstatHeaders() []string {
	return tableHeaders(socketColumns)
}

func netstatRowValues(socket domain.SocketSnapshot) []string {
	return tableRowValues(socket, socketColumns)
}
