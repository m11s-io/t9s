package tui

import (
	"context"
	"fmt"
	"image/color"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type directModel struct {
	client               *application.Client
	options              LaunchOptions
	requests             *directRequests
	rootCtx              context.Context
	rootCancel           context.CancelFunc
	sessionCtx           context.Context
	sessionCancel        context.CancelFunc
	sessionRequestCancel context.CancelFunc
	session              *application.Session
	generationCounter    uint64

	cluster    directClusterState
	view       directViewState
	nodes      nodesModel
	services   servicesModel
	events     eventsModel
	etcd       etcdModel
	problems   problemsModel
	contexts   contextsModel
	palette    commandModel
	processes  processesModel
	disks      disksModel
	network    networkModel
	netstat    netstatModel
	mounts     mountsModel
	memory     memoryModel
	time       timeModel
	cpu        cpuModel
	timePrompt *timePromptModel
	views      viewStack
	styles     styles
	width      int
	height     int
	splash     bool
}

func newDirectModel(parent context.Context, client *application.Client, options LaunchOptions) directModel {
	if parent == nil {
		parent = context.Background()
	}
	rootCtx, rootCancel := context.WithCancel(parent)
	nodeState := application.NodeState{Status: application.Loading}
	return directModel{
		client: client, options: options, requests: newDirectRequests(),
		rootCtx: rootCtx, rootCancel: rootCancel,
		cluster: directClusterState{
			contextName: options.ContextName,
			nodes:       nodeState,
			services:    directServiceState{Status: directLoading},
			events:      directEventState{Status: directLoading},
			etcd:        directEtcdState{Status: directLoading},
			kubernetes:  directKubernetesState{Status: directLoading},
		},
		nodes:     newNodesModel(),
		services:  newServicesModel(),
		events:    newEventsModel(),
		etcd:      newEtcdModel(),
		problems:  newProblemsModel(),
		processes: newDirectProcessesModel(),
		disks:     newDirectDisksModel(),
		network:   newDirectNetworkModel(),
		netstat:   newDirectNetstatModel(),
		mounts:    newDirectMountsModel(),
		memory:    newDirectMemoryModel(),
		time:      newDirectTimeModel(),
		cpu:       newDirectCPUModel(),
		palette:   newCommandModel(),
		views:     newViewStack(viewFrame{Kind: viewNodes, Label: "nodes"}),
		styles:    defaultStyles(), width: 120, height: 40, splash: true,
	}
}

func (m directModel) Init() tea.Cmd {
	request, _ := m.requests.beginWithContext("contexts", 0, m.rootCtx)
	return directContextsCommand(m.client, request)
}

func (m directModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		return m, nil
	case directContextsLoaded:
		if !m.requests.current(message.request) {
			return m, nil
		}
		m.splash = false
		m.requests.cancelSlot("contexts")
		m.cluster.contexts = append([]domain.ClusterContext(nil), message.contexts...)
		if message.err != nil {
			m.cluster.nodes = application.NodeState{Status: application.Failed, Err: "Context discovery failed"}
			m.cluster.services = directServiceState{Status: directFailed, Err: "Service data is unavailable"}
			m.cluster.events = directEventState{Status: directFailed, Err: "Event data is unavailable"}
			m.cluster.etcd = directEtcdState{Status: directFailed, Err: "Etcd data is unavailable"}
			m.cluster.kubernetes = directKubernetesState{Status: directFailed, Err: "Kubernetes data is unavailable"}
			m.view.notice = "Unable to discover contexts"
			return m, nil
		}
		selected, err := directResolveContext(message.contexts, m.options.ContextName)
		if err != nil {
			m.cluster.nodes = application.NodeState{Status: application.Failed, Err: "Selected context is unavailable"}
			m.cluster.services = directServiceState{Status: directFailed, Err: "Service data is unavailable"}
			m.cluster.events = directEventState{Status: directFailed, Err: "Event data is unavailable"}
			m.cluster.etcd = directEtcdState{Status: directFailed, Err: "Etcd data is unavailable"}
			m.cluster.kubernetes = directKubernetesState{Status: directFailed, Err: "Kubernetes data is unavailable"}
			m.view.notice = "Selected context is unavailable"
			return m, nil
		}
		m.cluster.contextName = selected
		if m.options.OpenContextPicker {
			m.contexts = newContextsModel(message.contexts, selected)
			m.options.OpenContextPicker = false
		}
		command := m.beginSessionOpen(selected)
		return m, command
	case directSelectContext:
		if !directContextKnown(m.cluster.contexts, message.name) {
			return m, nil
		}
		command := m.beginSessionOpen(message.name)
		return m, command
	case directSessionOpened:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			if message.session != nil {
				_ = message.session.Close()
			}
			return m, nil
		}
		if message.err != nil {
			m.cluster.nodes = application.NodeState{Status: application.Failed, Err: "Session unavailable"}
			m.cluster.services = directServiceState{Status: directFailed, Err: "Service data is unavailable"}
			m.cluster.events = directEventState{Status: directFailed, Err: "Event data is unavailable"}
			m.cluster.etcd = directEtcdState{Status: directFailed, Err: "Etcd data is unavailable"}
			m.cluster.kubernetes = directKubernetesState{Status: directFailed, Err: "Kubernetes data is unavailable"}
			m.view.notice = "Unable to open session"
			return m, nil
		}
		m.session = message.session
		m.cluster.contextName = message.contextName
		return m, m.startClusterObservations(message.session, message.request.generation)
	case directNodeServicesLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		m.requests.cancelSlot("cluster-nodes-services")
		nodeStatus := directNodeLoadStatus(message.nodes)
		state := application.NodeState{Status: application.Ready, Value: message.nodes}
		if nodeStatus == directPartial {
			state.Status = application.Partial
			state.Err = "Some nodes report problems"
		}
		serviceStatus := directServiceLoadStatus(message.services)
		serviceState := directServiceState{Status: serviceStatus, Value: message.services}
		if serviceStatus == directPartial {
			serviceState.Err = "Some services report problems"
		}
		if message.err != nil {
			state.Status = application.Failed
			state.Err = "Node data is unavailable"
			if nodeStatus == directPartial || len(message.nodes.Nodes) > 0 || !message.nodes.ObservedAt.IsZero() {
				state.Status = application.Partial
				state.Err = "Some node data is unavailable"
			}
			serviceState.Status = directFailed
			serviceState.Err = "Service data is unavailable"
			if serviceStatus == directPartial || len(message.services.Services) > 0 || !message.services.ObservedAt.IsZero() {
				serviceState.Status = directPartial
				serviceState.Err = "Some service data is unavailable"
			}
		}
		state.Value = application.EnrichNodes(state.Value, m.cluster.kubernetes.Nodes)
		m.cluster.nodes = state
		m.cluster.services = serviceState
		m.services = m.services.normalizeSelection(serviceState.Value.Services, m.services.control.Cursor())
		previousCursor := m.nodes.control.Cursor()
		m.nodes = m.nodes.normalizeSelection(state.Value.Nodes, previousCursor)
		if m.options.NodeFocus != "" {
			if focusedID := resolveNodeFocus(state.Value.Nodes, m.options.NodeFocus); focusedID != "" {
				m.nodes = m.nodes.selectID(focusedID, state.Value.Nodes)
			}
			m.options.NodeFocus = ""
		}
		return m, nil
	case directEventsLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		m.requests.cancelSlot("cluster-events")
		state := directEventState{Value: message.events, Status: directReady}
		if len(message.events.Problems) > 0 {
			state.Status = directPartial
			state.Err = "Some event streams are unavailable"
		}
		if message.err != nil {
			state.Status = directFailed
			state.Err = "Event data is unavailable"
			if len(message.events.Events) > 0 || len(message.events.Problems) > 0 {
				state.Status = directPartial
				state.Err = "Some event data is unavailable"
			}
		}
		m.cluster.events = state
		m.events = m.events.normalizeSelection(eventRows(state.Value), m.events.control.Cursor())
		return m, nil
	case directEtcdLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		m.requests.cancelSlot("cluster-etcd")
		state := directEtcdState{Value: message.etcd, Status: directEtcdLoadStatus(message.etcd)}
		if state.Status == directPartial {
			state.Err = "Some etcd members are unavailable"
		}
		if message.err != nil {
			state.Status = directFailed
			state.Err = "Etcd data is unavailable"
			if len(message.etcd.Members) > 0 || state.Status == directPartial {
				state.Status = directPartial
				state.Err = "Some etcd data is unavailable"
			}
		}
		m.cluster.etcd = state
		m.etcd = m.etcd.normalizeSelection(state.Value.Members, m.etcd.control.Cursor())
		return m, nil
	case directKubernetesNodesLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		m.requests.cancelSlot("cluster-kubernetes")
		state := directKubernetesState{Status: directReady, Nodes: cloneKubernetesSnapshots(message.nodes)}
		if message.err != nil {
			state.Status = directFailed
			state.Err = "Kubernetes data is unavailable"
		}
		m.cluster.kubernetes = state
		m.cluster.nodes.Value = application.EnrichNodes(m.cluster.nodes.Value, state.Nodes)
		m.nodes = m.nodes.normalizeSelection(m.cluster.nodes.Value.Nodes, m.nodes.control.Cursor())
		return m, nil
	case directProcessesLoaded, directDisksLoaded, directNetworkLoaded, directSocketsLoaded, directMountsLoaded, directMemoryLoaded,
		directCPULoaded, directTimeLoaded, directTimeCheckLoaded:
		m.applyDiagnosticResult(message)
		return m, nil
	case directRunCommand:
		return m, m.runClusterCommand(message.value)
	case tea.KeyPressMsg:
		m.splash = false
		if message.String() == "ctrl+c" {
			return m, m.cleanup(tea.Quit())
		}
		if m.timePrompt != nil {
			switch message.String() {
			case "esc":
				m.timePrompt = nil
				return m, nil
			case "enter":
				server, err := application.ValidateTimeServer(m.timePrompt.input.Value())
				if err != nil {
					m.timePrompt.err = err.Error()
					return m, nil
				}
				m.timePrompt = nil
				frame := m.views.top()
				request, _ := m.requests.beginWithContext(directDiagnosticSlot(frame.NodeID, "time-check"), m.generation(), m.sessionCtx)
				m.time.checkStatus = directLoading
				m.time.check = domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: server}}
				m.time.checkErr = ""
				return m, directTimeCheckCommand(m.session, request, frame.NodeID, frame.NodeTarget, server)
			}
			var command tea.Cmd
			*m.timePrompt, command = m.timePrompt.update(message)
			return m, command
		}
		if m.contexts.active {
			switch message.String() {
			case "esc":
				m.contexts.active = false
			case "up", "k":
				m.contexts.control.MoveUp(1)
			case "down", "j":
				m.contexts.control.MoveDown(1)
			case "enter":
				if len(m.contexts.values) > 0 {
					selected := m.contexts.values[m.contexts.control.Cursor()].Name
					if selected != m.cluster.contextName {
						m.contexts.active = false
						return m, m.beginSessionOpen(selected)
					}
				}
				m.contexts.active = false
			}
			return m, nil
		}
		if m.palette.active {
			switch message.String() {
			case "esc":
				m.palette = m.palette.close()
				return m, nil
			case "enter":
				value := m.palette.input.Value()
				m.palette = m.palette.close()
				return m, func() tea.Msg { return directRunCommand{value: value} }
			}
			var command tea.Cmd
			m.palette, command = m.palette.update(message)
			return m, command
		}
		if message.String() == ":" && !m.nodes.filtering && !m.services.filtering && !m.events.filtering && !m.etcd.filtering && !m.problems.filtering && !m.processes.filtering && !m.disks.filtering && !m.network.filtering && !m.netstat.filtering && !m.mounts.filtering && !m.memory.filtering && !m.cpu.filtering && !m.time.filtering {
			var command tea.Cmd
			m.palette, command = m.palette.open()
			return m, command
		}
		if command, handled := m.handleDiagnosticKey(message); handled {
			return m, command
		}
		if m.views.top().Kind == viewNodeDetail || m.views.top().Kind == viewServiceDetail {
			if message.String() == "esc" || message.String() == "q" {
				m.popDirectView()
			}
			return m, nil
		}
		if m.views.top().Kind == viewNodes {
			if message.String() == "q" && !m.nodes.filtering {
				return m, m.cleanup(tea.Quit())
			}
			if (message.String() == "enter" || message.String() == "d") && !m.nodes.filtering {
				if node, ok := m.nodes.selected(m.cluster.nodes.Value.Nodes); ok {
					m.views = m.views.push(viewFrame{Kind: viewNodeDetail, Label: fallback(node.DisplayName())})
				}
				return m, nil
			}
			if message.String() == "r" && !m.nodes.filtering && m.session != nil {
				return m, m.refreshDirectNodes()
			}
			if command, handled := m.handleNodeDiagnosticKey(message); handled {
				return m, command
			}
			if message.String() == "space" && !m.options.WritesEnabled && !m.nodes.filtering {
				return m, nil
			}
			m.nodes = m.nodes.update(message, m.cluster.nodes.Value.Nodes)
		} else if m.views.top().Kind == viewServices {
			if message.String() == "q" && !m.services.filtering {
				return m, m.cleanup(tea.Quit())
			}
			if (message.String() == "enter" || message.String() == "d") && !m.services.filtering {
				if service, ok := m.services.selected(m.cluster.services.Value.Services); ok {
					label := fallback(service.Name) + "@" + fallback(service.Node)
					m.views = m.views.push(viewFrame{Kind: viewServiceDetail, Label: label, ServiceKey: serviceKey(service)})
				}
				return m, nil
			}
			if message.String() == "r" && !m.services.filtering && m.session != nil {
				return m, m.refreshDirectNodes()
			}
			m.services = m.services.update(message, m.cluster.services.Value.Services)
		} else if m.views.top().Kind == viewEvents {
			if message.String() == "q" && !m.events.filtering {
				return m, m.cleanup(tea.Quit())
			}
			if message.String() == "r" && !m.events.filtering && m.session != nil {
				request, _ := m.requests.beginWithContext("cluster-events", m.generation(), m.sessionCtx)
				return m, directEventsCommand(m.session, request)
			}
			m.events = m.events.update(message, eventRows(m.cluster.events.Value))
		} else if m.views.top().Kind == viewEtcd {
			if message.String() == "q" && !m.etcd.filtering {
				return m, m.cleanup(tea.Quit())
			}
			if message.String() == "r" && !m.etcd.filtering && m.session != nil {
				request, _ := m.requests.beginWithContext("cluster-etcd", m.generation(), m.sessionCtx)
				return m, directEtcdCommand(m.session, request)
			}
			m.etcd = m.etcd.update(message, m.cluster.etcd.Value.Members)
		} else if m.views.top().Kind == viewProblems {
			diagnoses := application.EvaluateObservations(directHealthInput(m.cluster))
			m.problems = m.problems.normalizeSelection(m.problems.control.Cursor(), diagnoses)
			if (message.String() == "enter" || message.String() == "d") && !m.problems.filtering {
				if diagnosis, ok := m.problems.selected(diagnoses); ok {
					switch diagnosis.ResourceKind {
					case "node":
						m.nodes.filter = ""
						m.nodes.filtering = false
						m.nodes.selectedID = diagnosis.ResourceID
						m.nodes = m.nodes.selectID(diagnosis.ResourceID, m.cluster.nodes.Value.Nodes)
						m.views = m.views.push(viewFrame{Kind: viewNodeDetail, Label: fallback(diagnosis.ResourceName)})
					case "etcd-member":
						m.etcd.filter = ""
						m.etcd.filtering = false
						for index, member := range m.cluster.etcd.Value.Members {
							if fmt.Sprint(member.MemberID) == diagnosis.ResourceID {
								m.etcd.selectedKey = etcdMemberKey(member)
								m.etcd = m.etcd.normalizeSelection(m.cluster.etcd.Value.Members, index)
								break
							}
						}
						m.views = m.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
					}
				}
				return m, nil
			}
			if message.String() == "q" && !m.problems.filtering {
				return m, m.cleanup(tea.Quit())
			}
			m.problems = m.problems.update(message, diagnoses)
		} else if m.views.top().Kind == viewOverview && message.String() == "q" {
			return m, m.cleanup(tea.Quit())
		}
	}
	return m, nil
}

func (m *directModel) startClusterObservations(session *application.Session, generation uint64) tea.Cmd {
	nodeRequest, _ := m.requests.beginWithContext("cluster-nodes-services", generation, m.sessionCtx)
	eventsRequest, _ := m.requests.beginWithContext("cluster-events", generation, m.sessionCtx)
	etcdRequest, _ := m.requests.beginWithContext("cluster-etcd", generation, m.sessionCtx)
	kubeRequest, _ := m.requests.beginWithContext("cluster-kubernetes", generation, m.sessionCtx)
	return tea.Batch(
		directNodeServicesCommand(session, nodeRequest),
		directEventsCommand(session, eventsRequest),
		directEtcdCommand(session, etcdRequest),
		directKubernetesNodesCommand(session, kubeRequest),
	)
}

func cloneKubernetesSnapshots(values map[string]domain.KubernetesNodeSnapshot) map[string]domain.KubernetesNodeSnapshot {
	if values == nil {
		return nil
	}
	cloned := make(map[string]domain.KubernetesNodeSnapshot, len(values))
	for key, value := range values {
		value.Roles = append([]string(nil), value.Roles...)
		value.Conditions = append([]domain.KubernetesCondition(nil), value.Conditions...)
		cloned[key] = value
	}
	return cloned
}

func resolveNodeFocus(nodes []domain.NodeSnapshot, focus string) string {
	for _, node := range nodes {
		if node.ID == focus || node.Name == focus {
			return node.ID
		}
		for _, address := range node.Addresses {
			if address == focus {
				return node.ID
			}
		}
	}
	return ""
}

func (m *directModel) refreshDirectNodes() tea.Cmd {
	if m.session == nil {
		return nil
	}
	nodeRequest, _ := m.requests.beginWithContext("cluster-nodes-services", m.generation(), m.sessionCtx)
	kubeRequest, _ := m.requests.beginWithContext("cluster-kubernetes", m.generation(), m.sessionCtx)
	return tea.Batch(
		directNodeServicesCommand(m.session, nodeRequest),
		directKubernetesNodesCommand(m.session, kubeRequest),
	)
}

func (m directModel) View() tea.View {
	layout := layoutShell(m.width, m.height)
	headerKind := m.views.top().Kind
	if m.contexts.active {
		headerKind = viewContexts
	}
	metadata := deriveShellMetadata(m.cluster.contextName, m.cluster.contexts, m.options.WritesEnabled, m.cluster.nodes.Status, directHealthInput(m.cluster))
	content := ""
	if m.contexts.active {
		frame := layoutResourceFrame(layout.Width, layout.ContentHeight, "contexts")
		content = renderResourceFrame(frame, m.contexts.viewSized(contentSize{Width: frame.InnerWidth, Height: frame.InnerHeight}), m.styles.k9s)
	} else if m.splash {
		content = "t9s\nInspect Talos clusters"
	} else {
		if diagnosticContent, handled := m.directDiagnosticContent(contentSize{Width: layout.Width, Height: layout.ContentHeight}); handled {
			content = diagnosticContent
		} else if m.views.top().Kind == viewNodeDetail {
			content = renderNodeDetail(m.nodes.selectedValue(m.cluster.nodes.Value.Nodes))
		} else if m.views.top().Kind == viewServiceDetail {
			if service, ok := findServiceByKey(m.cluster.services.Value.Services, m.views.top().ServiceKey); ok {
				content = renderServiceDetail(service)
			} else {
				content = "SERVICE DETAIL\nService unavailable"
			}
		} else if m.views.top().Kind == viewServices {
			content = m.services.viewSized(contentSize{Width: layout.Width, Height: layout.ContentHeight}, m.cluster.services.Value,
				m.cluster.services.Status == directLoading, m.cluster.services.Status == directFailed, m.cluster.services.Err)
		} else if m.views.top().Kind == viewEvents {
			content = m.events.viewSized(contentSize{Width: layout.Width, Height: layout.ContentHeight}, m.cluster.events)
		} else if m.views.top().Kind == viewEtcd {
			content = m.etcd.viewSized(contentSize{Width: layout.Width, Height: layout.ContentHeight}, m.cluster.etcd)
		} else if m.views.top().Kind == viewOverview {
			content = withHealthNotices(renderOverview(directHealthInput(m.cluster)), directHealthNotices(m.cluster))
		} else if m.views.top().Kind == viewProblems {
			notices := directHealthNotices(m.cluster)
			content = withHealthNotices(m.problems.viewSized(contentSize{Width: layout.Width, Height: max(0, layout.ContentHeight-len(notices))}, application.EvaluateObservations(directHealthInput(m.cluster))), notices)
		} else {
			content = m.nodes.viewSized(contentSize{Width: layout.Width, Height: layout.ContentHeight}, m.cluster.nodes)
		}
	}
	if m.palette.active {
		content += "\n" + m.palette.view()
	}
	flash := m.view.notice
	if flash == "" {
		switch m.cluster.nodes.Status {
		case application.Loading, application.Idle:
			flash = "Loading nodes…"
		case application.Failed:
			flash = m.cluster.nodes.Err
		case application.Ready, application.Partial:
			if len(m.cluster.nodes.Value.Nodes) == 0 {
				flash = "No nodes"
			}
		}
	}
	header := renderK9sHeader(layoutK9sHeader(layout.Width), metadata, directActionHints(headerKind, m.options.WritesEnabled), m.styles.k9s)
	breadcrumb := m.views.breadcrumb(m.cluster.contextName)
	if m.contexts.active {
		breadcrumb = m.cluster.contextName + " > contexts"
	}
	prompt := ""
	if m.timePrompt != nil {
		prompt = m.timePrompt.view()
	} else if !m.palette.active && m.views.top().Kind == viewNodes && (m.nodes.filtering || m.nodes.filter != "") {
		prompt = "/" + m.nodes.filter
	} else if !m.palette.active && m.views.top().Kind == viewServices && (m.services.filtering || m.services.filter != "") {
		prompt = "/" + m.services.filter
	} else if !m.palette.active && m.views.top().Kind == viewEvents && (m.events.filtering || m.events.filter != "") {
		prompt = "/" + m.events.filter
	} else if !m.palette.active && m.views.top().Kind == viewEtcd && (m.etcd.filtering || m.etcd.filter != "") {
		prompt = "/" + m.etcd.filter
	} else if !m.palette.active && m.views.top().Kind == viewProblems && (m.problems.filtering || m.problems.filter != "") {
		prompt = "/" + m.problems.filter
	} else if !m.palette.active && m.views.top().Kind == viewProcesses && (m.processes.filtering || m.processes.filter != "") {
		prompt = "/" + m.processes.filter
	} else if !m.palette.active && m.views.top().Kind == viewDisks && (m.disks.filtering || m.disks.filter != "") {
		prompt = "/" + m.disks.filter
	} else if !m.palette.active && m.views.top().Kind == viewNetwork && (m.network.filtering || m.network.filter != "") {
		prompt = "/" + m.network.filter
	} else if !m.palette.active && m.views.top().Kind == viewNetstat && (m.netstat.filtering || m.netstat.filter != "") {
		prompt = "/" + m.netstat.filter
	} else if !m.palette.active && m.views.top().Kind == viewMounts && (m.mounts.filtering || m.mounts.filter != "") {
		prompt = "/" + m.mounts.filter
	} else if !m.palette.active && m.views.top().Kind == viewMemory && (m.memory.filtering || m.memory.filter != "") {
		prompt = "/" + m.memory.filter
	} else if !m.palette.active && m.views.top().Kind == viewCPU && m.cpu.filteringActive() {
		prompt = "/" + m.cpu.filter
	} else if !m.palette.active && m.views.top().Kind == viewTime && m.time.filteringActive() {
		prompt = "/" + m.time.filter
	}
	footer := renderK9sFooter(layout.Width, breadcrumb, prompt, flash, m.styles.k9s)
	result := tea.NewView(renderShell(layout, header, content, footer))
	result.AltScreen = true
	result.BackgroundColor = color.Black
	result.WindowTitle = "t9s"
	return result
}

func (m *directModel) beginSessionOpen(contextName string) tea.Cmd {
	if m.sessionCancel != nil {
		m.sessionCancel()
	}
	m.requests.cancelSlot("session-open")
	m.generationCounter++
	m.cluster.contextName = contextName
	m.cluster.nodes = application.NodeState{Status: application.Loading}
	m.cluster.services = directServiceState{Status: directLoading}
	m.cluster.events = directEventState{Status: directLoading}
	m.cluster.etcd = directEtcdState{Status: directLoading}
	m.cluster.kubernetes = directKubernetesState{Status: directLoading}
	m.nodes = m.nodes.normalizeSelection(m.cluster.nodes.Value.Nodes, m.nodes.control.Cursor())
	m.nodes.selectedID = ""
	m.nodes.marked = nil
	m.nodes.filter = ""
	m.nodes.filtering = false
	m.services.filter = ""
	m.services.filtering = false
	m.services.selectedKey = ""
	m.services.control.SetCursor(0)
	m.events.filter = ""
	m.events.filtering = false
	m.events.selectedKey = ""
	m.events.control.SetCursor(0)
	m.etcd.filter = ""
	m.etcd.filtering = false
	m.etcd.selectedKey = ""
	m.etcd.control.SetCursor(0)
	m.problems.filter = ""
	m.problems.filtering = false
	m.problems.selectedKey = ""
	m.problems.control.SetCount(0)
	m.session = nil
	m.view.notice = ""
	m.palette = m.palette.close()
	m.views = m.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
	ctx, cancel := context.WithCancel(m.rootCtx)
	m.sessionCancel = cancel
	m.sessionCtx = ctx
	request, requestCancel := m.requests.beginWithContext("session-open", m.generationCounter, ctx)
	// Retain requestCancel until a context switch or root teardown. Open's
	// context becomes the persistent lifetime of the returned Session.
	m.sessionRequestCancel = requestCancel
	return directOpenSessionCommand(m.client, request, contextName, application.Options{WritesEnabled: m.options.WritesEnabled})
}

func directActionHints(kind viewKind, writesEnabled bool) []actionHint {
	if kind == viewEtcd {
		return actionHints(kind, false)
	}
	hints := actionHints(kind, writesEnabled)
	if kind != viewEvents {
		return hints
	}
	filtered := hints[:0]
	for _, hint := range hints {
		if hint.Key != "s" || hint.Label != "Follow" {
			filtered = append(filtered, hint)
		}
	}
	return filtered
}

func (m directModel) fallbackCleanup() {
	if m.rootCancel != nil {
		m.rootCancel()
	}
	if m.sessionRequestCancel != nil {
		m.sessionRequestCancel()
	} else if m.sessionCancel != nil {
		m.sessionCancel()
	}
	m.requests.cancelAll()
}

func (m directModel) cleanup(next tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m.fallbackCleanup()
		return next
	}
}

func (m directModel) generation() uint64 { return m.generationCounter }

func directResolveContext(contexts []domain.ClusterContext, requested string) (string, error) {
	if requested != "" {
		for _, clusterContext := range contexts {
			if clusterContext.Name == requested {
				return requested, nil
			}
		}
		return "", fmt.Errorf("Talos context %q is not available", requested)
	}
	for _, clusterContext := range contexts {
		if clusterContext.Current {
			return clusterContext.Name, nil
		}
	}
	return "", fmt.Errorf("Talos context catalog has no current context")
}
