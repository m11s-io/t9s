package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type networkModel struct {
	status    directLoadStatus
	values    domain.NetworkSet
	node      string
	err       string
	filter    string
	filtering bool
	control   listControl
}

func newNetworkModel(state application.NetworkState) networkModel {
	return (networkModel{status: directLoading}).setState(state)
}

func newDirectNetworkModel() networkModel { return networkModel{status: directLoading} }

func (m networkModel) setState(state application.NetworkState) networkModel {
	previousIndex := m.control.Cursor()
	m.values, m.node, m.err = state.Value, state.Node, state.Err
	switch state.Status {
	case application.Loading, application.Idle:
		m.status = directLoading
	case application.Partial:
		m.status = directPartial
	case application.Failed:
		m.status = directFailed
	default:
		m.status = directReady
	}
	return m.normalizeSelection(previousIndex)
}

func (m networkModel) setResult(node string, values domain.NetworkSet, err error) networkModel {
	selectedName, hadSelection := m.selectedName()
	previousIndex := m.control.Cursor()
	m.node, m.values, m.err, m.status = node, values, "", directReady
	if err != nil {
		m.status, m.err = directFailed, "Network data is unavailable"
		if len(values.Links) > 0 {
			m.status, m.err = directPartial, "Some network data is unavailable"
		}
	}
	m = m.normalizeSelection(previousIndex)
	if hadSelection {
		for index, link := range m.visibleLinks() {
			if link.Name == selectedName {
				m.control.SetCursor(index)
				break
			}
		}
	}
	return m
}

func (m networkModel) selectedName() (string, bool) {
	link, ok := m.selected()
	return link.Name, ok
}

func (m networkModel) startFilter(value string) networkModel {
	m.filter = value
	m.filtering = true
	return m.normalizeSelection(m.control.Cursor())
}

func (m networkModel) update(message tea.KeyPressMsg) networkModel {
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

func (m networkModel) normalizeSelection(closest int) networkModel {
	links := m.visibleLinks()
	m.control.SetCount(len(links))
	m.control.SetCursor(min(max(closest, 0), max(0, len(links)-1)))
	return m
}

func (m networkModel) selected() (domain.LinkSnapshot, bool) {
	links := m.visibleLinks()
	cursor := m.control.Cursor()
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
		return m.values.Links
	}
	filtered := make([]domain.LinkSnapshot, 0, len(m.values.Links))
	for _, link := range m.values.Links {
		if strings.Contains(strings.ToLower(link.Name), query) {
			filtered = append(filtered, link)
		}
	}
	return filtered
}

func (m networkModel) view(width int) string {
	contents := renderLinkTable(width, m.visibleLinks(), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m networkModel) viewSized(size contentSize) string {
	if m.status == directFailed {
		return fallback(m.err)
	}
	links := m.visibleLinks()
	if len(links) == 0 {
		if m.status == directLoading {
			return "Loading network interfaces…"
		}
		if m.status == directFailed || m.status == directPartial {
			return fallback(m.err)
		}
		return "No network interfaces"
	}
	warningRows := 0
	if m.status == directPartial {
		warningRows = 1
	}
	rowCapacity := max(0, size.Height-1-warningRows)
	start, end := resourceWindow(len(links), m.control.Cursor(), rowCapacity)
	content := renderLinkTable(size.Width, links[start:end], m.control.Cursor()-start)
	if warningRows > 0 {
		content = "NETWORK PARTIAL: " + fallback(m.err) + "\n" + content
	}
	return content
}

const defaultNetworkWidth = 120

func joinLinkAddresses(link domain.LinkSnapshot) string {
	values := make([]string, len(link.Addresses))
	for index, address := range link.Addresses {
		values[index] = address.Address
	}
	return strings.Join(values, ", ")
}

var linkColumns = []tableColumn[domain.LinkSnapshot]{
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
	writeTableCells(&header, linkHeaders(), widths)
	output.WriteString(renderSelectedRow(header.String(), width, false, defaultK9sSkin()))

	for index, link := range links {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString("  ")
		writeTableCells(&row, linkRowValues(link), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func linkColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultNetworkWidth
	}
	return calculateColumnWidths(width, linkColumns)
}

func linkHeaders() []string {
	return tableHeaders(linkColumns)
}

func linkRowValues(link domain.LinkSnapshot) []string {
	return tableRowValues(link, linkColumns)
}
