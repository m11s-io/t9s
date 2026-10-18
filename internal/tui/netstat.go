package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type netstatModel struct {
	state     application.SocketState
	filter    string
	filtering bool
	control   listControl
}

func newNetstatModel(state application.SocketState) netstatModel {
	return (netstatModel{state: state}).normalizeSelection(0)
}

func (m netstatModel) setState(state application.SocketState) netstatModel {
	previousIndex := m.control.Cursor()
	m.state = state
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
		return m.state.Value.Sockets
	}
	filtered := make([]domain.SocketSnapshot, 0, len(m.state.Value.Sockets))
	for _, socket := range m.state.Value.Sockets {
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
	contents := renderNetstatTable(width, m.visibleSockets(), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m netstatModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	sockets := m.visibleSockets()
	if len(sockets) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading sockets…"
		}
		return "No sockets"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(sockets), m.control.Cursor(), rowCapacity)
	return renderNetstatTable(size.Width, sockets[start:end], m.control.Cursor()-start)
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
	{header: "PROTO", minWidth: 6, value: func(s domain.SocketSnapshot) string { return fallback(s.Protocol) }},
	{header: "STATE", minWidth: 10, value: func(s domain.SocketSnapshot) string { return fallback(s.State) }},
	{header: "LOCAL", minWidth: 22, value: func(s domain.SocketSnapshot) string { return fallback(s.LocalAddress) }},
	{header: "REMOTE", minWidth: 22, grow: true, value: func(s domain.SocketSnapshot) string { return fallback(s.RemoteAddress) }},
	{header: "PROCESS", minWidth: 16, value: socketProcessLabel},
}

func renderNetstatTable(width int, sockets []domain.SocketSnapshot, selectedIndex int) string {
	widths := netstatColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeTableCells(&header, netstatHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, socket := range sockets {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, netstatRowValues(socket), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func netstatColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultNetstatWidth
	}
	return calculateColumnWidths(width, socketColumns)
}

func netstatHeaders() []string {
	return tableHeaders(socketColumns)
}

func netstatRowValues(socket domain.SocketSnapshot) []string {
	return tableRowValues(socket, socketColumns)
}
