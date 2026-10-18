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
		cluster: directClusterState{contextName: options.ContextName, nodes: nodeState},
		nodes:   newNodesModel(nodeState),
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
			m.nodes = m.nodes.setState(m.cluster.nodes)
			m.view.notice = "Unable to discover contexts"
			return m, nil
		}
		selected, err := directResolveContext(message.contexts, m.options.ContextName)
		if err != nil {
			m.cluster.nodes = application.NodeState{Status: application.Failed, Err: "Selected context is unavailable"}
			m.nodes = m.nodes.setState(m.cluster.nodes)
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
			m.nodes = m.nodes.setState(m.cluster.nodes)
			m.view.notice = "Unable to open session"
			return m, nil
		}
		m.session = message.session
		m.cluster.contextName = message.contextName
		request, _ := m.requests.beginWithContext("cluster-nodes", message.request.generation, m.sessionCtx)
		return m, directNodeServicesCommand(message.session, request)
	case directNodeServicesLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		state := application.NodeState{Status: application.Ready, Value: message.nodes}
		if message.err != nil {
			state.Status = application.Failed
			state.Err = "Node data is unavailable"
			if len(message.nodes.Nodes) > 0 {
				state.Status = application.Partial
				state.Err = "Some node data is unavailable"
			}
		}
		m.cluster.nodes = state
		m.nodes = m.nodes.setState(state)
		if m.options.NodeFocus != "" {
			m.nodes.selectedID = m.options.NodeFocus
			m.nodes = m.nodes.normalizeSelection(m.nodes.control.Cursor())
		}
		return m, nil
	case tea.KeyPressMsg:
		m.splash = false
		if message.String() == "ctrl+c" {
			return m, m.cleanup(tea.Quit())
		}
		if message.String() == "esc" && m.contexts.active {
			m.contexts.active = false
		}
	}
	return m, nil
}

func (m directModel) View() tea.View {
	layout := layoutShell(m.width, m.height)
	headerKind := viewNodes
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
		content = m.nodes.viewSized(contentSize{Width: layout.Width, Height: layout.ContentHeight})
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
	m.nodes = m.nodes.setState(m.cluster.nodes)
	m.session = nil
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
