package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
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

type directDiskUsageOpened struct {
	request      directRequest
	nodeID, node string
	path         string
	stream       ports.DiskUsageStream
	err          error
}

type directDiskUsageBatch struct {
	request      directRequest
	nodeID, node string
	path         string
	stream       ports.DiskUsageStream
	batch        domain.DiskUsageBatch
	err          error
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

type directContainersLoaded struct {
	request   directRequestStamp
	nodeID    string
	values    domain.ContainerSet
	namespace domain.ContainerNamespace
	err       error
}

type directImagesOpened struct {
	request   directRequest
	nodeID    string
	namespace domain.ContainerNamespace
	stream    ports.ImageStream
	err       error
}

type directImagesBatch struct {
	request   directRequest
	nodeID    string
	namespace domain.ContainerNamespace
	stream    ports.ImageStream
	batch     domain.ImageBatch
	err       error
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

func directDiskUsageOpenCommand(session *application.Session, request directRequest, nodeID, node, root string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		stream, err := session.OpenDiskUsage(ctx, domain.DiskUsageRequest{Node: node, Path: root})
		return directDiskUsageOpened{request: request, nodeID: nodeID, node: node, path: root, stream: stream, err: err}
	})
}

func directDiskUsageNextCommand(request directRequest, nodeID, node, root string, stream ports.DiskUsageStream) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		batch, err := stream.Next(ctx)
		return directDiskUsageBatch{request: request, nodeID: nodeID, node: node, path: root, stream: stream, batch: batch, err: err}
	})
}

func (m *model) applyDirectDiskUsageResult(message tea.Msg) tea.Cmd {
	switch result := message.(type) {
	case directDiskUsageOpened:
		if !m.requests.current(result.request.stamp) || result.request.stamp.generation != m.generation() || !m.directDiskUsageOpen(result.nodeID, result.path) {
			if result.stream != nil {
				_ = result.stream.Close()
			}
			return nil
		}
		if result.err != nil {
			m.diskUsage.accept(domain.DiskUsageBatch{}, result.err)
			m.requests.cancelSlot(directDiskUsageSlot(result.nodeID, result.path))
			return nil
		}
		if result.stream == nil {
			m.diskUsage.accept(domain.DiskUsageBatch{}, application.ErrCapabilityUnavailable)
			m.requests.cancelSlot(directDiskUsageSlot(result.nodeID, result.path))
			return nil
		}
		m.diskUsage.attach(result.stream)
		return directDiskUsageNextCommand(result.request, result.nodeID, result.node, result.path, result.stream)
	case directDiskUsageBatch:
		if !m.requests.current(result.request.stamp) || result.request.stamp.generation != m.generation() || !m.directDiskUsageOpen(result.nodeID, result.path) {
			if result.stream != nil {
				_ = result.stream.Close()
			}
			return nil
		}
		if m.diskUsage.accept(result.batch, result.err) {
			if result.stream != nil {
				_ = result.stream.Close()
			}
			m.diskUsage.stream = nil
			m.requests.cancelSlot(directDiskUsageSlot(result.nodeID, result.path))
			return nil
		}
		return directDiskUsageNextCommand(result.request, result.nodeID, result.node, result.path, result.stream)
	default:
		return nil
	}
}

func (m model) directDiskUsageOpen(nodeID, path string) bool {
	for _, frame := range m.views {
		if frame.Kind == viewDiskUsage && frame.NodeID == nodeID && frame.MountPath == path {
			return true
		}
	}
	return false
}

func directDiskUsageSlot(nodeID, root string) string {
	return directDiagnosticSlot(nodeID, "disk-usage:"+root)
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

func directContainersCommand(session *application.Session, request directRequest, nodeID, node string, namespace domain.ContainerNamespace) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Containers(ctx, domain.ContainerRequest{Node: node, Namespace: namespace})
		return directContainersLoaded{request: request.stamp, nodeID: nodeID, values: values, namespace: namespace, err: err}
	})
}

func directImagesOpenCommand(session *application.Session, request directRequest, nodeID, node string, namespace domain.ContainerNamespace) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		stream, err := session.OpenImages(ctx, domain.ImageRequest{Node: node, Namespace: namespace})
		return directImagesOpened{request: request, nodeID: nodeID, namespace: namespace, stream: stream, err: err}
	})
}

func directImagesNextCommand(request directRequest, nodeID string, namespace domain.ContainerNamespace, stream ports.ImageStream) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		batch, err := stream.Next(ctx)
		return directImagesBatch{request: request, nodeID: nodeID, namespace: namespace, stream: stream, batch: batch, err: err}
	})
}

func (m *model) applyDirectContainerResult(message tea.Msg) tea.Cmd {
	switch result := message.(type) {
	case directContainersLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directContainerOpen(result.nodeID, result.namespace, false) {
			return nil
		}
		m.requests.cancelSlot(directContainerSlot(result.nodeID))
		m.containers = m.containers.setContainers(result.values, result.err)
		return nil
	case directImagesOpened:
		if !m.requests.current(result.request.stamp) || result.request.stamp.generation != m.generation() || !m.directContainerOpen(result.nodeID, result.namespace, true) {
			if result.stream != nil {
				_ = result.stream.Close()
			}
			return nil
		}
		if result.err != nil || result.stream == nil {
			m.containers.status, m.containers.err = directFailed, "Image data is unavailable"
			if result.err != nil {
				m.containers.err = "Image data is unavailable: " + sanitizeUntrustedText(result.err.Error())
			}
			m.requests.cancelSlot(directContainerSlot(result.nodeID))
			return nil
		}
		m.containers.attach(result.stream)
		return directImagesNextCommand(result.request, result.nodeID, result.namespace, result.stream)
	case directImagesBatch:
		if !m.requests.current(result.request.stamp) || result.request.stamp.generation != m.generation() || !m.directContainerOpen(result.nodeID, result.namespace, true) {
			if result.stream != nil {
				_ = result.stream.Close()
			}
			return nil
		}
		if m.containers.acceptImages(result.batch, result.err) {
			if result.stream != nil {
				_ = result.stream.Close()
			}
			m.containers.stream = nil
			m.requests.cancelSlot(directContainerSlot(result.nodeID))
			return nil
		}
		return directImagesNextCommand(result.request, result.nodeID, result.namespace, result.stream)
	default:
		return nil
	}
}

func directContainerSlot(nodeID string) string { return directDiagnosticSlot(nodeID, "containers") }

func directContainerFrameMatches(frame viewFrame, nodeID string, namespace domain.ContainerNamespace, images bool) bool {
	return (frame.Kind == viewContainers || frame.Kind == viewContainerDetail) && frame.NodeID == nodeID && frame.ContainerNamespace == namespace && frame.ImageMode == images
}

func (m model) directContainerOpen(nodeID string, namespace domain.ContainerNamespace, images bool) bool {
	for _, frame := range m.views {
		if directContainerFrameMatches(frame, nodeID, namespace, images) {
			return true
		}
	}
	return false
}

func (m *model) applyDiagnosticResult(message tea.Msg) bool {
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
		m.time = m.time.setReadResult(result.node, result.value, result.err).clampOffset(m.timeViewportHeight())
	case directTimeCheckLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directDiagnosticOpen(result.nodeID, viewTime, 0) {
			return true
		}
		m.requests.cancelSlot(directDiagnosticSlot(result.nodeID, "time-check"))
		m.time = m.time.setCheckResult(result.node, result.server, result.value, result.err).clampOffset(m.timeViewportHeight())
	default:
		return false
	}
	return true
}

func (m *model) handleNodeDiagnosticKey(message tea.KeyPressMsg) (tea.Cmd, bool) {
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
	case "c":
		return m.openDirectContainers(node), true
	default:
		return nil, false
	}
}

func (m *model) handleDiagnosticKey(message tea.KeyPressMsg) (tea.Cmd, bool) {
	frame := m.views.top()
	switch frame.Kind {
	case viewDiskUsage:
		if !m.diskUsage.filtering && (message.String() == "esc" || message.String() == "q") {
			m.popDirectView()
			return nil, true
		}
		if message.String() == "r" && !m.diskUsage.filtering && m.session != nil {
			return m.refreshDirectDiskUsage(frame), true
		}
		m.diskUsage = m.diskUsage.update(message)
		return nil, true
	case viewContainers:
		if m.containers.filtering {
			m.containers = m.containers.update(message)
			return nil, true
		}
		switch message.String() {
		case "q", "esc":
			m.popDirectView()
			return nil, true
		case "n":
			namespace := nextContainerNamespace(m.containers.namespace)
			return m.switchDirectContainerScope(frame, namespace, m.containers.imageMode), true
		case "i":
			return m.switchDirectContainerScope(frame, m.containers.namespace, !m.containers.imageMode), true
		case "r":
			return m.refreshDirectContainers(frame), true
		case "enter", "d":
			if m.containers.imageMode {
				if image, ok := m.containers.selectedImage(); ok {
					label := firstNonEmpty(image.Reference, image.Digest)
					m.views = m.views.push(viewFrame{Kind: viewContainerDetail, Label: label, NodeID: frame.NodeID, NodeTarget: frame.NodeTarget, ContainerNamespace: frame.ContainerNamespace, ImageMode: true, ImageReference: image.Reference, ImageDigest: image.Digest, ImageSizeBytes: image.SizeBytes})
				}
			} else if container, ok := m.containers.selectedContainer(); ok {
				label := firstNonEmpty(container.Name, container.ID)
				m.views = m.views.push(viewFrame{Kind: viewContainerDetail, Label: label, NodeID: frame.NodeID, NodeTarget: frame.NodeTarget, ContainerNamespace: frame.ContainerNamespace, ContainerRuntimeNamespace: container.Namespace, ContainerID: container.ID})
			}
			return nil, true
		}
		m.containers = m.containers.update(message)
		return nil, true
	case viewContainerDetail:
		if message.String() == "q" || message.String() == "esc" {
			m.popDirectView()
		}
		return nil, true
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
		if frame.Kind == viewMounts && message.String() == "u" && !filtering {
			if mount, ok := m.mounts.selectedMount(); ok {
				return m.openDirectDiskUsage(frame, mount.MountedOn), true
			}
			return nil, true
		}
		if frame.Kind == viewDisks && message.String() == "W" && !filtering && !m.mutation.busy && m.mutation.prompt == nil && m.options.WritesEnabled && m.session != nil {
			if disk, ok := m.disks.selected(); ok && frame.NodeTarget != "" && m.disks.status != directLoading {
				request := application.NodeActionRequest{Kind: application.ActionWipeDevice, Targets: []string{frame.NodeTarget}, DeviceWipe: &ports.DeviceWipeOptions{Node: frame.NodeTarget, Device: disk.DeviceName, Method: ports.DeviceWipeFast}}
				return m.prepareDirectNodeMutation(request), true
			}
			return nil, true
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
			m.palette = m.palette.close()
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
			m.time = m.time.update(message).clampOffset(m.timeViewportHeight())
		}
		return nil, true
	default:
		return nil, false
	}
}

func (m model) directDiagnosticContent(size contentSize) (string, bool) {
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
	case viewDiskUsage:
		return m.diskUsage.viewSized(size), true
	case viewContainers:
		return m.containers.viewSized(size), true
	case viewContainerDetail:
		if frame.ImageMode {
			if value, ok := findImageByIdentity(m.containers.images, frame.ImageReference, frame.ImageDigest); ok {
				return renderImageDetail(value, size.Width), true
			}
			return renderDetail([]detailField{{label: "IMAGE", value: "unavailable"}}), true
		}
		if value, ok := findContainerByID(m.containers.values.Containers, frame.ContainerRuntimeNamespace, frame.ContainerID); ok {
			return renderContainerDetail(value), true
		}
		return renderDetail([]detailField{{label: "CONTAINER", value: "unavailable"}}), true
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
		return renderDetail([]detailField{{label: "PROCESS", value: "unavailable"}}), true
	case viewDiskDetail:
		if value, ok := findDiskByName(m.disks.values.Disks, frame.DeviceName); ok {
			return renderDiskDetail(value), true
		}
		return renderDetail([]detailField{{label: "DISK", value: "unavailable"}}), true
	case viewLinkDetail:
		if value, ok := findLinkByName(m.network.values.Links, frame.LinkName); ok {
			return renderLinkDetail(value), true
		}
		return renderDetail([]detailField{{label: "LINK", value: "unavailable"}}), true
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

func findContainerByID(values []domain.ContainerSnapshot, namespace string, id string) (domain.ContainerSnapshot, bool) {
	for _, value := range values {
		if value.Namespace == namespace && value.ID == id {
			return value, true
		}
	}
	return domain.ContainerSnapshot{}, false
}

func findImageByIdentity(values []domain.ImageSnapshot, reference, digest string) (domain.ImageSnapshot, bool) {
	for _, value := range values {
		if value.Reference == reference && value.Digest == digest {
			return value, true
		}
	}
	return domain.ImageSnapshot{}, false
}

func nextContainerNamespace(namespace domain.ContainerNamespace) domain.ContainerNamespace {
	switch namespace {
	case domain.ContainerNamespaceSystem:
		return domain.ContainerNamespaceCRI
	case domain.ContainerNamespaceCRI:
		return domain.ContainerNamespaceTalosContainers
	default:
		return domain.ContainerNamespaceSystem
	}
}

func (m *model) openDirectContainers(node domain.NodeSnapshot) tea.Cmd {
	if m.session == nil {
		return nil
	}
	nodeID := directDiagnosticNodeID(node)
	frame := viewFrame{Kind: viewContainers, Label: fallback(node.DisplayName()) + " > containers", NodeID: nodeID, NodeTarget: node.Target(), ContainerNamespace: domain.ContainerNamespaceSystem}
	m.views = m.views.push(frame)
	m.containers = newContainersModel(frame.ContainerNamespace, false)
	request, _ := m.requests.beginWithContext(directContainerSlot(nodeID), m.generation(), m.sessionCtx)
	return directContainersCommand(m.session, request, nodeID, node.Target(), frame.ContainerNamespace)
}

func (m *model) switchDirectContainerScope(frame viewFrame, namespace domain.ContainerNamespace, images bool) tea.Cmd {
	m.containers.close()
	frame.ContainerNamespace, frame.ImageMode = namespace, images
	frame.Label = frame.NodeTarget + " > " + string(namespace)
	m.views[len(m.views)-1] = frame
	m.containers = newContainersModel(namespace, images)
	request, _ := m.requests.beginWithContext(directContainerSlot(frame.NodeID), m.generation(), m.sessionCtx)
	if images {
		return directImagesOpenCommand(m.session, request, frame.NodeID, frame.NodeTarget, namespace)
	}
	return directContainersCommand(m.session, request, frame.NodeID, frame.NodeTarget, namespace)
}

func (m *model) refreshDirectContainers(frame viewFrame) tea.Cmd {
	m.containers.close()
	m.containers = newContainersModel(frame.ContainerNamespace, frame.ImageMode)
	request, _ := m.requests.beginWithContext(directContainerSlot(frame.NodeID), m.generation(), m.sessionCtx)
	if frame.ImageMode {
		return directImagesOpenCommand(m.session, request, frame.NodeID, frame.NodeTarget, frame.ContainerNamespace)
	}
	return directContainersCommand(m.session, request, frame.NodeID, frame.NodeTarget, frame.ContainerNamespace)
}

func directDiagnosticSlot(nodeID, scope string) string { return "node:" + nodeID + ":" + scope }

func (m model) directDiagnosticOpen(nodeID string, list, detail viewKind) bool {
	for _, frame := range m.views {
		if (frame.Kind == list || frame.Kind == detail) && frame.NodeID == nodeID {
			return true
		}
	}
	return false
}

func (m *model) openDirectDiagnostic(node domain.NodeSnapshot, kind viewKind) tea.Cmd {
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

func (m *model) openDirectDiskUsage(parent viewFrame, root string) tea.Cmd {
	if m.session == nil || root == "" {
		return nil
	}
	frame := viewFrame{Kind: viewDiskUsage, Label: "usage " + root, NodeID: parent.NodeID, NodeTarget: parent.NodeTarget, MountPath: root}
	m.views = m.views.push(frame)
	m.diskUsage = newDiskUsageModel(parent.NodeTarget, root)
	request, _ := m.requests.beginWithContext(directDiskUsageSlot(parent.NodeID, root), m.generation(), m.sessionCtx)
	return directDiskUsageOpenCommand(m.session, request, parent.NodeID, parent.NodeTarget, root)
}

func (m *model) refreshDirectDiskUsage(frame viewFrame) tea.Cmd {
	m.diskUsage.close()
	m.diskUsage = newDiskUsageModel(frame.NodeTarget, frame.MountPath)
	request, _ := m.requests.beginWithContext(directDiskUsageSlot(frame.NodeID, frame.MountPath), m.generation(), m.sessionCtx)
	return directDiskUsageOpenCommand(m.session, request, frame.NodeID, frame.NodeTarget, frame.MountPath)
}

func directDiagnosticNodeID(node domain.NodeSnapshot) string {
	if node.ID != "" {
		return node.ID
	}
	return node.Target()
}

func (m *model) refreshDirectDiagnostic(frame viewFrame) tea.Cmd {
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

func (m *model) popDirectView() {
	frame := m.views.top()
	if frame.Kind == viewServiceDetail {
		m.requests.cancelSlot(directServiceDetailSlot(frame.ServiceKey))
	}
	if frame.Kind == viewDiskUsage {
		m.diskUsage.close()
		m.requests.cancelSlot(directDiskUsageSlot(frame.NodeID, frame.MountPath))
	}
	if frame.Kind == viewContainers {
		m.containers.close()
		m.requests.cancelSlot(directContainerSlot(frame.NodeID))
	}
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
	case viewContainers, viewContainerDetail:
		list, scope = viewContainers, "containers"
	}
	m.views, _ = m.views.pop()
	if list != 0 && list != viewContainers && !m.directDiagnosticOpen(frame.NodeID, list, map[viewKind]viewKind{viewProcesses: viewProcessDetail, viewDisks: viewDiskDetail, viewNetwork: viewLinkDetail}[list]) {
		m.requests.cancelSlot(directDiagnosticSlot(frame.NodeID, scope))
		if frame.Kind == viewTime {
			m.requests.cancelSlot(directDiagnosticSlot(frame.NodeID, "time-check"))
			m.timePrompt = nil
		}
	}
}

func (m *model) cancelClosedDirectDiagnostics(previous viewStack) {
	for _, frame := range previous {
		switch frame.Kind {
		case viewServiceDetail:
			if !m.directServiceDetailOpen(frame.ServiceKey) {
				m.requests.cancelSlot(directServiceDetailSlot(frame.ServiceKey))
			}
		case viewDiskUsage:
			if !m.directDiskUsageOpen(frame.NodeID, frame.MountPath) {
				m.diskUsage.close()
				m.requests.cancelSlot(directDiskUsageSlot(frame.NodeID, frame.MountPath))
			}
		}
	}
	seen := map[string]bool{}
	for _, frame := range previous {
		scope := map[viewKind]string{viewProcesses: "processes", viewProcessDetail: "processes", viewDisks: "disks", viewDiskDetail: "disks", viewNetwork: "network", viewLinkDetail: "network", viewNetstat: "sockets", viewMounts: "mounts", viewMemory: "memory", viewCPU: "cpu", viewTime: "time", viewContainers: "containers", viewContainerDetail: "containers"}[frame.Kind]
		if scope == "" {
			continue
		}
		slot := directDiagnosticSlot(frame.NodeID, scope)
		if seen[slot] {
			continue
		}
		seen[slot] = true
		if !m.directDiagnosticOpen(frame.NodeID, map[string]viewKind{"processes": viewProcesses, "disks": viewDisks, "network": viewNetwork, "sockets": viewNetstat, "mounts": viewMounts, "memory": viewMemory, "cpu": viewCPU, "time": viewTime, "containers": viewContainers}[scope], map[string]viewKind{"processes": viewProcessDetail, "disks": viewDiskDetail, "network": viewLinkDetail, "containers": viewContainerDetail}[scope]) {
			if scope == "containers" {
				m.containers.close()
			}
			m.requests.cancelSlot(slot)
			if scope == "time" {
				m.requests.cancelSlot(directDiagnosticSlot(frame.NodeID, "time-check"))
				m.timePrompt = nil
			}
		}
	}
}
