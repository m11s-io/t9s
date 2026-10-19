package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/viewport"

	"github.com/m11s-io/t9s/internal/domain"
)

func sensitivityMarker(sensitive bool) string {
	if sensitive {
		return "⚠"
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type resourceKindsModel struct {
	status      directLoadStatus
	values      domain.ResourceKindSet
	err         string
	selectedKey string
	filter      string
	filtering   bool
	control     listControl
}

func newResourceKindsModel(state resourceBrowserState) resourceKindsModel {
	return (resourceKindsModel{values: state.Kinds, status: state.KindsStatus, err: state.KindsErr}).normalizeSelection(0)
}

func (m resourceKindsModel) setKinds(values domain.ResourceKindSet, status directLoadStatus, err string) resourceKindsModel {
	previous := m.selectedKey
	m.values, m.status, m.err = values, status, err
	cursor := m.control.Cursor()
	visible := m.visibleKinds()
	for index, kind := range visible {
		if kind.Type == previous {
			cursor = index
			break
		}
	}
	return m.normalizeSelection(cursor)
}

func (m resourceKindsModel) update(message tea.KeyPressMsg) resourceKindsModel {
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
	return m.normalizeSelection(m.control.Cursor())
}

func (m resourceKindsModel) normalizeSelection(closest int) resourceKindsModel {
	kinds := m.visibleKinds()
	m.control.SetCount(len(kinds))
	m.control.SetCursor(min(max(closest, 0), max(0, len(kinds)-1)))
	if kind, ok := m.selectedAtCursor(kinds); ok {
		m.selectedKey = kind.Type
	}
	return m
}

func (m resourceKindsModel) selectedAtCursor(kinds []domain.ResourceKindSnapshot) (domain.ResourceKindSnapshot, bool) {
	cursor := m.control.Cursor()
	if cursor < 0 || cursor >= len(kinds) {
		return domain.ResourceKindSnapshot{}, false
	}
	return kinds[cursor], true
}

func (m resourceKindsModel) selected() (domain.ResourceKindSnapshot, bool) {
	kinds := m.visibleKinds()
	cursor := m.control.Cursor()
	if len(kinds) == 0 || cursor < 0 || cursor >= len(kinds) {
		return domain.ResourceKindSnapshot{}, false
	}
	return kinds[cursor], true
}

func (m resourceKindsModel) selectedValue() domain.ResourceKindSnapshot {
	kind, _ := m.selected()
	return kind
}

func (m resourceKindsModel) visibleKinds() []domain.ResourceKindSnapshot {
	kinds := m.values.Kinds
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return kinds
	}
	filtered := make([]domain.ResourceKindSnapshot, 0, len(kinds))
	for _, kind := range kinds {
		if strings.Contains(strings.ToLower(kind.DisplayType), query) || strings.Contains(strings.ToLower(kind.Type), query) {
			filtered = append(filtered, kind)
		}
	}
	return filtered
}

func (m resourceKindsModel) view(width int) string {
	contents := renderResourceKindTable(width, m.visibleKinds(), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m resourceKindsModel) viewSized(size contentSize) string {
	kinds := m.visibleKinds()
	if m.status == directFailed && len(kinds) == 0 {
		return fallback(m.err)
	}
	if len(kinds) == 0 {
		if m.status == directLoading || m.status == "" {
			return "Loading resource kinds…"
		}
		if m.err != "" {
			return "No resource kinds\n" + m.err
		}
		return "No resource kinds"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(kinds), m.control.Cursor(), rowCapacity)
	content := renderResourceKindTable(size.Width, kinds[start:end], m.control.Cursor()-start)
	if m.status == directPartial || m.err != "" {
		content += "\n" + m.err
	}
	return content
}

const defaultResourceKindsWidth = 120

var resourceKindColumns = []tableColumn[domain.ResourceKindSnapshot]{
	{header: "TYPE", minWidth: 20, value: func(k domain.ResourceKindSnapshot) string { return fallback(k.DisplayType) }},
	{header: "NAMESPACE", minWidth: 14, value: func(k domain.ResourceKindSnapshot) string { return fallback(k.DefaultNamespace) }},
	{header: "ALIASES", minWidth: 20, grow: true, value: func(k domain.ResourceKindSnapshot) string {
		return sensitivityMarker(k.Sensitive) + strings.Join(k.Aliases, ", ")
	}},
}

func renderResourceKindTable(width int, kinds []domain.ResourceKindSnapshot, selectedIndex int) string {
	widths := resourceKindColumnWidths(width)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString(" ")
	writeTableCells(&header, resourceKindHeaders(), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))

	for index, kind := range kinds {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString(" ")
		writeTableCells(&row, resourceKindRowValues(kind), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func resourceKindColumnWidths(width int) []int {
	if width <= 0 {
		width = defaultResourceKindsWidth
	}
	return calculateColumnWidths(width, resourceKindColumns)
}

func resourceKindHeaders() []string {
	return tableHeaders(resourceKindColumns)
}

func resourceKindRowValues(kind domain.ResourceKindSnapshot) []string {
	return tableRowValues(kind, resourceKindColumns)
}

type resourceInstancesModel struct {
	status       directLoadStatus
	values       domain.ResourceInstanceSet
	err          string
	selectedKey  string
	selectedKind string
	filter       string
	filtering    bool
	control      listControl
}

func newResourceInstancesModel(state resourceBrowserState) resourceInstancesModel {
	return (resourceInstancesModel{values: state.Instances, status: state.InstancesStatus, err: state.InstancesErr, selectedKind: state.SelectedKind}).normalizeSelection(0)
}

func resourceInstanceIdentity(value domain.ResourceInstanceSnapshot) string {
	return value.Namespace + "\x00" + value.ID
}
func (m resourceInstancesModel) setInstances(values domain.ResourceInstanceSet, status directLoadStatus, err string) resourceInstancesModel {
	previous := m.selectedKey
	m.values, m.status, m.err = values, status, err
	cursor := m.control.Cursor()
	visible := m.visibleInstances()
	for index, item := range visible {
		if resourceInstanceIdentity(item) == previous {
			cursor = index
			break
		}
	}
	return m.normalizeSelection(cursor)
}

func (m resourceInstancesModel) update(message tea.KeyPressMsg) resourceInstancesModel {
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
	return m.normalizeSelection(m.control.Cursor())
}

func (m resourceInstancesModel) normalizeSelection(closest int) resourceInstancesModel {
	instances := m.visibleInstances()
	m.control.SetCount(len(instances))
	m.control.SetCursor(min(max(closest, 0), max(0, len(instances)-1)))
	if item, ok := m.selectedAtCursor(instances); ok {
		m.selectedKey = resourceInstanceIdentity(item)
	}
	return m
}

func (m resourceInstancesModel) selectedAtCursor(items []domain.ResourceInstanceSnapshot) (domain.ResourceInstanceSnapshot, bool) {
	cursor := m.control.Cursor()
	if cursor < 0 || cursor >= len(items) {
		return domain.ResourceInstanceSnapshot{}, false
	}
	return items[cursor], true
}

func (m resourceInstancesModel) selected() (domain.ResourceInstanceSnapshot, bool) {
	instances := m.visibleInstances()
	cursor := m.control.Cursor()
	if len(instances) == 0 || cursor < 0 || cursor >= len(instances) {
		return domain.ResourceInstanceSnapshot{}, false
	}
	return instances[cursor], true
}

func (m resourceInstancesModel) selectedValue() domain.ResourceInstanceSnapshot {
	instance, _ := m.selected()
	return instance
}

func (m resourceInstancesModel) visibleInstances() []domain.ResourceInstanceSnapshot {
	instances := m.values.Instances
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return instances
	}
	filtered := make([]domain.ResourceInstanceSnapshot, 0, len(instances))
	for _, instance := range instances {
		if strings.Contains(strings.ToLower(instance.ID), query) {
			filtered = append(filtered, instance)
		}
	}
	return filtered
}

var resourceInstanceColumns = []tableColumn[domain.ResourceInstanceSnapshot]{
	{header: "NAMESPACE", minWidth: 14, value: func(r domain.ResourceInstanceSnapshot) string { return fallback(r.Namespace) }},
	{header: "ID", minWidth: 20, value: func(r domain.ResourceInstanceSnapshot) string { return fallback(r.ID) }},
	{header: "PHASE", minWidth: 10, value: func(r domain.ResourceInstanceSnapshot) string { return fallback(r.Phase) }},
}

func resourceInstanceColumnWidths(width int, instances []domain.ResourceInstanceSnapshot) []int {
	if width <= 0 {
		width = defaultResourceKindsWidth
	}
	return contentColumnWidths(width, resourceInstanceColumns, instances)
}

func (m resourceInstancesModel) view(width int) string {
	contents := renderResourceInstanceTable(width, m.visibleInstances(), m.control.Cursor())
	if m.filter != "" || m.filtering {
		contents += "\nFILTER " + m.filter
	}
	return contents
}

func (m resourceInstancesModel) viewSized(size contentSize) string {
	instances := m.visibleInstances()
	if m.status == directFailed && len(instances) == 0 {
		return fallback(m.err)
	}
	if len(instances) == 0 {
		if m.status == directLoading || m.status == "" {
			return "Loading instances…"
		}
		if m.err != "" {
			return "No instances\n" + m.err
		}
		return "No instances"
	}
	rowCapacity := max(0, size.Height-1)
	start, end := resourceWindow(len(instances), m.control.Cursor(), rowCapacity)
	content := renderResourceInstanceRows(size.Width, instances[start:end], instances, m.control.Cursor()-start)
	if m.status == directPartial || m.err != "" {
		content += "\n" + m.err
	}
	return content
}

func renderResourceInstanceTable(width int, instances []domain.ResourceInstanceSnapshot, selectedIndex int) string {
	return renderResourceInstanceRows(width, instances, instances, selectedIndex)
}

func renderResourceInstanceRows(width int, instances, widthValues []domain.ResourceInstanceSnapshot, selectedIndex int) string {
	widths := resourceInstanceColumnWidths(width, widthValues)
	var output strings.Builder
	header := strings.Builder{}
	header.WriteString(" ")
	writeTableCells(&header, resourceInstanceHeaders(), widths)
	output.WriteString(renderTableHeader(header.String(), width, defaultK9sSkin()))

	for index, instance := range instances {
		output.WriteByte('\n')
		row := strings.Builder{}
		row.WriteString(" ")
		writeTableCells(&row, resourceInstanceRowValues(instance), widths)
		output.WriteString(renderSelectedRow(row.String(), width, index == selectedIndex, defaultK9sSkin()))
	}

	return output.String()
}

func resourceInstanceHeaders() []string {
	return tableHeaders(resourceInstanceColumns)
}

func resourceInstanceRowValues(instance domain.ResourceInstanceSnapshot) []string {
	return tableRowValues(instance, resourceInstanceColumns)
}

type resourceDetailModel struct {
	viewport   viewport.Model
	bodyHeight int
	status     directLoadStatus
	value      domain.ResourceInstanceSnapshot
	err        string
	sensitive  bool
}

func newResourceDetailModel() resourceDetailModel {
	return resourceDetailModel{viewport: viewport.New()}
}

func (m resourceDetailModel) update(message tea.KeyPressMsg) resourceDetailModel {
	key := message.String()
	switch key {
	case "up", "k":
		m.viewport.ScrollUp(1)
	case "down", "j":
		m.viewport.ScrollDown(1)
	case "g":
		m.viewport.GotoTop()
	case "G":
		m.viewport.GotoBottom()
	}
	return m
}

func (m *resourceDetailModel) scroll(message tea.KeyPressMsg) {
	key := message.String()
	switch key {
	case "up", "k":
		m.viewport.ScrollUp(1)
	case "down", "j":
		m.viewport.ScrollDown(1)
	case "g":
		m.viewport.GotoTop()
	case "G":
		m.viewport.GotoBottom()
	}
}

func (m resourceDetailModel) beginLoading(sensitive bool) resourceDetailModel {
	return resourceDetailModel{viewport: viewport.New(), status: directLoading, sensitive: sensitive}
}

func (m *resourceDetailModel) setViewport(size contentSize) {
	header := renderResourceDetailHeader(m.value, m.sensitive)
	headerLines := strings.Split(header, "\n")
	m.bodyHeight = max(0, size.Height-len(headerLines)-1)
	m.viewport.SetHeight(m.bodyHeight)
	m.viewport.SetContentLines(strings.Split(m.value.YAML, "\n"))
}

func (m resourceDetailModel) setResult(value domain.ResourceInstanceSnapshot, status directLoadStatus, err string, sensitive bool) resourceDetailModel {
	m.value, m.status, m.err, m.sensitive = value, status, err, sensitive
	return m
}

func renderResourceDetailHeader(instance domain.ResourceInstanceSnapshot, sensitive bool) string {
	fields := []detailField{
		{label: "NAMESPACE", value: fallback(instance.Namespace)},
		{label: "TYPE", value: fallback(instance.Type)},
		{label: "ID", value: fallback(instance.ID)},
		{label: "VERSION", value: fallback(instance.Version)},
		{label: "PHASE", value: fallback(instance.Phase)},
	}
	if sensitive {
		fields = append([]detailField{{label: "SENSITIVE", value: "⚠"}}, fields...)
	}
	return renderDetail(fields)
}

func (m resourceDetailModel) viewSized(size contentSize, instance domain.ResourceInstanceSnapshot, sensitive bool) string {
	header := renderResourceDetailHeader(instance, sensitive)
	headerLines := strings.Split(header, "\n")
	bodyHeight := max(0, size.Height-len(headerLines)-1)
	body := strings.Split(instance.YAML, "\n")
	m.viewport.SetHeight(bodyHeight)
	m.viewport.SetContentLines(body)
	start := min(m.viewport.YOffset(), len(body))
	end := min(start+bodyHeight, len(body))
	lines := append([]string(nil), headerLines...)
	lines = append(lines, "")
	for _, line := range body[start:end] {
		lines = append(lines, styleDetail(line))
	}
	return strings.Join(lines, "\n")
}

func (m resourceDetailModel) directViewSized(_ contentSize) string {
	switch m.status {
	case directLoading:
		return "Loading resource…"
	case directFailed:
		if m.value.YAML == "" {
			return sanitizeUntrustedText(firstNonEmpty(m.err, "Resource unavailable"))
		}
	}
	header := renderResourceDetailHeader(m.value, m.sensitive)
	headerLines := strings.Split(header, "\n")
	body := strings.Split(m.value.YAML, "\n")
	start := min(m.viewport.YOffset(), len(body))
	end := min(start+m.bodyHeight, len(body))
	lines := append([]string(nil), headerLines...)
	lines = append(lines, "")
	for _, line := range body[start:end] {
		lines = append(lines, styleDetail(line))
	}
	content := strings.Join(lines, "\n")
	if m.status == directPartial || m.err != "" {
		content += "\n" + sanitizeUntrustedText(m.err)
	}
	return content
}
