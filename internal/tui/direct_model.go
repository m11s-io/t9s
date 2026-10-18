package tui

import (
	"context"
	"fmt"
	"image/color"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/version"
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

	cluster  directClusterState
	view     directViewState
	nodes    nodesModel
	contexts contextsModel
	palette  commandModel
	views    viewStack
	styles   styles
	width    int
	height   int
	splash   bool
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
		nodes:   newNodesModel(),
		palette: newCommandModel(),
		views:   newViewStack(viewFrame{Kind: viewNodes, Label: "nodes"}),
		styles:  defaultStyles(), width: 120, height: 40, splash: true,
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
		if message.err != nil {
			state.Status = directFailed
			state.Err = "Event data is unavailable"
			if len(message.events.Events) > 0 || len(message.events.Problems) > 0 {
				state.Status = directPartial
				state.Err = "Some event data is unavailable"
			}
		}
		m.cluster.events = state
		return m, nil
	case directEtcdLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		m.requests.cancelSlot("cluster-etcd")
		state := directEtcdState{Value: message.etcd, Status: directReady}
		if message.err != nil {
			state.Status = directFailed
			state.Err = "Etcd data is unavailable"
			if len(message.etcd.Members) > 0 {
				state.Status = directPartial
				state.Err = "Some etcd data is unavailable"
			}
		}
		m.cluster.etcd = state
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
	case directRunCommand:
		return m, m.runClusterCommand(message.value)
	case tea.KeyPressMsg:
		m.splash = false
		if message.String() == "ctrl+c" {
			return m, m.cleanup(tea.Quit())
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
		if m.views.top().Kind == viewNodeDetail {
			if message.String() == "esc" || message.String() == "q" {
				m.views, _ = m.views.pop()
			}
			return m, nil
		}
		if message.String() == ":" {
			var command tea.Cmd
			m.palette, command = m.palette.open()
			return m, command
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
			if message.String() == "space" && !m.options.WritesEnabled && !m.nodes.filtering {
				return m, nil
			}
			m.nodes = m.nodes.update(message, m.cluster.nodes.Value.Nodes)
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
	health := "Loading"
	switch m.cluster.nodes.Status {
	case application.Failed:
		health = "Unavailable"
	case application.Partial:
		health = "Degraded"
	case application.Ready:
		health = "Healthy"
	}
	metadata := shellMetadata{Context: m.cluster.contextName, Mode: "[RO]", Health: health, AppVersion: version.Version}
	if m.options.WritesEnabled {
		metadata.Mode = "[RW]"
	}
	for _, clusterContext := range m.cluster.contexts {
		if clusterContext.Name == m.cluster.contextName {
			metadata.Cluster = clusterContext.Cluster
			if len(clusterContext.Endpoints) > 0 {
				metadata.EndpointSummary = strconv.Itoa(len(clusterContext.Endpoints))
			}
			break
		}
	}
	content := ""
	if m.contexts.active {
		frame := layoutResourceFrame(layout.Width, layout.ContentHeight, "contexts")
		content = renderResourceFrame(frame, m.contexts.viewSized(contentSize{Width: frame.InnerWidth, Height: frame.InnerHeight}), m.styles.k9s)
	} else if m.splash {
		content = "t9s\nInspect Talos clusters"
	} else {
		if m.views.top().Kind == viewNodeDetail {
			content = renderNodeDetail(m.nodes.selectedValue(m.cluster.nodes.Value.Nodes))
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
	header := renderK9sHeader(layoutK9sHeader(layout.Width), metadata, actionHints(headerKind, m.options.WritesEnabled), m.styles.k9s)
	breadcrumb := m.views.breadcrumb(m.cluster.contextName)
	if m.contexts.active {
		breadcrumb = m.cluster.contextName + " > contexts"
	}
	footer := renderK9sFooter(layout.Width, breadcrumb, "", flash, m.styles.k9s)
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
