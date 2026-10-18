package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type directProcessesLoaded struct {
	request   directRequestStamp
	nodeID    string
	node      string
	processes domain.ProcessSet
	err       error
}

type directDisksLoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	disks   domain.DiskSet
	err     error
}

type directNetworkLoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	network domain.NetworkSet
	err     error
}

func directProcessesCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Processes(ctx, node)
		return directProcessesLoaded{request: request.stamp, nodeID: nodeID, node: node, processes: values, err: err}
	})
}

func directDisksCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Disks(ctx, node)
		return directDisksLoaded{request: request.stamp, nodeID: nodeID, node: node, disks: values, err: err}
	})
}

func directNetworkCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Network(ctx, node)
		return directNetworkLoaded{request: request.stamp, nodeID: nodeID, node: node, network: values, err: err}
	})
}

func (m *directModel) applyDiagnosticResult(message tea.Msg) bool {
	switch result := message.(type) {
	case directProcessesLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewProcesses, viewProcessDetail) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "processes"))
		m.processes = m.processes.setResult(result.node, result.processes, result.err)
	case directDisksLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewDisks, viewDiskDetail) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "disks"))
		m.disks = m.disks.setResult(result.node, result.disks, result.err)
	case directNetworkLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewNetwork, viewLinkDetail) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "network"))
		m.network = m.network.setResult(result.node, result.network, result.err)
	default:
		return false
	}
	return true
}

func (m *directModel) handleNodeDiagnosticKey(message tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.views.top().Kind != viewNodes || m.nodes.filtering || m.session == nil {
		return nil, false
	}
	node, ok := m.nodes.selected(m.cluster.nodes.Value.Nodes)
	if !ok {
		return nil, false
	}
	switch message.String() {
	case "p":
		return m.openDirectDiagnostic(node, viewProcesses), true
	case "k":
		return m.openDirectDiagnostic(node, viewDisks), true
	case "n":
		return m.openDirectDiagnostic(node, viewNetwork), true
	default:
		return nil, false
	}
}

func (m *directModel) handleDiagnosticKey(message tea.KeyPressMsg) (tea.Cmd, bool) {
	frame := m.views.top()
	switch frame.Kind {
	case viewProcessDetail, viewDiskDetail, viewLinkDetail:
		if message.String() == "esc" || message.String() == "q" {
			m.popDirectView()
			return nil, true
		}
		return nil, true
	case viewProcesses, viewDisks, viewNetwork:
		filtering := m.processes.filtering
		if frame.Kind == viewDisks {
			filtering = m.disks.filtering
		}
		if frame.Kind == viewNetwork {
			filtering = m.network.filtering
		}
		if !filtering && (message.String() == "q" || message.String() == "esc") {
			m.popDirectView()
			return nil, true
		}
		if message.String() == "r" && !filtering && m.session != nil {
			return m.refreshDirectDiagnostic(frame), true
		}
		if (message.String() == "enter" || message.String() == "d") && !filtering {
			switch frame.Kind {
			case viewProcesses:
				if process, ok := m.processes.selected(); ok {
					m.views = m.views.push(viewFrame{Kind: viewProcessDetail, Label: fmt.Sprintf("pid %d", process.PID), NodeID: frame.NodeID, NodeTarget: frame.NodeTarget, ProcessPID: process.PID})
				}
			case viewDisks:
				if disk, ok := m.disks.selected(); ok {
					m.views = m.views.push(viewFrame{Kind: viewDiskDetail, Label: "disk " + disk.DeviceName, NodeID: frame.NodeID, NodeTarget: frame.NodeTarget, DeviceName: disk.DeviceName})
				}
			case viewNetwork:
				if link, ok := m.network.selected(); ok {
					m.views = m.views.push(viewFrame{Kind: viewLinkDetail, Label: "link " + link.Name, NodeID: frame.NodeID, NodeTarget: frame.NodeTarget, LinkName: link.Name})
				}
			}
			return nil, true
		}
		switch frame.Kind {
		case viewProcesses:
			m.processes = m.processes.update(message)
		case viewDisks:
			m.disks = m.disks.update(message)
		case viewNetwork:
			m.network = m.network.update(message)
		}
		return nil, true
	default:
		return nil, false
	}
}

func (m directModel) directDiagnosticContent(size contentSize) (string, bool) {
	frame := m.views.top()
	switch frame.Kind {
	case viewProcesses:
		return m.processes.viewSized(size), true
	case viewDisks:
		return m.disks.viewSized(size), true
	case viewNetwork:
		return m.network.viewSized(size), true
	case viewProcessDetail:
		if value, ok := findProcessByPID(m.processes.values.Processes, frame.ProcessPID); ok {
			return renderProcessDetail(value), true
		}
		return "PROCESS DETAIL\nProcess unavailable", true
	case viewDiskDetail:
		if value, ok := findDiskByName(m.disks.values.Disks, frame.DeviceName); ok {
			return renderDiskDetail(value), true
		}
		return "DISK DETAIL\nDisk unavailable", true
	case viewLinkDetail:
		if value, ok := findLinkByName(m.network.values.Links, frame.LinkName); ok {
			return renderLinkDetail(value), true
		}
		return "LINK DETAIL\nLink unavailable", true
	default:
		return "", false
	}
}

func findProcessByPID(values []domain.ProcessSnapshot, pid int32) (domain.ProcessSnapshot, bool) {
	for _, value := range values {
		if value.PID == pid {
			return value, true
		}
	}
	return domain.ProcessSnapshot{}, false
}
func findDiskByName(values []domain.DiskSnapshot, name string) (domain.DiskSnapshot, bool) {
	for _, value := range values {
		if value.DeviceName == name {
			return value, true
		}
	}
	return domain.DiskSnapshot{}, false
}
func findLinkByName(values []domain.LinkSnapshot, name string) (domain.LinkSnapshot, bool) {
	for _, value := range values {
		if value.Name == name {
			return value, true
		}
	}
	return domain.LinkSnapshot{}, false
}

func directDiagnosticSlot(nodeID, scope string) string { return "node:" + nodeID + ":" + scope }

func (m directModel) directDiagnosticOpen(nodeID string, list, detail viewKind) bool {
	for _, frame := range m.views {
		if (frame.Kind == list || frame.Kind == detail) && frame.NodeID == nodeID {
			return true
		}
	}
	return false
}

func (m *directModel) openDirectDiagnostic(node domain.NodeSnapshot, kind viewKind) tea.Cmd {
	label := map[viewKind]string{viewProcesses: "processes", viewDisks: "disks", viewNetwork: "network"}[kind]
	scope := label
	nodeID := directDiagnosticNodeID(node)
	m.views = m.views.push(viewFrame{Kind: kind, Label: fallback(node.DisplayName()) + " > " + label, NodeID: nodeID, NodeTarget: node.Target()})
	request, _ := m.requests.beginWithContext(directDiagnosticSlot(nodeID, scope), m.generation(), m.sessionCtx)
	switch kind {
	case viewProcesses:
		m.processes = processesModel{status: directLoading}
		return directProcessesCommand(m.session, request, nodeID, node.Target())
	case viewDisks:
		m.disks = disksModel{status: directLoading}
		return directDisksCommand(m.session, request, nodeID, node.Target())
	default:
		m.network = networkModel{status: directLoading}
		return directNetworkCommand(m.session, request, nodeID, node.Target())
	}
}

func directDiagnosticNodeID(node domain.NodeSnapshot) string {
	if node.ID != "" {
		return node.ID
	}
	return node.Target()
}

func (m *directModel) refreshDirectDiagnostic(frame viewFrame) tea.Cmd {
	scope := map[viewKind]string{viewProcesses: "processes", viewDisks: "disks", viewNetwork: "network"}[frame.Kind]
	request, _ := m.requests.beginWithContext(directDiagnosticSlot(frame.NodeID, scope), m.generation(), m.sessionCtx)
	switch frame.Kind {
	case viewProcesses:
		return directProcessesCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	case viewDisks:
		return directDisksCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	default:
		return directNetworkCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	}
}

func (m *directModel) popDirectView() {
	frame := m.views.top()
	list, scope := viewKind(0), ""
	switch frame.Kind {
	case viewProcesses, viewProcessDetail:
		list, scope = viewProcesses, "processes"
	case viewDisks, viewDiskDetail:
		list, scope = viewDisks, "disks"
	case viewNetwork, viewLinkDetail:
		list, scope = viewNetwork, "network"
	}
	m.views, _ = m.views.pop()
	if list != 0 && !m.directDiagnosticOpen(frame.NodeID, list, map[viewKind]viewKind{viewProcesses: viewProcessDetail, viewDisks: viewDiskDetail, viewNetwork: viewLinkDetail}[list]) {
		m.requests.cancelSlot(directDiagnosticSlot(frame.NodeID, scope))
	}
}

func (m *directModel) cancelClosedDirectDiagnostics(previous viewStack) {
	seen := map[string]bool{}
	for _, frame := range previous {
		scope := map[viewKind]string{viewProcesses: "processes", viewProcessDetail: "processes", viewDisks: "disks", viewDiskDetail: "disks", viewNetwork: "network", viewLinkDetail: "network"}[frame.Kind]
		if scope == "" {
			continue
		}
		slot := directDiagnosticSlot(frame.NodeID, scope)
		if seen[slot] {
			continue
		}
		seen[slot] = true
		if !m.directDiagnosticOpen(frame.NodeID, map[string]viewKind{"processes": viewProcesses, "disks": viewDisks, "network": viewNetwork}[scope], map[string]viewKind{"processes": viewProcessDetail, "disks": viewDiskDetail, "network": viewLinkDetail}[scope]) {
			m.requests.cancelSlot(slot)
		}
	}
}
