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

type networkModel struct {
	state     application.NetworkState
	filter    string
	filtering bool
	table     table.Model
}

func newNetworkModel(state application.NetworkState) networkModel {
	return (networkModel{state: state, table: table.New()}).normalizeSelection(0)
}

func (m networkModel) setState(state application.NetworkState) networkModel {
	previousIndex := m.table.Cursor()
	m.state = state
	return m.normalizeSelection(previousIndex)
}

func (m networkModel) startFilter(value string) networkModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.table.Cursor())
}

func (m networkModel) update(message tea.KeyPressMsg) networkModel {
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

func (m networkModel) normalizeSelection(closest int) networkModel {
	links := m.visibleLinks()
	m.table.SetRows(make([]table.Row, len(links)))
	m.table.SetCursor(min(max(closest, 0), max(0, len(links)-1)))
	return m
}

func (m networkModel) selected() (domain.LinkSnapshot, bool) {
	links := m.visibleLinks()
	cursor := m.table.Cursor()
	if len(links) == 0 || cursor < 0 || cursor >= len(links) {
		return domain.LinkSnapshot{}, false
	}
	return links[cursor], true
}

func (m networkModel) selectedValue() domain.LinkSnapshot {
	link, _ := m.selected()
	return link
}

func (m networkModel) visibleLinks() []domain.LinkSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.state.Value.Links
	}
	filtered := make([]domain.LinkSnapshot, 0, len(m.state.Value.Links))
	for _, link := range m.state.Value.Links {
		if strings.Contains(strings.ToLower(link.Name), query) {
			filtered = append(filtered, link)
		}
	}
	return filtered
}

func (m networkModel) view(width int) string {
	contents := renderLinkTable(width, m.visibleLinks(), m.table.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m networkModel) viewSized(size contentSize) string {
	if m.state.Status == application.Failed {
		return fallback(m.state.Err)
	}
	links := m.visibleLinks()
	if len(links) == 0 {
		if m.state.Status == application.Loading || m.state.Status == application.Idle {
			return "Loading network interfaces…"
		}
		return "No network interfaces"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(links), m.table.Cursor(), rowCapacity)
	return renderLinkTable(size.Width, links[start:end], m.table.Cursor()-start)
}

const defaultNetworkWidth = 120

func joinLinkAddresses(link domain.LinkSnapshot) string {
	values := make([]string, len(link.Addresses))
	for index, address := range link.Addresses {
		values[index] = address.Address
	}
	return strings.Join(values, ", ")
}

type linkColumn struct {
	header   string
	minWidth int
	value    func(domain.LinkSnapshot) string
	grow     bool
}

var linkColumns = []linkColumn{
	{header: "LINK", minWidth: 10, value: func(l domain.LinkSnapshot) string { return fallback(l.Name) }},
	{header: "TYPE", minWidth: 8, value: func(l domain.LinkSnapshot) string { return fallback(l.Type) }},
	{header: "STATE", minWidth: 8, value: func(l domain.LinkSnapshot) string { return fallback(l.OperationalState) }},
	{header: "MTU", minWidth: 6, value: func(l domain.LinkSnapshot) string { return fmt.Sprintf("%d", l.MTU) }},
	{header: "ADDRESSES", minWidth: 20, grow: true, value: joinLinkAddresses},
}

func renderLinkTable(width int, links []domain.LinkSnapshot, selectedIndex int) string {
	widths := linkColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString("  ")
	writeLinkCells(&header, linkHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, link := range links {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeLinkCells(&row, linkRowValues(link), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func linkColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultNetworkWidth
	}
	widths := make([]int, len(linkColumns))
	used := selectionWidth + columnSpacing*(len(linkColumns)-1)
	growIndex := 0
	for index, column := range linkColumns {
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

func linkHeaders() []string {
	headers := make([]string, len(linkColumns))
	for index, column := range linkColumns {
		headers[index] = column.header
	}
	return headers
}

func linkRowValues(link domain.LinkSnapshot) []string {
	values := make([]string, len(linkColumns))
	for index, column := range linkColumns {
		values[index] = column.value(link)
	}
	return values
}

func writeLinkCells(output *strings.Builder, values []string, widths []int) {
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
