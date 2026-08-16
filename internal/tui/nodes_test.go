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
	"github.com/m11s-io/t9s/internal/application"
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
	nodes := newNodesModel(nodesTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('W'), keyPress('o'), keyPress('R'), keyPress('k'), keyPress('e'), keyPress('r'),
		{Code: tea.KeyEnter},
	} {
		nodes = nodes.update(message)
	}

	filtered := nodes.view(120)
	assert.Contains(t, filtered, "worker-1")
	assert.NotContains(t, filtered, "control-plane-node")
	assert.NotContains(t, filtered, "10.0.0.3")
	assert.Contains(t, filtered, "FILTER WoRker")

	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyEsc})
	cleared := nodes.view(120)
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
			nodes := newNodesModel(nodesTestState())
			nodes = nodes.startFilter(query)

			require.NotEmpty(t, nodes.visibleNodes())
		})
	}
}

func TestNodesSelectionSurvivesRefreshByID(t *testing.T) {
	nodes := newNodesModel(nodesTestState())
	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyDown})
	assert.Equal(t, "worker-1", nodes.selectedValue().DisplayName())

	refreshed := nodesTestState()
	refreshed.Value.Nodes[0], refreshed.Value.Nodes[1] = refreshed.Value.Nodes[1], refreshed.Value.Nodes[0]
	nodes = nodes.setState(refreshed)

	assert.Equal(t, "worker-1", nodes.selectedValue().DisplayName())
}

func TestNodesSelectionMovesToClosestRowWhenSelectedNodeDisappears(t *testing.T) {
	nodes := newNodesModel(nodesTestState())
	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyDown})

	refreshed := nodesTestState()
	refreshed.Value.Nodes = []domain.NodeSnapshot{
		refreshed.Value.Nodes[0],
		refreshed.Value.Nodes[2],
	}
	nodes = nodes.setState(refreshed)

	assert.Equal(t, "10.0.0.3", nodes.selectedValue().DisplayName())
}

func TestNodesSelectionResetsWhenRefreshedNodeSetIsEmpty(t *testing.T) {
	nodes := newNodesModel(nodesTestState())
	nodes = nodes.update(tea.KeyPressMsg{Code: tea.KeyDown})

	empty := nodesTestState()
	empty.Value.Nodes = nil
	nodes = nodes.setState(empty)

	_, ok := nodes.selected()
	assert.False(t, ok)
	assert.Equal(t, "No nodes", nodes.viewSized(contentSize{Width: 80, Height: 10}))
}

func TestNodesEnterShowsSelectedNodeDetail(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Nodes: nodesTestState()}, nil)
	root.splash = false

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyDown})
	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Nil(t, command)
	assert.Contains(t, root.View().Content, "NODE DETAIL")
	assert.Contains(t, root.View().Content, "NAME       worker-1")
	assert.Contains(t, root.View().Content, "KUBERNETES Unknown")
}

func TestNodesEscapeReturnsToListAndKeepsRootResource(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Nodes: nodesTestState()}, nil)
	root.splash = false
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})

	assert.Nil(t, command)
	assert.NotContains(t, root.View().Content, "NODE DETAIL")
}

func TestNodesRootRoutesQToActiveFilterInsteadOfQuitting(t *testing.T) {
	applicationModel := application.Model{Nodes: nodesTestState()}
	root := newModel(t.Context(), false, applicationModel, nil)
	root.nodes = root.nodes.update(keyPress('/'))

	updated, command := root.Update(keyPress('q'))

	assert.Nil(t, command)
	assert.Contains(t, updated.View().Content, "/q")
}

func TestNodesRootPreservesSelectionAcrossLoadedSnapshot(t *testing.T) {
	applicationModel := application.Model{Generation: 1, Nodes: nodesTestState()}
	root := newModel(t.Context(), false, applicationModel, nil)
	root.splash = false
	root.nodes = root.nodes.update(tea.KeyPressMsg{Code: tea.KeyDown})
	refreshed := nodesTestState().Value
	refreshed.Nodes[0], refreshed.Nodes[1] = refreshed.Nodes[1], refreshed.Nodes[0]

	updated, _ := root.Update(applicationMessage{message: application.NodesLoaded{
		Generation: 1,
		Nodes:      refreshed,
	}})

	assert.Equal(t, "worker-1", updated.(model).nodes.selectedValue().DisplayName())
}

func TestSpaceTogglesMarkOnSelectedRow(t *testing.T) {
	model := newNodesModel(nodesTestState())

	toggled := model.update(keyPress(' '))

	assert.True(t, toggled.isMarked(toggled.selectedValue().ID))

	untoggled := toggled.update(keyPress(' '))
	assert.False(t, untoggled.isMarked(untoggled.selectedValue().ID))
}

func TestMarksPersistAcrossFiltering(t *testing.T) {
	model := newNodesModel(nodesTestState())
	model = model.update(keyPress(' '))
	markedID := model.selectedValue().ID

	model = model.startFilter("")
	model = model.update(keyPress('x')) // arbitrary filter text
	model = model.update(tea.KeyPressMsg{Code: tea.KeyEsc})

	assert.True(t, model.isMarked(markedID))
}

func TestActionTargetsPrefersMarkedSetOverCursor(t *testing.T) {
	model := newNodesModel(nodesTestState())
	nodes := model.visibleNodes()
	require.GreaterOrEqual(t, len(nodes), 2)
	model = model.update(keyPress(' ')) // mark node at cursor 0
	model = model.moveSelection(1)
	model = model.update(keyPress(' ')) // mark node at cursor 1

	targets := model.actionTargets()

	assert.ElementsMatch(t, []string{nodes[0].Target(), nodes[1].Target()}, targets)
}

func TestActionTargetsFallsBackToCursorWhenNothingMarked(t *testing.T) {
	model := newNodesModel(nodesTestState())

	targets := model.actionTargets()

	assert.Equal(t, []string{model.selectedValue().Target()}, targets)
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

func nodesTestState() application.NodeState {
	return application.NodeState{
		Status: application.Partial,
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
// keyPress produces. Keystroke() for this shape is "shift+<lower>", not
// the bare uppercase letter — so it exercises a real, previously-unhandled
// input path that keyPress(upper) never did.
func shiftKeyPress(upper rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: unicode.ToLower(upper), Text: string(upper), Mod: tea.ModShift}
}

func TestNodesGotoBottomHandlesKittyShiftEncoding(t *testing.T) {
	model := newNodesModel(nodesTestState())

	updated := model.update(shiftKeyPress('G'))

	nodes := updated.visibleNodes()
	require.NotEmpty(t, nodes)
	assert.Equal(t, nodes[len(nodes)-1].ID, updated.selectedValue().ID, "shift+g (Kitty protocol) must jump to the bottom, same as legacy \"G\"")
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

func TestPKeyOpensProcessesForSelectedNode(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Nodes: nodesTestState()}, application.NewRunner(application.Dependencies{}))
	root.splash = false

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyDown})
	root, command := updateRoot(root, keyPress('p'))

	require.NotNil(t, command)
	assert.Equal(t, viewProcesses, root.views.top().Kind)

	message := command()
	root, _ = updateRoot(root, message)
	assert.Equal(t, "worker-1", root.application.Processes.Node)
}

func TestProcessDetailOpensFromSelectedRow(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Processes: application.ProcessesState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.ProcessSet{Processes: []domain.ProcessSnapshot{
			{PID: 42, Command: "kubelet"},
		}},
	}}, nil)
	root.splash = false
	root.views = root.views.push(viewFrame{Kind: viewProcesses, Label: "cp-1 > processes"})

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, viewProcessDetail, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "kubelet")
}

func TestKKeyOpensDisksForSelectedNode(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Nodes: nodesTestState()}, application.NewRunner(application.Dependencies{}))
	root.splash = false

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyDown})
	root, command := updateRoot(root, keyPress('k'))

	require.NotNil(t, command)
	assert.Equal(t, viewDisks, root.views.top().Kind)

	message := command()
	root, _ = updateRoot(root, message)
	assert.Equal(t, "worker-1", root.application.Disks.Node)
}

func TestNKeyOpensNetworkForSelectedNode(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Nodes: nodesTestState()}, application.NewRunner(application.Dependencies{}))
	root.splash = false

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyDown})
	root, command := updateRoot(root, keyPress('n'))

	require.NotNil(t, command)
	assert.Equal(t, viewNetwork, root.views.top().Kind)

	message := command()
	root, _ = updateRoot(root, message)
	assert.Equal(t, "worker-1", root.application.Network.Node)
}

func TestLinkDetailOpensFromSelectedRow(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Network: application.NetworkState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.NetworkSet{Links: []domain.LinkSnapshot{
			{Name: "eth0", Type: "ether"},
		}},
	}}, nil)
	root.splash = false
	root.views = root.views.push(viewFrame{Kind: viewNetwork, Label: "cp-1 > network"})

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, viewLinkDetail, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "eth0")
}

func TestOpenContextPickerActivatesOnceContextsLoad(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{OpenContextPicker: true}, nil)

	assert.False(t, root.contexts.active, "the picker must not activate before any contexts have loaded")
	assert.NotEmpty(t, root.notice)

	updated, _ := root.Update(applicationMessage{message: application.ContextsLoaded{
		Generation: root.application.Generation,
		Contexts:   []domain.ClusterContext{{Name: "prod"}, {Name: "staging"}},
	}})
	root = updated.(model)

	assert.True(t, root.contexts.active)

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	updated, _ = root.Update(applicationMessage{message: application.ContextsLoaded{
		Generation: root.application.Generation,
		Contexts:   []domain.ClusterContext{{Name: "prod"}, {Name: "staging"}},
	}})
	root = updated.(model)

	assert.False(t, root.contexts.active, "a later ContextsLoaded must not reopen a picker the user already closed")
}

func TestNodeFocusSelectsAndOpensDetailOnceNodesLoad(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{NodeFocus: "worker-1"}, nil)
	root.splash = false

	updated, _ := root.Update(applicationMessage{message: application.NodesLoaded{
		Generation: root.application.Generation,
		Nodes:      nodesTestState().Value,
	}})
	root = updated.(model)

	assert.Equal(t, viewNodeDetail, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "worker-1")

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	updated, _ = root.Update(applicationMessage{message: application.NodesLoaded{
		Generation: root.application.Generation,
		Nodes:      nodesTestState().Value,
	}})
	root = updated.(model)

	assert.Equal(t, viewNodes, root.views.top().Kind, "a later NodesLoaded must not re-trigger navigation once resolved")
}

func TestNodeFocusWithNoMatchLeavesNodesShowingNormally(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{NodeFocus: "does-not-exist"}, nil)
	root.splash = false

	updated, _ := root.Update(applicationMessage{message: application.NodesLoaded{
		Generation: root.application.Generation,
		Nodes:      nodesTestState().Value,
	}})
	root = updated.(model)

	assert.Equal(t, viewNodes, root.views.top().Kind)
}

func TestDiskDetailOpensFromSelectedRow(t *testing.T) {
	root := newModel(t.Context(), false, application.Model{Disks: application.DisksState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.DiskSet{Disks: []domain.DiskSnapshot{
			{DeviceName: "sda", Model: "SAMSUNG MZVL"},
		}},
	}}, nil)
	root.splash = false
	root.views = root.views.push(viewFrame{Kind: viewDisks, Label: "cp-1 > disks"})

	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, viewDiskDetail, root.views.top().Kind)
	assert.Contains(t, root.View().Content, "SAMSUNG MZVL")
}
