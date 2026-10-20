package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type containersModel struct {
	namespace domain.ContainerNamespace
	imageMode bool
	status    directLoadStatus
	values    domain.ContainerSet
	images    []domain.ImageSnapshot
	err       string
	partial   bool
	filter    string
	filtering bool
	control   listControl
	stream    ports.ImageStream
}

func newContainersModel(namespace domain.ContainerNamespace, images bool) containersModel {
	return containersModel{namespace: namespace, imageMode: images, status: directLoading}
}

func (m *containersModel) close() {
	if m.stream != nil {
		_ = m.stream.Close()
		m.stream = nil
	}
}

func (m containersModel) setContainers(values domain.ContainerSet, err error) containersModel {
	m.values = values
	m.err = ""
	m.status = directReady
	m.partial = len(values.Problems) > 0
	if err != nil {
		m.status, m.err = directFailed, "Container data is unavailable"
		if len(values.Containers) > 0 || len(values.Problems) > 0 {
			m.status, m.err = directPartial, sanitizeUntrustedText(err.Error())
		}
		m.partial = true
	} else if len(values.Problems) > 0 {
		m.err = sanitizeUntrustedText(strings.Join(values.Problems, "; "))
	}
	return m.normalizeSelection(0)
}

func (m *containersModel) attach(stream ports.ImageStream) { m.stream = stream }

func (m *containersModel) acceptImages(batch domain.ImageBatch, err error) bool {
	m.images = append(m.images, batch.Images...)
	if batch.Err != "" {
		m.partial, m.err = true, sanitizeUntrustedText(batch.Err)
	}
	if batch.Truncated {
		m.partial = true
		if m.err == "" {
			m.err = "Image list was truncated"
		}
	}
	if err != nil {
		m.partial = true
		m.err = "Image list ended before it was complete: " + sanitizeUntrustedText(err.Error())
		if len(m.images) == 0 {
			m.status = directFailed
		} else {
			m.status = directPartial
		}
		return true
	}
	if batch.EOF {
		if m.partial {
			m.status = directPartial
		} else {
			m.status = directReady
		}
		*m = m.normalizeSelection(m.control.Cursor())
		return true
	}
	m.status = directLoading
	*m = m.normalizeSelection(m.control.Cursor())
	return false
}

func (m containersModel) update(message tea.KeyPressMsg) containersModel {
	key := message.String()
	if m.filtering {
		switch key {
		case "esc":
			m.filter, m.filtering = "", false
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

func (m containersModel) normalizeSelection(closest int) containersModel {
	count := len(m.visibleContainers())
	if m.imageMode {
		count = len(m.visibleImages())
	}
	m.control.SetCount(count)
	m.control.SetCursor(min(max(closest, 0), max(0, count-1)))
	return m
}

func (m containersModel) visibleContainers() []domain.ContainerSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.values.Containers
	}
	filtered := make([]domain.ContainerSnapshot, 0, len(m.values.Containers))
	for _, row := range m.values.Containers {
		for _, value := range []string{row.Namespace, row.ID, row.Name, row.PodID, row.Image, row.Status} {
			if strings.Contains(strings.ToLower(value), query) {
				filtered = append(filtered, row)
				break
			}
		}
	}
	return filtered
}

func (m containersModel) visibleImages() []domain.ImageSnapshot {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.images
	}
	filtered := make([]domain.ImageSnapshot, 0, len(m.images))
	for _, row := range m.images {
		if strings.Contains(strings.ToLower(row.Reference), query) || strings.Contains(strings.ToLower(row.Digest), query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (m containersModel) selectedContainer() (domain.ContainerSnapshot, bool) {
	rows := m.visibleContainers()
	index := m.control.Cursor()
	if m.imageMode || index < 0 || index >= len(rows) {
		return domain.ContainerSnapshot{}, false
	}
	return rows[index], true
}

func (m containersModel) selectedImage() (domain.ImageSnapshot, bool) {
	rows := m.visibleImages()
	index := m.control.Cursor()
	if !m.imageMode || index < 0 || index >= len(rows) {
		return domain.ImageSnapshot{}, false
	}
	return rows[index], true
}

func (m containersModel) viewSized(size contentSize) string {
	if m.status == directLoading && m.imageMode && len(m.images) == 0 {
		return "Loading images…"
	}
	if m.status == directLoading && !m.imageMode && len(m.values.Containers) == 0 {
		return "Loading containers…"
	}
	if m.status == directFailed {
		return fallback(m.err)
	}
	content := ""
	if m.imageMode {
		rows := m.visibleImages()
		start, end := resourceWindow(len(rows), m.control.Cursor(), max(0, size.Height-2))
		content = renderImageRows(size.Width, rows[start:end], m.control.Cursor()-start)
		if len(rows) == 0 && m.status != directLoading {
			content = "No images"
		}
	} else {
		rows := m.visibleContainers()
		start, end := resourceWindow(len(rows), m.control.Cursor(), max(0, size.Height-2))
		content = renderContainerRows(size.Width, rows[start:end], m.control.Cursor()-start)
		if len(rows) == 0 && m.status != directLoading {
			content = "No containers"
		}
	}
	if m.status == directPartial || m.partial {
		content = "PARTIAL: " + fallback(m.err) + "\n" + content
	} else if m.status == directLoading {
		content = "LOADING: " + map[bool]string{true: "images", false: "containers"}[m.imageMode] + "\n" + content
	}
	if m.filtering || m.filter != "" {
		content += "\nFILTER " + sanitizeUntrustedText(m.filter)
	}
	return content
}

var containerColumns = []tableColumn[domain.ContainerSnapshot]{
	{header: "NAME", minWidth: 14, grow: true, value: func(row domain.ContainerSnapshot) string {
		return sanitizeUntrustedText(firstNonEmpty(row.Name, row.ID))
	}},
	{header: "STATUS", minWidth: 8, value: func(row domain.ContainerSnapshot) string { return sanitizeUntrustedText(row.Status) }},
	{header: "IMAGE", minWidth: 18, grow: true, value: func(row domain.ContainerSnapshot) string { return sanitizeUntrustedText(row.Image) }},
	{header: "PID", minWidth: 7, value: func(row domain.ContainerSnapshot) string { return fmt.Sprint(row.PID) }},
	{header: "MEMORY", minWidth: 10, value: func(row domain.ContainerSnapshot) string {
		if !row.MemoryKnown {
			return "-"
		}
		return formatBytes(int64(row.MemoryBytes))
	}},
	{header: "CPU TIME", minWidth: 9, value: func(row domain.ContainerSnapshot) string {
		if !row.CPUTimeKnown {
			return "-"
		}
		return row.CPUTime.String()
	}},
}

func renderContainerRows(width int, rows []domain.ContainerSnapshot, selected int) string {
	return renderContainerTable(width, rows, selected, containerColumns)
}

var imageColumns = []tableColumn[domain.ImageSnapshot]{
	{header: "REFERENCE", minWidth: 22, grow: true, value: func(row domain.ImageSnapshot) string { return sanitizeUntrustedText(row.Reference) }},
	{header: "DIGEST", minWidth: 18, grow: true, value: func(row domain.ImageSnapshot) string { return sanitizeUntrustedText(row.Digest) }},
	{header: "SIZE", minWidth: 10, value: func(row domain.ImageSnapshot) string { return formatBytes(int64(row.SizeBytes)) }},
}

func renderImageRows(width int, rows []domain.ImageSnapshot, selected int) string {
	return renderContainerTable(width, rows, selected, imageColumns)
}

func renderImageDetail(value domain.ImageSnapshot, width int) string {
	skin := defaultK9sSkin()
	lines := []string{skin.tableHeader.Render(fmt.Sprintf(" %-*s %s", fieldLabelWidth, "FIELD", "VALUE"))}
	fields := []detailField{
		{label: "REFERENCE", value: sanitizeUntrustedText(value.Reference)},
		{label: "DIGEST", value: sanitizeUntrustedText(value.Digest)},
		{label: "SIZE", value: formatBytes(int64(value.SizeBytes))},
	}
	valueWidth := max(1, width-fieldLabelWidth-2)
	for _, field := range fields {
		label := fmt.Sprintf(" %-*s", fieldLabelWidth, sanitizeUntrustedText(field.label))
		wrapped := strings.Split(ansi.Hardwrap(field.value, valueWidth, true), "\n")
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		lines = append(lines, skin.tableRow.Render(label)+skin.tableRow.Render(" "+wrapped[0]))
		for _, continuation := range wrapped[1:] {
			lines = append(lines, skin.tableRow.Render(strings.Repeat(" ", fieldLabelWidth+2)+continuation))
		}
	}
	return strings.Join(lines, "\n")
}

func renderContainerTable[T any](width int, rows []T, selected int, columns []tableColumn[T]) string {
	widths := calculateColumnWidths(width, columns)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteByte(' ')
	writeTableCells(&header, tableHeaders(columns), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))
	for index, rowValue := range rows {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteByte(' ')
		writeTableCells(&row, tableRowValues(rowValue, columns), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selected, defaultK9sSkin()))
	}
	return output.String()
}

func renderContainerDetail(value domain.ContainerSnapshot) string {
	cpu := "unknown"
	if value.CPUTimeKnown {
		cpu = value.CPUTime.String()
	}
	memory := "unknown"
	if value.MemoryKnown {
		memory = formatBytes(int64(value.MemoryBytes))
	}
	return renderDetail([]detailField{
		{label: "NAMESPACE", value: sanitizeUntrustedText(value.Namespace)},
		{label: "ID", value: sanitizeUntrustedText(value.ID)},
		{label: "NAME", value: sanitizeUntrustedText(value.Name)},
		{label: "POD", value: sanitizeUntrustedText(value.PodID)},
		{label: "IMAGE", value: sanitizeUntrustedText(value.Image)},
		{label: "PID", value: fmt.Sprint(value.PID)},
		{label: "STATUS", value: sanitizeUntrustedText(value.Status)},
		{label: "MEMORY", value: memory},
		{label: "CPU TIME", value: cpu},
	})
}
