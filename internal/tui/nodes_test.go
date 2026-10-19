package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodesRenderSemanticColumns(t *testing.T) {
	rendered := renderNodes(120, nodesTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 4)
	assert.Equal(t,
		[]string{"NAME", "ROLE", "STAGE", "HEALTH", "K8S", "SERVICES", "VERSION"},
		strings.Fields(ansi.Strip(lines[0])),
	)

	for index, values := range [][]string{
		{"control-plane-node-with-a-deliberately-long-display-name", "control", "running", "✓", "Unknown", "7✓", "0!", "0?", "v1.9.5"},
		{"worker-1", "worker", "maintenance", "!", "Unknown", "6✓", "1!", "0?", "v1.9.4"},
		{"10.0.0.3", "unknown", "unreachable", "?", "Unknown", "?", "-"},
	} {
		assert.Equal(t, values, strings.Fields(strings.TrimPrefix(ansi.Strip(lines[index+1]), "> ")))
	}
}

func TestNodesK8SColumnReflectsCorrelatedReadiness(t *testing.T) {
	state := nodesTestState()
	state.Value.Nodes[0].Kubernetes = domain.KubernetesReady
	rendered := renderNodes(120, state)
	lines := strings.Split(rendered, "\n")

	assert.Contains(t, ansi.Strip(lines[1]), "Ready")
}

func TestNodesDisplayKubernetesRoleWhenTalosRoleIsUnknown(t *testing.T) {
	node := domain.NodeSnapshot{
		Name: "10.10.17.11", Role: domain.NodeRoleUnknown,
		KubernetesNode: &domain.KubernetesNodeSnapshot{Roles: []string{"control-plane"}},
	}

	values := nodeRowValues(node)

	assert.Equal(t, "control-plane", values[1])
	assert.Equal(t, domain.NodeRoleUnknown, node.Role, "Kubernetes display data must not change the Talos role used by actions")
}

func TestNodesFilterMatchesKubernetesRoleWhenTalosRoleIsUnknown(t *testing.T) {
	node := domain.NodeSnapshot{
		ID: "control-1", Name: "10.10.17.11", Role: domain.NodeRoleUnknown,
		KubernetesNode: &domain.KubernetesNodeSnapshot{Roles: []string{"control-plane"}},
	}
	model := newNodesModel().startFilter("control-plane", []domain.NodeSnapshot{node})

	assert.Equal(t, []domain.NodeSnapshot{node}, model.visibleNodes([]domain.NodeSnapshot{node}))
}

func TestNodesRenderRetainsAllColumnsAtWidth80(t *testing.T) {
	rendered := renderNodes(80, nodesTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 4, "cells must truncate rather than wrap")
	assert.Equal(t,
		[]string{"NAME", "ROLE", "STAGE", "HEALTH", "K8S", "SERVICES", "VERSION"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, rendered, "…")
	assert.NotContains(t, rendered, "control-plane-node-with-a-deliberately-long-display-name")
	assert.NotContains(t, rendered, "...")
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 80)
	}
}

func TestNodeTableDistributesColumnsAcrossWideTerminals(t *testing.T) {
	rendered := renderNodeTable(220, []domain.NodeSnapshot{
		{ID: "node-1", Name: "10.10.17.11"},
		{ID: "node-2", Name: "10.10.17.12"},
	}, 0, nil)

	header := ansi.Strip(strings.Split(rendered, "\n")[0])
	roleColumn := strings.Index(header, "ROLE")
	versionColumn := strings.Index(header, "VERSION")
	assert.GreaterOrEqual(t, roleColumn, 0)
	assert.Greater(t, roleColumn, 30, "wide terminals should distribute space after the name column")
	assert.GreaterOrEqual(t, versionColumn, 180, "the final column should reach the right side of a wide terminal")
	assert.LessOrEqual(t, 220-versionColumn, 12, "the final column should leave only a small right gutter")
}

func TestNodeTableHeaderUsesWhiteText(t *testing.T) {
	header := strings.Split(renderNodeTable(80, nodesTestState().Value.Nodes, 0, nil), "\n")[0]

	assert.Contains(t, header, "38;2;255;255;255m")
	assert.NotContains(t, header, "38;2;0;255;255m")
}

func TestNodeTableUsesOneCellOfLeftPadding(t *testing.T) {
	header := ansi.Strip(strings.Split(renderNodeTable(80, nodesTestState().Value.Nodes, 0, nil), "\n")[0])

	assert.True(t, strings.HasPrefix(header, " NAME"), "table content should sit one space inside the frame")
}

func TestNodesFilterMatchesDisplayNameAndRoleCaseInsensitively(t *testing.T) {
	nodes := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('W'), keyPress('o'), keyPress('R'), keyPress('k'), keyPress('e'), keyPress('r'),
		{Code: tea.KeyEnter},
	} {
		nodes = nodes.update(message, nodesTestState().Value.Nodes)
	}

	filtered := nodes.view(120, nodesTestState().Value.Nodes)
	assert.Contains(t, filtered, "worker-1")
	assert.NotContains(t, filtered, "control-plane-node")
	assert.NotContains(t, filtered, "10.0.0.3")
	assert.Contains(t, filtered, "FILTER WoRker")

	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyEsc}, nodesTestState().Value.Nodes)
	cleared := nodes.view(120, nodesTestState().Value.Nodes)
	assert.Contains(t, cleared, "worker-1")
	assert.Contains(t, cleared, "control-plane-node")
	assert.Contains(t, cleared, "10.0.0.3")
	assert.NotContains(t, cleared, "FILTER")
}

func TestNodesFilterSearchesEverySemanticValue(t *testing.T) {
	for name, query := range map[string]string{
		"display name":    "10.0.0.3",
		"role":            "CONTROL",
		"stage":           "maintenance",
		"health":          "unhealthy",
		"service summary": "6 healthy",
		"version":         "V1.9.5",
	} {
		t.Run(name, func(t *testing.T) {
			nodes := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)
			nodes = nodes.startFilter(query, nodesTestState().Value.Nodes)

			require.NotEmpty(t, nodes.visibleNodes(nodesTestState().Value.Nodes))
		})
	}
}

func TestNodesSelectionSurvivesRefreshByID(t *testing.T) {
	nodes := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)
	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyDown}, nodesTestState().Value.Nodes)
	assert.Equal(t, "worker-1", nodes.selectedValue(nodesTestState().Value.Nodes).DisplayName())

	refreshed := nodesTestState()
	refreshed.Value.Nodes[0], refreshed.Value.Nodes[1] = refreshed.Value.Nodes[1], refreshed.Value.Nodes[0]
	nodes = nodes.normalizeSelection(refreshed.Value.Nodes, nodes.control.Cursor())

	assert.Equal(t, "worker-1", nodes.selectedValue(refreshed.Value.Nodes).DisplayName())
}

func TestNodesSelectionMovesToClosestRowWhenSelectedNodeDisappears(t *testing.T) {
	nodes := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)
	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyDown}, nodesTestState().Value.Nodes)

	refreshed := nodesTestState()
	refreshed.Value.Nodes = []domain.NodeSnapshot{
		refreshed.Value.Nodes[0],
		refreshed.Value.Nodes[2],
	}
	nodes = nodes.normalizeSelection(refreshed.Value.Nodes, nodes.control.Cursor())

	assert.Equal(t, "10.0.0.3", nodes.selectedValue(refreshed.Value.Nodes).DisplayName())
}

func TestNodesSelectionResetsWhenRefreshedNodeSetIsEmpty(t *testing.T) {
	nodes := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)
	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyDown}, nodesTestState().Value.Nodes)

	empty := nodesTestState()
	empty.Value.Nodes = nil
	nodes = nodes.normalizeSelection(empty.Value.Nodes, nodes.control.Cursor())

	_, ok := nodes.selected(empty.Value.Nodes)
	assert.False(t, ok)
	assert.Equal(t, "No nodes", nodes.viewSized(contentSize{Width: 80, Height: 10}, empty))
}

func TestSpaceTogglesMarkOnSelectedRow(t *testing.T) {
	model := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)

	toggled := model.update(keyPress(' '), nodesTestState().Value.Nodes)

	assert.True(t, toggled.isMarked(toggled.selectedValue(nodesTestState().Value.Nodes).ID))

	untoggled := toggled.update(keyPress(' '), nodesTestState().Value.Nodes)
	assert.False(t, untoggled.isMarked(untoggled.selectedValue(nodesTestState().Value.Nodes).ID))
}

func TestMarksPersistAcrossFiltering(t *testing.T) {
	model := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)
	model = model.update(keyPress(' '), nodesTestState().Value.Nodes)
	markedID := model.selectedValue(nodesTestState().Value.Nodes).ID

	model = model.startFilter("", nodesTestState().Value.Nodes)
	model = model.update(keyPress('x'), nodesTestState().Value.Nodes) // arbitrary filter text
	model = model.update(tea.KeyPressMsg{Code: tea.KeyEsc}, nodesTestState().Value.Nodes)

	assert.True(t, model.isMarked(markedID))
}

func TestActionTargetsPrefersMarkedSetOverCursor(t *testing.T) {
	model := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)
	nodes := model.visibleNodes(nodesTestState().Value.Nodes)
	require.GreaterOrEqual(t, len(nodes), 2)
	model = model.update(keyPress(' '), nodesTestState().Value.Nodes) // mark node at cursor 0
	model = model.moveSelection(nodesTestState().Value.Nodes, 1)
	model = model.update(keyPress(' '), nodesTestState().Value.Nodes) // mark node at cursor 1

	targets := model.actionTargets(nodesTestState().Value.Nodes)

	assert.ElementsMatch(t, []string{nodes[0].Target(), nodes[1].Target()}, targets)
}

func TestActionTargetsFallsBackToCursorWhenNothingMarked(t *testing.T) {
	model := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)

	targets := model.actionTargets(nodesTestState().Value.Nodes)

	assert.Equal(t, []string{model.selectedValue(nodesTestState().Value.Nodes).Target()}, targets)
}

func TestRenderNodeTableMarksRowWithIndicator(t *testing.T) {
	nodes := nodesTestState().Value.Nodes
	marked := map[string]struct{}{nodes[1].ID: {}}

	rendered := renderNodeTable(120, nodes, 0, marked)
	lines := strings.Split(rendered, "\n")

	assert.True(t, strings.HasPrefix(ansi.Strip(lines[2]), "●"))
	assert.False(t, strings.Contains(ansi.Strip(lines[1]), "●"))
}

func TestNodesGolden(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			got := trimGoldenLines(renderNodes(width, nodesTestState())) + "\n"
			path := filepath.Join("testdata", "nodes-"+strconv.Itoa(width)+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}

			want, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
		})
	}
}

func trimGoldenLines(value string) string {
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return strings.Join(lines, "\n")
}

func nodesTestState() directNodeState {
	return directNodeState{
		Status: directPartial,
		Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{
				ID:         "node-cp-1",
				Name:       "control-plane-node-with-a-deliberately-long-display-name",
				Role:       domain.NodeRoleControl,
				Stage:      "running",
				Health:     domain.HealthHealthy,
				Services:   domain.ServiceSummary{Healthy: 7, Total: 7, Known: true},
				Kubernetes: domain.KubernetesUnknown,
				Version:    "v1.9.5",
			},
			{
				ID:         "node-worker-1",
				Name:       "worker-1",
				Role:       domain.NodeRoleWorker,
				Stage:      "maintenance",
				Health:     domain.HealthUnhealthy,
				Services:   domain.ServiceSummary{Healthy: 6, Total: 7, Known: true},
				Kubernetes: domain.KubernetesUnknown,
				Version:    "v1.9.4",
			},
			{
				ID:         "node-unknown-1",
				Addresses:  []string{"10.0.0.3"},
				Role:       domain.NodeRoleUnknown,
				Stage:      "unreachable",
				Health:     domain.HealthUnknown,
				Kubernetes: domain.KubernetesUnknown,
				Problem:    "unreachable",
			},
		}},
	}
}

func keyPress(value rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: value, Text: string(value)}
}

// shiftKeyPress models a Shift+<letter> keypress as reported by terminals
// using the Kitty keyboard protocol (e.g. Ghostty): the base lowercase key
// plus a separate Shift modifier, rather than the single uppercase rune
// keyPress produces. It is the realistic Kitty-protocol fixture used by
// uppercase-binding tests.
func shiftKeyPress(upper rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: unicode.ToLower(upper), Text: string(upper), Mod: tea.ModShift}
}

func TestKeyStringCanonicalizesLegacyAndKittyUppercase(t *testing.T) {
	legacy := keyPress('G')
	kitty := shiftKeyPress('G')

	assert.Equal(t, "G", legacy.String())
	assert.Equal(t, "G", kitty.String())
	assert.Equal(t, "shift+g", kitty.Keystroke(), "the fixture must exercise the Kitty modifier encoding")
}

func TestNodesGotoBottomHandlesKittyShiftEncoding(t *testing.T) {
	model := newNodesModel().normalizeSelection(nodesTestState().Value.Nodes, 0)

	updated := model.update(shiftKeyPress('G'), nodesTestState().Value.Nodes)

	nodes := updated.visibleNodes(nodesTestState().Value.Nodes)
	require.NotEmpty(t, nodes)
	assert.Equal(t, nodes[len(nodes)-1].ID, updated.selectedValue(nodesTestState().Value.Nodes).ID, "shift+g (Kitty protocol) must jump to the bottom, same as legacy \"G\"")
}

func TestRenderNodeDetailShowsKubernetesBlockWhenCorrelated(t *testing.T) {
	node := domain.NodeSnapshot{
		Name: "worker-1", Kubernetes: domain.KubernetesReady,
		KubernetesNode: &domain.KubernetesNodeSnapshot{
			Roles: []string{"worker"}, KubeletVersion: "v1.31.0",
			Conditions: []domain.KubernetesCondition{{Type: "Ready", Status: "True"}},
		},
	}

	rendered := renderNodeDetail(node)

	assert.Contains(t, rendered, "\x1b[38;2;0;255;255m")
	firstFieldLabel := fmt.Sprintf(" %-*s", fieldLabelWidth, "NAME")
	assert.True(t, strings.HasPrefix(strings.Split(rendered, "\n")[1], defaultK9sSkin().tableRow.Render(firstFieldLabel)), "field labels should use the cyan row color")
	assert.Equal(t, " FIELD            VALUE", ansi.Strip(strings.Split(rendered, "\n")[0]))
	assert.True(t, strings.HasPrefix(ansi.Strip(strings.Split(rendered, "\n")[1]), " NAME             worker-1"), "the first field should identify the node inside the standard inset")
	assert.NotContains(t, rendered, "NODE DETAIL")
	plain := ansi.Strip(rendered)
	assert.Contains(t, plain, "KUBERNETES       Ready")
	assert.Contains(t, plain, "ROLE             worker")
	assert.Contains(t, plain, "K8S ROLES        worker")
	assert.Contains(t, plain, "KUBELET          v1.31.0")
	assert.Contains(t, plain, "Ready=True")
}

func TestRenderNodeDetailOmitsKubernetesBlockWhenUncorrelated(t *testing.T) {
	rendered := renderNodeDetail(domain.NodeSnapshot{Name: "worker-1", Kubernetes: domain.KubernetesUnknown})

	plain := ansi.Strip(rendered)
	assert.Contains(t, plain, "KUBERNETES       Unknown")
	assert.NotContains(t, plain, "K8S ROLES")
	assert.NotContains(t, plain, "KUBELET")
}

func TestNodeAndProcessDetailsUseTheSameValueColumn(t *testing.T) {
	nodeLines := strings.Split(ansi.Strip(renderNodeDetail(domain.NodeSnapshot{Name: "worker-1"})), "\n")
	processLines := strings.Split(ansi.Strip(renderProcessDetail(domain.ProcessSnapshot{PID: 42})), "\n")

	assert.Equal(t,
		strings.Index(nodeLines[1], "worker-1"),
		strings.Index(processLines[1], "42"),
		"detail values should align across view types",
	)
}

func TestDetailValuesAlignWithMemoryTableValueColumn(t *testing.T) {
	detail := ansi.Strip(renderDiskDetail(domain.DiskSnapshot{DeviceName: "sda"}))
	memory := strings.Split(ansi.Strip(renderMemoryTable(120, []memoryMetric{{Label: "Total", Value: "23.5 GiB"}}, 0)), "\n")

	assert.Equal(t,
		strings.Index(strings.Split(detail, "\n")[1], "sda"),
		strings.Index(memory[1], "23.5 GiB"),
		"detail values should align with the METRIC/VALUE table columns",
	)
}

func TestDetailViewsUseWhiteFieldValueColumnHeaders(t *testing.T) {
	rendered := renderDetail([]detailField{{label: "NAME", value: "worker-1"}})
	lines := strings.Split(rendered, "\n")
	plain := strings.Split(ansi.Strip(rendered), "\n")
	header := fmt.Sprintf(" %-*s %s", fieldLabelWidth, "FIELD", "VALUE")

	require.Len(t, lines, 2)
	assert.Equal(t, defaultK9sSkin().tableHeader.Render(header), lines[0], "the generic detail headers should use the white table-header style")
	assert.Equal(t, " FIELD"+strings.Repeat(" ", fieldLabelWidth-len("FIELD")+1)+"VALUE", plain[0])
	label := fmt.Sprintf(" %-*s", fieldLabelWidth, "NAME")
	assert.True(t, strings.HasPrefix(lines[1], defaultK9sSkin().tableRow.Render(label)), "detail field labels should use the cyan row style")
	assert.Equal(t, strings.Index(plain[0], "VALUE"), strings.Index(plain[1], "worker-1"), "the VALUE header should align with detail values")
}

func TestRenderDetailSanitizesRemoteValuesWithoutAddingRows(t *testing.T) {
	rendered := renderDetail([]detailField{{label: "COMMAND", value: "safe\x1b[2J\r\ninjected"}})
	plain := ansi.Strip(rendered)

	require.Len(t, strings.Split(plain, "\n"), 2)
	assert.NotContains(t, rendered, "\x1b[2J")
	assert.NotContains(t, plain, "\r")
	assert.Contains(t, plain, "safeinjected")
}

func TestRenderProcessDetailShowsEveryField(t *testing.T) {
	rendered := renderProcessDetail(domain.ProcessSnapshot{
		PID: 42, PPID: 1, State: "Running", Threads: 4, CPUTime: 12.3,
		VirtualMemory: 4194304, ResidentMemory: 2097152,
		Command: "kubelet", Executable: "/usr/bin/kubelet", Args: "--config=/etc/kubelet.yaml", Label: "runtime",
	})

	plain := ansi.Strip(rendered)
	assert.NotContains(t, plain, "PROCESS DETAIL")
	assert.Contains(t, plain, "PID              42")
	assert.Contains(t, plain, "PPID             1")
	assert.Contains(t, plain, "STATE            Running")
	assert.Contains(t, plain, "THREADS          4")
	assert.Contains(t, plain, "CPU TIME         12.3s")
	assert.Contains(t, plain, "VIRTUAL MEMORY   4.0 MiB")
	assert.Contains(t, plain, "RESIDENT MEMORY  2.0 MiB")
	assert.Contains(t, plain, "COMMAND          kubelet")
	assert.Contains(t, plain, "EXECUTABLE       /usr/bin/kubelet")
	assert.Contains(t, plain, "ARGS             --config=/etc/kubelet.yaml")
	assert.Contains(t, plain, "LABEL            runtime")
}

func TestRenderDiskDetailShowsEveryField(t *testing.T) {
	rendered := renderDiskDetail(domain.DiskSnapshot{
		DeviceName: "sda", Model: "SAMSUNG MZVL", Serial: "S123456", Type: "ssd",
		SizeBytes: 536870912000, BusPath: "/pci0000:00/0000:00:1d.0", SystemDisk: true, ReadOnly: false,
	})

	plain := ansi.Strip(rendered)
	assert.NotContains(t, plain, "DISK DETAIL")
	assert.Contains(t, plain, "DEVICE           sda")
	assert.Contains(t, plain, "MODEL            SAMSUNG MZVL")
	assert.Contains(t, plain, "SERIAL           S123456")
	assert.Contains(t, plain, "TYPE             ssd")
	assert.Contains(t, plain, "SIZE             500.0 GiB")
	assert.Contains(t, plain, "BUS PATH         /pci0000:00/0000:00:1d.0")
	assert.Contains(t, plain, "SYSTEM DISK      true")
	assert.Contains(t, plain, "READ ONLY        false")
}

func TestRenderLinkDetailShowsScalarFieldsAndAddressesAndRoutes(t *testing.T) {
	rendered := renderLinkDetail(domain.LinkSnapshot{
		Name: "eth0", Type: "ether", OperationalState: "up", HardwareAddr: "aa:bb:cc:dd:ee:ff",
		MTU: 1500, Driver: "virtio_net",
		Addresses: []domain.NetworkAddress{{Address: "10.0.0.5/24", Scope: "global"}},
		Routes:    []domain.NetworkRoute{{Destination: "0.0.0.0/0", Gateway: "10.0.0.1", Table: "main"}},
	})

	plain := ansi.Strip(rendered)
	assert.NotContains(t, plain, "LINK DETAIL")
	assert.Contains(t, plain, "NAME             eth0")
	assert.Contains(t, plain, "TYPE             ether")
	assert.Contains(t, plain, "STATE            up")
	assert.Contains(t, plain, "HW ADDRESS       aa:bb:cc:dd:ee:ff")
	assert.Contains(t, plain, "MTU              1500")
	assert.Contains(t, plain, "DRIVER           virtio_net")
	assert.Contains(t, plain, "ADDRESSES        10.0.0.5/24 (global)")
	assert.Contains(t, plain, "ROUTES           0.0.0.0/0 via 10.0.0.1 (main)")
}
