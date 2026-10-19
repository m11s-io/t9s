package tui

import (
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
		[]string{"NAME", "ROLE", "STAGE", "HEALTH", "SERVICES", "K8S", "VERSION"},
		strings.Fields(ansi.Strip(lines[0])),
	)

	for index, values := range [][]string{
		{"control-plane-node-with-a-deliberately-long-display-name", "control", "running", "✓", "7✓", "0!", "0?", "Unknown", "v1.9.5"},
		{"worker-1", "worker", "maintenance", "!", "6✓", "1!", "0?", "Unknown", "v1.9.4"},
		{"10.0.0.3", "unknown", "unreachable", "?", "?", "Unknown", "-"},
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

func TestNodesRenderRetainsAllColumnsAtWidth80(t *testing.T) {
	rendered := renderNodes(80, nodesTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 4, "cells must truncate rather than wrap")
	assert.Equal(t,
		[]string{"NAME", "ROLE", "STAGE", "HEALTH", "SERVICES", "K8S", "VERSION"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, rendered, "…")
	assert.NotContains(t, rendered, "control-plane-node-with-a-deliberately-long-display-name")
	assert.NotContains(t, rendered, "...")
	for _, line := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(line), 80)
	}
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

	assert.Contains(t, rendered, "KUBERNETES Ready")
	assert.Contains(t, rendered, "ROLES      worker")
	assert.Contains(t, rendered, "KUBELET    v1.31.0")
	assert.Contains(t, rendered, "Ready=True")
}

func TestRenderNodeDetailOmitsKubernetesBlockWhenUncorrelated(t *testing.T) {
	rendered := renderNodeDetail(domain.NodeSnapshot{Name: "worker-1", Kubernetes: domain.KubernetesUnknown})

	assert.Contains(t, rendered, "KUBERNETES Unknown")
	assert.NotContains(t, rendered, "ROLES")
	assert.NotContains(t, rendered, "KUBELET")
}

func TestRenderProcessDetailShowsEveryField(t *testing.T) {
	rendered := renderProcessDetail(domain.ProcessSnapshot{
		PID: 42, PPID: 1, State: "Running", Threads: 4, CPUTime: 12.3,
		VirtualMemory: 4194304, ResidentMemory: 2097152,
		Command: "kubelet", Executable: "/usr/bin/kubelet", Args: "--config=/etc/kubelet.yaml", Label: "runtime",
	})

	assert.Contains(t, rendered, "PROCESS DETAIL")
	assert.Contains(t, rendered, "PID              42")
	assert.Contains(t, rendered, "PPID             1")
	assert.Contains(t, rendered, "STATE            Running")
	assert.Contains(t, rendered, "THREADS          4")
	assert.Contains(t, rendered, "CPU TIME         12.3s")
	assert.Contains(t, rendered, "VIRTUAL MEMORY   4.0 MiB")
	assert.Contains(t, rendered, "RESIDENT MEMORY  2.0 MiB")
	assert.Contains(t, rendered, "COMMAND          kubelet")
	assert.Contains(t, rendered, "EXECUTABLE       /usr/bin/kubelet")
	assert.Contains(t, rendered, "ARGS             --config=/etc/kubelet.yaml")
	assert.Contains(t, rendered, "LABEL            runtime")
}

func TestRenderDiskDetailShowsEveryField(t *testing.T) {
	rendered := renderDiskDetail(domain.DiskSnapshot{
		DeviceName: "sda", Model: "SAMSUNG MZVL", Serial: "S123456", Type: "ssd",
		SizeBytes: 536870912000, BusPath: "/pci0000:00/0000:00:1d.0", SystemDisk: true, ReadOnly: false,
	})

	assert.Contains(t, rendered, "DISK DETAIL")
	assert.Contains(t, rendered, "DEVICE      sda")
	assert.Contains(t, rendered, "MODEL       SAMSUNG MZVL")
	assert.Contains(t, rendered, "SERIAL      S123456")
	assert.Contains(t, rendered, "TYPE        ssd")
	assert.Contains(t, rendered, "SIZE        500.0 GiB")
	assert.Contains(t, rendered, "BUS PATH    /pci0000:00/0000:00:1d.0")
	assert.Contains(t, rendered, "SYSTEM DISK true")
	assert.Contains(t, rendered, "READ ONLY   false")
}

func TestRenderLinkDetailShowsScalarFieldsAndAddressesAndRoutes(t *testing.T) {
	rendered := renderLinkDetail(domain.LinkSnapshot{
		Name: "eth0", Type: "ether", OperationalState: "up", HardwareAddr: "aa:bb:cc:dd:ee:ff",
		MTU: 1500, Driver: "virtio_net",
		Addresses: []domain.NetworkAddress{{Address: "10.0.0.5/24", Scope: "global"}},
		Routes:    []domain.NetworkRoute{{Destination: "0.0.0.0/0", Gateway: "10.0.0.1", Table: "main"}},
	})

	assert.Contains(t, rendered, "LINK DETAIL")
	assert.Contains(t, rendered, "NAME        eth0")
	assert.Contains(t, rendered, "TYPE        ether")
	assert.Contains(t, rendered, "STATE       up")
	assert.Contains(t, rendered, "HW ADDRESS  aa:bb:cc:dd:ee:ff")
	assert.Contains(t, rendered, "MTU         1500")
	assert.Contains(t, rendered, "DRIVER      virtio_net")
	assert.Contains(t, rendered, "ADDRESSES")
	assert.Contains(t, rendered, "10.0.0.5/24 (global)")
	assert.Contains(t, rendered, "ROUTES")
	assert.Contains(t, rendered, "0.0.0.0/0 via 10.0.0.1 (main)")
}
