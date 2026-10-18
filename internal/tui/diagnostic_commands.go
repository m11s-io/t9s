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

type directSocketsLoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	sockets domain.SocketSet
	err     error
}

type directMountsLoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	mounts  domain.MountSet
	err     error
}

type directMemoryLoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	memory  domain.MemorySnapshot
	err     error
}

type directCPULoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	value   domain.CPUSnapshot
	err     error
}

type directTimeLoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	value   domain.TimeSnapshot
	err     error
}

type directTimeCheckLoaded struct {
	request directRequestStamp
	nodeID  string
	node    string
	server  string
	value   domain.TimeCheckResult
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

func directSocketsCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Sockets(ctx, node)
		return directSocketsLoaded{request: request.stamp, nodeID: nodeID, node: node, sockets: values, err: err}
	})
}

func directMountsCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Mounts(ctx, node)
		return directMountsLoaded{request: request.stamp, nodeID: nodeID, node: node, mounts: values, err: err}
	})
}

func directMemoryCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Memory(ctx, node)
		return directMemoryLoaded{request: request.stamp, nodeID: nodeID, node: node, memory: values, err: err}
	})
}

func directCPUCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.CPU(ctx, node)
		return directCPULoaded{request: request.stamp, nodeID: nodeID, node: node, value: values, err: err}
	})
}

func directTimeCommand(session *application.Session, request directRequest, nodeID, node string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Time(ctx, node)
		return directTimeLoaded{request: request.stamp, nodeID: nodeID, node: node, value: values, err: err}
	})
}

func directTimeCheckCommand(session *application.Session, request directRequest, nodeID, node, server string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.CheckTime(ctx, node, server)
		return directTimeCheckLoaded{request: request.stamp, nodeID: nodeID, node: node, server: server, value: values, err: err}
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
	case directSocketsLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewNetstat, 0) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "sockets"))
		m.netstat = m.netstat.setResult(result.node, result.sockets, result.err)
	case directMountsLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewMounts, 0) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "mounts"))
		m.mounts = m.mounts.setResult(result.node, result.mounts, result.err)
	case directMemoryLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewMemory, 0) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "memory"))
		m.memory = m.memory.setResult(result.node, result.memory, result.err)
	case directCPULoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewCPU, 0) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "cpu"))
		m.cpu = m.cpu.setResult(result.node, result.value, result.err)
	case directTimeLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewTime, 0) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "time"))
		m.time = m.time.setReadResult(result.node, result.value, result.err)
	case directTimeCheckLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewTime, 0) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "time-check"))
		m.time = m.time.setCheckResult(result.node, result.server, result.value, result.err)
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
	case "s":
		return m.openDirectDiagnostic(node, viewNetstat), true
	case "m":
		return m.openDirectDiagnostic(node, viewMounts), true
	case "f":
		return m.openDirectDiagnostic(node, viewMemory), true
	case "u":
		return m.openDirectDiagnostic(node, viewCPU), true
	case "t":
		return m.openDirectDiagnostic(node, viewTime), true
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
	case viewProcesses, viewDisks, viewNetwork, viewNetstat, viewMounts, viewMemory, viewCPU, viewTime:
		filtering := m.processes.filtering
		if frame.Kind == viewDisks {
			filtering = m.disks.filtering
		}
		if frame.Kind == viewNetwork {
			filtering = m.network.filtering
		}
		if frame.Kind == viewNetstat {
			filtering = m.netstat.filtering
		}
		if frame.Kind == viewMounts {
			filtering = m.mounts.filtering
		}
		if frame.Kind == viewMemory {
			filtering = m.memory.filtering
		}
		if frame.Kind == viewCPU {
			filtering = m.cpu.filtering
		}
		if frame.Kind == viewTime {
			filtering = m.time.filtering
		}
		if !filtering && (message.String() == "q" || message.String() == "esc") {
			m.popDirectView()
			return nil, true
		}
		if message.String() == "r" && !filtering && m.session != nil {
			return m.refreshDirectDiagnostic(frame), true
		}
		if frame.Kind == viewTime && message.String() == "c" && !filtering {
			prompt := newTimePromptModel()
			m.timePrompt = &prompt
			return prompt.input.Focus(), true
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
		case viewNetstat:
			m.netstat = m.netstat.update(message)
		case viewMounts:
			m.mounts = m.mounts.update(message)
		case viewMemory:
			m.memory = m.memory.update(message)
		case viewCPU:
			m.cpu = m.cpu.update(message)
		case viewTime:
			m.time = m.time.update(message)
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
	case viewNetstat:
		return m.netstat.viewSized(size), true
	case viewMounts:
		return m.mounts.viewSized(size), true
	case viewMemory:
		return m.memory.viewSized(size), true
	case viewCPU:
		return m.cpu.viewSized(size), true
	case viewTime:
		return m.time.viewSized(size), true
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
	label := map[viewKind]string{viewProcesses: "processes", viewDisks: "disks", viewNetwork: "network", viewNetstat: "netstat", viewMounts: "mounts", viewMemory: "memory", viewCPU: "CPU", viewTime: "time"}[kind]
	scope := map[viewKind]string{viewProcesses: "processes", viewDisks: "disks", viewNetwork: "network", viewNetstat: "sockets", viewMounts: "mounts", viewMemory: "memory", viewCPU: "cpu", viewTime: "time"}[kind]
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
	case viewNetwork:
		m.network = networkModel{status: directLoading}
		return directNetworkCommand(m.session, request, nodeID, node.Target())
	case viewNetstat:
		m.netstat = newDirectNetstatModel()
		return directSocketsCommand(m.session, request, nodeID, node.Target())
	case viewMounts:
		m.mounts = newDirectMountsModel()
		return directMountsCommand(m.session, request, nodeID, node.Target())
	case viewMemory:
		m.memory = newDirectMemoryModel()
		return directMemoryCommand(m.session, request, nodeID, node.Target())
	case viewCPU:
		m.cpu = newDirectCPUModel()
		return directCPUCommand(m.session, request, nodeID, node.Target())
	default:
		m.time = newDirectTimeModel()
		return directTimeCommand(m.session, request, nodeID, node.Target())
	}
}

func directDiagnosticNodeID(node domain.NodeSnapshot) string {
	if node.ID != "" {
		return node.ID
	}
	return node.Target()
}

func (m *directModel) refreshDirectDiagnostic(frame viewFrame) tea.Cmd {
	scope := map[viewKind]string{viewProcesses: "processes", viewDisks: "disks", viewNetwork: "network", viewNetstat: "sockets", viewMounts: "mounts", viewMemory: "memory", viewCPU: "cpu", viewTime: "time"}[frame.Kind]
	request, _ := m.requests.beginWithContext(directDiagnosticSlot(frame.NodeID, scope), m.generation(), m.sessionCtx)
	switch frame.Kind {
	case viewProcesses:
		return directProcessesCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	case viewDisks:
		return directDisksCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	case viewNetwork:
		return directNetworkCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	case viewNetstat:
		return directSocketsCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	case viewMounts:
		return directMountsCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	case viewMemory:
		return directMemoryCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	case viewCPU:
		m.cpu.status = directLoading
		m.cpu.err = ""
		return directCPUCommand(m.session, request, frame.NodeID, frame.NodeTarget)
	default:
		m.time.status = directLoading
		m.time.err = ""
		return directTimeCommand(m.session, request, frame.NodeID, frame.NodeTarget)
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
	case viewNetstat:
		list, scope = viewNetstat, "sockets"
	case viewMounts:
		list, scope = viewMounts, "mounts"
	case viewMemory:
		list, scope = viewMemory, "memory"
	case viewCPU:
		list, scope = viewCPU, "cpu"
	case viewTime:
		list, scope = viewTime, "time"
	}
	m.views, _ = m.views.pop()
	if list != 0 && !m.directDiagnosticOpen(frame.NodeID, list, map[viewKind]viewKind{viewProcesses: viewProcessDetail, viewDisks: viewDiskDetail, viewNetwork: viewLinkDetail}[list]) {
		m.requests.cancelSlot(directDiagnosticSlot(frame.NodeID, scope))
		if frame.Kind == viewTime {
			m.requests.cancelSlot(directDiagnosticSlot(frame.NodeID, "time-check"))
			m.timePrompt = nil
		}
	}
}

func (m *directModel) cancelClosedDirectDiagnostics(previous viewStack) {
	seen := map[string]bool{}
	for _, frame := range previous {
		scope := map[viewKind]string{viewProcesses: "processes", viewProcessDetail: "processes", viewDisks: "disks", viewDiskDetail: "disks", viewNetwork: "network", viewLinkDetail: "network", viewNetstat: "sockets", viewMounts: "mounts", viewMemory: "memory", viewCPU: "cpu", viewTime: "time"}[frame.Kind]
		if scope == "" {
			continue
		}
		slot := directDiagnosticSlot(frame.NodeID, scope)
		if seen[slot] {
			continue
		}
		seen[slot] = true
		if !m.directDiagnosticOpen(frame.NodeID, map[string]viewKind{"processes": viewProcesses, "disks": viewDisks, "network": viewNetwork, "sockets": viewNetstat, "mounts": viewMounts, "memory": viewMemory, "cpu": viewCPU, "time": viewTime}[scope], map[string]viewKind{"processes": viewProcessDetail, "disks": viewDiskDetail, "network": viewLinkDetail}[scope]) {
			m.requests.cancelSlot(slot)
			if scope == "time" {
				m.requests.cancelSlot(directDiagnosticSlot(frame.NodeID, "time-check"))
				m.timePrompt = nil
			}
		}
	}
}
