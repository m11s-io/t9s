package tui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectProcessesKeyReadsAndRendersThroughSession(t *testing.T) {
	var gotTarget string
	reads := 0
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{ProcessReader: &testkit.FakeProcessReader{ListFunc: func(_ context.Context, node string) (domain.ProcessSet, error) {
			gotTarget = node
			reads++
			command := "kubelet"
			if reads > 1 {
				command = "kubelet-refreshed"
			}
			return domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 7, Command: command}}}, nil
		}}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()

	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.session = session
	model.sessionCtx = context.Background()
	model, command := updateDirect(model, keyPress('p'))
	require.Equal(t, viewProcesses, model.views.top().Kind)
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)

	assert.Equal(t, "worker-1", gotTarget)
	assert.Contains(t, model.View().Content, "kubelet")
	model, command = updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, 2, reads)
	assert.Contains(t, model.View().Content, "kubelet-refreshed")
}

func TestDirectDisksAndNetworkKeysReadAndOpenStableDetails(t *testing.T) {
	var diskTarget, networkTarget string
	diskReads, networkReads := 0, 0
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			DiskReader: &testkit.FakeDiskReader{ListFunc: func(_ context.Context, node string) (domain.DiskSet, error) {
				diskTarget = node
				diskReads++
				return domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sdb", Serial: fmt.Sprintf("disk-serial-%d", diskReads)}}}, nil
			}},
			NetworkReader: &testkit.FakeNetworkReader{ListFunc: func(_ context.Context, node string) (domain.NetworkSet, error) {
				networkTarget = node
				networkReads++
				return domain.NetworkSet{Links: []domain.LinkSnapshot{{Name: "eth0", HardwareAddr: fmt.Sprintf("aa:bb:%02x", networkReads)}}}, nil
			}},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()

	for _, test := range []struct {
		key      rune
		list     viewKind
		detail   viewKind
		contains string
	}{
		{key: 'k', list: viewDisks, detail: viewDiskDetail, contains: "disk-serial-2"},
		{key: 'n', list: viewNetwork, detail: viewLinkDetail, contains: "aa:bb:02"},
	} {
		model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
		model.session, model.sessionCtx = session, context.Background()
		model, command := updateDirect(model, keyPress(test.key))
		require.Equal(t, test.list, model.views.top().Kind)
		model, _ = applyDirectCommand(t, model, command)
		model, command = updateDirect(model, keyPress('r'))
		require.NotNil(t, command)
		model, _ = applyDirectCommand(t, model, command)
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
		require.Equal(t, test.detail, model.views.top().Kind)
		assert.Contains(t, model.View().Content, test.contains)
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
		assert.Equal(t, test.list, model.views.top().Kind)
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
		assert.Equal(t, viewNodes, model.views.top().Kind)
	}
	assert.Equal(t, "worker-1", diskTarget)
	assert.Equal(t, "worker-1", networkTarget)
	assert.Equal(t, 2, diskReads)
	assert.Equal(t, 2, networkReads)
}

func TestDirectDiagnosticDetailStaysOnProcessPIDAcrossRefresh(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.views = model.views.push(viewFrame{Kind: viewProcesses, Label: "worker-1 > processes", NodeID: "worker-id", NodeTarget: "worker-1"})
	request, _ := model.requests.begin("node:worker-id:processes", model.generation())
	first := domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 11, Command: "first"}, {PID: 22, Command: "selected"}}}
	model, _ = updateDirect(model, directProcessesLoaded{request: request.stamp, nodeID: "worker-id", node: "worker-1", processes: first})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, viewProcessDetail, model.views.top().Kind)

	refresh, _ := model.requests.begin("node:worker-id:processes", model.generation())
	model, _ = updateDirect(model, directProcessesLoaded{request: refresh.stamp, nodeID: "worker-id", node: "worker-1", processes: domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 22, Command: "selected refreshed"}, {PID: 11, Command: "first"}}}})
	assert.Contains(t, model.View().Content, "selected refreshed")
	assert.NotContains(t, model.View().Content, "first")
}

func TestDirectDiskAndLinkDetailsStayPinnedAcrossRefresh(t *testing.T) {
	t.Run("device name", func(t *testing.T) {
		model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
		model.views = model.views.push(viewFrame{Kind: viewDisks, Label: "worker-1 > disks", NodeID: "worker-id", NodeTarget: "worker-1"})
		request, _ := model.requests.begin("node:worker-id:disks", model.generation())
		initial := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "sda", Serial: "first"}, {DeviceName: "sdb", Serial: "selected"}}}
		model, _ = updateDirect(model, directDisksLoaded{request: request.stamp, nodeID: "worker-id", node: "worker-1", disks: initial})
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
		refresh, _ := model.requests.begin("node:worker-id:disks", model.generation())
		model, _ = updateDirect(model, directDisksLoaded{request: refresh.stamp, nodeID: "worker-id", node: "worker-1", disks: domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "sdb", Serial: "selected refreshed"}, {DeviceName: "sda", Serial: "first"}}}})
		assert.Contains(t, model.View().Content, "selected refreshed")
		assert.NotContains(t, model.View().Content, "Serial       first")
	})

	t.Run("link name", func(t *testing.T) {
		model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
		model.views = model.views.push(viewFrame{Kind: viewNetwork, Label: "worker-1 > network", NodeID: "worker-id", NodeTarget: "worker-1"})
		request, _ := model.requests.begin("node:worker-id:network", model.generation())
		initial := domain.NetworkSet{Links: []domain.LinkSnapshot{{Name: "eth0", HardwareAddr: "first"}, {Name: "eth1", HardwareAddr: "selected"}}}
		model, _ = updateDirect(model, directNetworkLoaded{request: request.stamp, nodeID: "worker-id", node: "worker-1", network: initial})
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
		model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
		refresh, _ := model.requests.begin("node:worker-id:network", model.generation())
		model, _ = updateDirect(model, directNetworkLoaded{request: refresh.stamp, nodeID: "worker-id", node: "worker-1", network: domain.NetworkSet{Links: []domain.LinkSnapshot{{Name: "eth1", HardwareAddr: "selected refreshed"}, {Name: "eth0", HardwareAddr: "first"}}}})
		assert.Contains(t, model.View().Content, "selected refreshed")
		assert.NotContains(t, model.View().Content, "eth0")
	})
}

func TestDirectDiagnosticNavigationCancelsAndRejectsLateResult(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.views = model.views.push(viewFrame{Kind: viewProcesses, Label: "worker-1 > processes", NodeID: "worker-id", NodeTarget: "worker-1"})
	request, _ := model.requests.beginWithContext("node:worker-id:processes", model.generation(), context.Background())
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.False(t, model.requests.current(request.stamp))
	model, _ = updateDirect(model, directProcessesLoaded{request: request.stamp, nodeID: "worker-id", node: "worker-1", processes: domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 9, Command: "late"}}}})
	assert.NotContains(t, model.View().Content, "late")
}

func TestDirectDiagnosticPaletteNavigationCancelsViewRead(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.views = model.views.push(viewFrame{Kind: viewProcesses, Label: "worker-1 > processes", NodeID: "worker-id", NodeTarget: "worker-1"})
	request, _ := model.requests.beginWithContext("node:worker-id:processes", model.generation(), context.Background())
	model, _ = updateDirect(model, keyPress(':'))
	for _, character := range "nodes" {
		model, _ = updateDirect(model, keyPress(character))
	}
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	model, _ = updateDirect(model, command())
	assert.Equal(t, viewNodes, model.views.top().Kind)
	assert.False(t, model.requests.current(request.stamp))
}

func TestDirectDiagnosticFilterCanTypeQThenBackAndOpenPalette(t *testing.T) {
	for _, kind := range []viewKind{viewProcesses, viewDisks, viewNetwork} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
			model.views = model.views.push(viewFrame{Kind: kind, Label: "worker-1 > diagnostic", NodeID: "worker-id", NodeTarget: "worker-1"})
			switch kind {
			case viewProcesses:
				model.processes.values = domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 1, Command: "query"}}}
			case viewDisks:
				model.disks.values = domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "query"}}}
			case viewNetwork:
				model.network.values = domain.NetworkSet{Links: []domain.LinkSnapshot{{Name: "query"}}}
			}
			model, _ = updateDirect(model, keyPress('/'))
			model, _ = updateDirect(model, keyPress('q'))
			require.Equal(t, kind, model.views.top().Kind, "q is filter input while the filter editor is active")
			filter, filtering := directDiagnosticFilter(model, kind)
			assert.Equal(t, "q", filter)
			assert.True(t, filtering)
			model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.Equal(t, kind, model.views.top().Kind)
			model, _ = updateDirect(model, keyPress('q'))
			assert.Equal(t, viewNodes, model.views.top().Kind, "q returns to nodes after accepting the filter")
			model, _ = updateDirect(model, keyPress(':'))
			assert.True(t, model.palette.active, "a hidden accepted query must not block the global palette")
		})
	}
}

func directDiagnosticFilter(model directModel, kind viewKind) (string, bool) {
	switch kind {
	case viewProcesses:
		return model.processes.filter, model.processes.filtering
	case viewDisks:
		return model.disks.filter, model.disks.filtering
	default:
		return model.network.filter, model.network.filtering
	}
}

func TestDirectNodesEmptyFilterEscClearsAndAllowsRefiltering(t *testing.T) {
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.session = session
	model, _ = updateDirect(model, keyPress('/'))
	for _, character := range "missing" {
		model, _ = updateDirect(model, keyPress(character))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, model.View().Content, "No nodes")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Empty(t, model.nodes.filter)
	model, _ = updateDirect(model, keyPress('/'))
	for _, character := range "worker" {
		model, _ = updateDirect(model, keyPress(character))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, model.View().Content, "worker-1")
}

func TestDirectProcessReadsDiscardLateResultAfterNodeABASwitch(t *testing.T) {
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{ProcessReader: &testkit.FakeProcessReader{ListFunc: func(_ context.Context, node string) (domain.ProcessSet, error) {
			return domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 1, Command: "fresh-" + node}}}, nil
		}}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-a", Name: "a"}, {ID: "node-b", Name: "b"}})
	model.session, model.sessionCtx = session, context.Background()
	model, _ = updateDirect(model, keyPress('p'))
	oldSlot := directDiagnosticSlot("node-a", "processes")
	model.requests.mu.Lock()
	oldStamp := model.requests.slots[oldSlot].request.stamp
	model.requests.mu.Unlock()
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyDown})
	model, command := updateDirect(model, keyPress('p'))
	model, _ = applyDirectCommand(t, model, command)
	assert.Contains(t, model.View().Content, "fresh-b")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyUp})
	model, command = updateDirect(model, keyPress('p'))
	model, _ = applyDirectCommand(t, model, command)
	assert.Contains(t, model.View().Content, "fresh-a")
	model, _ = updateDirect(model, directProcessesLoaded{request: oldStamp, nodeID: "node-a", node: "a", processes: domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 9, Command: "stale-a"}}}})
	assert.Contains(t, model.View().Content, "fresh-a")
	assert.NotContains(t, model.View().Content, "stale-a")
}

func TestDirectDiagnosticFailuresAreNormalizedAndPartialValuesRemainVisible(t *testing.T) {
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.views = model.views.push(viewFrame{Kind: viewProcesses, Label: "worker-1 > processes", NodeID: "worker-id", NodeTarget: "worker-1"})
	request, _ := model.requests.begin("node:worker-id:processes", model.generation())
	model, _ = updateDirect(model, directProcessesLoaded{request: request.stamp, nodeID: "worker-id", node: "worker-1", processes: domain.ProcessSet{Processes: []domain.ProcessSnapshot{{PID: 7, Command: "partial"}}}, err: errors.New("private transport details")})
	content := model.View().Content
	assert.Contains(t, content, "partial")
	assert.NotContains(t, content, "private transport details")
}

func TestDirectDiagnosticScreensShowLoadingEmptyFailureAndPartialData(t *testing.T) {
	for _, test := range []struct {
		name    string
		kind    viewKind
		scope   string
		loading string
		empty   string
		failure string
		row     string
		result  func(directRequestStamp, int) tea.Msg
	}{
		{
			name: "processes", kind: viewProcesses, scope: "processes", loading: "Loading processes…", empty: "No processes", failure: "Process data is unavailable", row: "kept-process",
			result: func(stamp directRequestStamp, outcome int) tea.Msg {
				values := domain.ProcessSet{}
				if outcome == 2 {
					values.Processes = []domain.ProcessSnapshot{{PID: 7, Command: "kept-process"}}
				}
				var err error
				if outcome != 0 {
					err = errors.New("private transport detail")
				}
				return directProcessesLoaded{request: stamp, nodeID: "worker-id", node: "worker-1", processes: values, err: err}
			},
		},
		{
			name: "disks", kind: viewDisks, scope: "disks", loading: "Loading disks…", empty: "No disks", failure: "Disk data is unavailable", row: "kept-disk",
			result: func(stamp directRequestStamp, outcome int) tea.Msg {
				values := domain.DiskSet{}
				if outcome == 2 {
					values.Disks = []domain.DiskSnapshot{{DeviceName: "kept-disk"}}
				}
				var err error
				if outcome != 0 {
					err = errors.New("private transport detail")
				}
				return directDisksLoaded{request: stamp, nodeID: "worker-id", node: "worker-1", disks: values, err: err}
			},
		},
		{
			name: "network", kind: viewNetwork, scope: "network", loading: "Loading network interfaces…", empty: "No network interfaces", failure: "Network data is unavailable", row: "kept-link",
			result: func(stamp directRequestStamp, outcome int) tea.Msg {
				values := domain.NetworkSet{}
				if outcome == 2 {
					values.Links = []domain.LinkSnapshot{{Name: "kept-link"}}
				}
				var err error
				if outcome != 0 {
					err = errors.New("private transport detail")
				}
				return directNetworkLoaded{request: stamp, nodeID: "worker-id", node: "worker-1", network: values, err: err}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
			model.views = model.views.push(viewFrame{Kind: test.kind, Label: test.name, NodeID: "worker-id", NodeTarget: "worker-1"})
			assert.Contains(t, model.View().Content, test.loading)
			request, _ := model.requests.begin(directDiagnosticSlot("worker-id", test.scope), model.generation())
			model, _ = updateDirect(model, test.result(request.stamp, 0))
			assert.Contains(t, model.View().Content, test.empty)
			request, _ = model.requests.begin(directDiagnosticSlot("worker-id", test.scope), model.generation())
			model, _ = updateDirect(model, test.result(request.stamp, 1))
			assert.Contains(t, model.View().Content, test.failure)
			assert.NotContains(t, model.View().Content, "private transport detail")
			request, _ = model.requests.begin(directDiagnosticSlot("worker-id", test.scope), model.generation())
			model, _ = updateDirect(model, test.result(request.stamp, 2))
			assert.Contains(t, model.View().Content, test.row)
			assert.Contains(t, model.View().Content, "PARTIAL")
			assert.NotContains(t, model.View().Content, "private transport detail")
		})
	}
}
