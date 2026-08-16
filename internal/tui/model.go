package tui

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type model struct {
	application       application.Model
	runner            *application.Runner
	initial           application.Effect
	lifecycle         *lifecycle
	watchCtx          bool
	width             int
	height            int
	styles            styles
	nodes             nodesModel
	services          servicesModel
	events            eventsModel
	etcd              etcdModel
	processes         processesModel
	disks             disksModel
	network           networkModel
	problems          problemsModel
	resourceKinds     resourceKindsModel
	resourceInstances resourceInstancesModel
	resourceDetail    resourceDetailModel
	logs              logsModel
	palette           commandModel
	contexts          contextsModel
	notice            string
	views             viewStack
	splash            bool

	pendingContextPicker bool
	nodeFocusResolved    bool
}

type applicationMessage struct {
	message application.Message
}

type shutdownMessage struct{}

type lifecycle struct {
	effectCtx context.Context
	cancel    context.CancelFunc
	runner    *application.Runner

	once sync.Once
}

// New creates the root terminal model without running any application effect.
func New(applicationModel application.Model, runner *application.Runner) tea.Model {
	return newModel(context.Background(), false, applicationModel, runner)
}

// NewWithContext creates a root terminal model whose application effects stop
// when ctx is canceled.
func NewWithContext(ctx context.Context, applicationModel application.Model, runner *application.Runner) tea.Model {
	return newModel(ctx, true, applicationModel, runner)
}

// NewWithCleanup creates a context-bound model and an idempotent fallback
// command for callers to execute if Bubble Tea exits without a shutdown message.
func NewWithCleanup(ctx context.Context, applicationModel application.Model, runner *application.Runner) (tea.Model, tea.Cmd) {
	model := newModel(ctx, true, applicationModel, runner)

	return model, model.lifecycle.cleanup(nil)
}

func newModel(parent context.Context, watchCtx bool, applicationModel application.Model, runner *application.Runner) model {
	applicationModel, initial := application.Update(applicationModel, application.Start{})
	if parent == nil {
		parent = context.Background()
	}
	effectCtx, cancel := context.WithCancel(parent)

	result := model{
		application: applicationModel,
		runner:      runner,
		initial:     initial,
		lifecycle: &lifecycle{
			effectCtx: effectCtx,
			cancel:    cancel,
			runner:    runner,
		},
		watchCtx:          watchCtx,
		styles:            defaultStyles(),
		nodes:             newNodesModel(applicationModel.Nodes),
		services:          newServicesModel(applicationModel.Services),
		events:            newEventsModel(applicationModel.Events),
		etcd:              newEtcdModel(applicationModel.Etcd),
		processes:         newProcessesModel(applicationModel.Processes),
		disks:             newDisksModel(applicationModel.Disks),
		network:           newNetworkModel(applicationModel.Network),
		problems:          newProblemsModel(application.EvaluateHealth(applicationModel)),
		resourceKinds:     newResourceKindsModel(applicationModel.ResourceBrowser),
		resourceInstances: newResourceInstancesModel(applicationModel.ResourceBrowser),
		resourceDetail:    newResourceDetailModel(),
		logs:              newLogsModel(applicationModel.Logs),
		palette:           newCommandModel(),
		views:             newViewStack(viewFrame{Kind: viewNodes, Label: "nodes"}),
		width:             120,
		height:            40,
		splash:            true,
	}
	if applicationModel.OpenContextPicker {
		result.pendingContextPicker = true
		result.notice = "select the Talos context for Kubernetes context association"
	}
	return result
}

func (m model) Init() tea.Cmd {
	if m.watchCtx {
		return tea.Batch(m.command(m.initial), splashTimer(), m.watchContext())
	}

	return tea.Batch(m.command(m.initial), splashTimer())
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		return m, nil
	case tea.KeyPressMsg:
		// Like k9s, any deliberate input skips the transient splash and is still
		// handled by the active view.
		m.splash = false
		if message.Keystroke() == "ctrl+c" {
			return m, m.shutdown()
		}
		if m.application.PendingAction != nil {
			if message.Keystroke() == "y" {
				pending := *m.application.PendingAction
				effects := application.BuildActionEffects(m.application, pending)
				var confirmEffect application.Effect
				m.application, confirmEffect = application.Update(m.application, application.ConfirmPendingAction{})
				cmds := make([]tea.Cmd, 0, len(effects)+1)
				if cmd := m.command(confirmEffect); cmd != nil {
					cmds = append(cmds, cmd)
				}
				for _, effect := range effects {
					cmds = append(cmds, m.command(effect))
				}
				return m, tea.Batch(cmds...)
			}
			var effect application.Effect
			m.application, effect = application.Update(m.application, application.CancelPendingAction{})
			return m, m.command(effect)
		}
		if message.Keystroke() == "esc" && !m.contexts.active && !m.palette.active && !m.filtering() {
			wasLogs := m.views.top().Kind == viewServiceLogs
			if views, ok := m.views.pop(); ok {
				m.views = views
				if wasLogs {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.CloseServiceLogs{})
					m.logs = m.logs.setState(m.application.Logs)
					return m, m.command(effect)
				}
			}
			return m, nil
		}
		if m.contexts.active {
			var command tea.Cmd
			m.contexts, command = m.contexts.update(message, m.application.ContextName)
			return m, command
		}
		if m.palette.active {
			switch message.Keystroke() {
			case "esc":
				m.palette = m.palette.close()
				return m, nil
			case "enter":
				value := m.palette.input.Value()
				m.palette = m.palette.close()
				if value == "" {
					return m, nil
				}
				switch resolveCommand(value) {
				case commandNodes:
					m.views = m.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
					return m, nil
				case commandServices:
					m.views = m.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RefreshServices{})
					m.services = m.services.setState(m.application.Services)
					return m, m.command(effect)
				case commandContexts:
					m.contexts = newContextsModel(m.application.Contexts, m.application.ContextName)
					return m, nil
				case commandEvents:
					m.views = m.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RefreshEvents{})
					m.events = m.events.setState(m.application.Events)
					return m, m.command(effect)
				case commandEtcd:
					m.views = m.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RefreshEtcd{})
					m.etcd = m.etcd.setState(m.application.Etcd)
					return m, m.command(effect)
				case commandOverview:
					m.views = m.views.replaceRoot(viewFrame{Kind: viewOverview, Label: "overview"})
					return m, nil
				case commandProblems:
					m.views = m.views.replaceRoot(viewFrame{Kind: viewProblems, Label: "problems"})
					m.problems = m.problems.setDiagnoses(application.EvaluateHealth(m.application))
					return m, nil
				case commandResources:
					m.views = m.views.replaceRoot(viewFrame{Kind: viewResourceKinds, Label: "resources"})
					argument, hasArgument := resourcesCommandArgument(value)
					var kindsEffect application.Effect
					m.application, kindsEffect = application.Update(m.application, application.OpenResourceBrowser{Kind: argument})
					m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
					if !hasArgument {
						return m, m.command(kindsEffect)
					}
					m.views = m.views.push(viewFrame{Kind: viewResourceInstances, Label: argument})
					var instancesEffect application.Effect
					m.application, instancesEffect = application.Update(m.application, application.SelectResourceKind{Kind: argument})
					m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
					return m, tea.Batch(m.command(kindsEffect), m.command(instancesEffect))
				case commandUnknown:
					m.notice = unknownCommandNotice(value)
					return m, nil
				}
			}
			var command tea.Cmd
			m.palette, command = m.palette.update(message)
			return m, command
		}
		switch message.Keystroke() {
		case ":":
			if !m.filtering() {
				m.notice = ""
				m.palette, _ = m.palette.open()
				return m, nil
			}
		case "q":
			if !m.filtering() {
				wasLogs := m.views.top().Kind == viewServiceLogs
				if views, ok := m.views.pop(); ok {
					m.views = views
					if wasLogs {
						var effect application.Effect
						m.application, effect = application.Update(m.application, application.CloseServiceLogs{})
						m.logs = m.logs.setState(m.application.Logs)
						return m, m.command(effect)
					}
					return m, nil
				}
				return m, m.shutdown()
			}
		case "?":
			if !m.filtering() {
				m.views = m.views.push(viewFrame{Kind: viewHelp, Label: "help"})
				return m, nil
			}
		case "r":
			if !m.filtering() && m.views.top().Kind == viewServiceLogs {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.ReconnectServiceLogs{})
				m.logs = m.logs.setState(m.application.Logs)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewServices {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshServices{})
				m.services = m.services.setState(m.application.Services)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewNodes {
				var nodesEffect, kubernetesEffect application.Effect
				m.application, nodesEffect = application.Update(m.application, application.RefreshNodes{})
				m.application, kubernetesEffect = application.Update(m.application, application.RefreshKubernetesNodes{})
				m.nodes = m.nodes.setState(m.application.Nodes)
				return m, tea.Batch(m.command(nodesEffect), m.command(kubernetesEffect))
			}
			if !m.filtering() && m.views.top().Kind == viewEvents {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshEvents{})
				m.events = m.events.setState(m.application.Events)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewEtcd {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshEtcd{})
				m.etcd = m.etcd.setState(m.application.Etcd)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewProcesses {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshProcesses{})
				m.processes = m.processes.setState(m.application.Processes)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewDisks {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshDisks{})
				m.disks = m.disks.setState(m.application.Disks)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewNetwork {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshNetwork{})
				m.network = m.network.setState(m.application.Network)
				return m, m.command(effect)
			}
			if !m.filtering() && (m.views.top().Kind == viewOverview || m.views.top().Kind == viewProblems) {
				var servicesEffect, etcdEffect application.Effect
				m.application, servicesEffect = application.Update(m.application, application.RefreshServices{})
				m.application, etcdEffect = application.Update(m.application, application.RefreshEtcd{})
				m.services = m.services.setState(m.application.Services)
				m.etcd = m.etcd.setState(m.application.Etcd)
				return m, tea.Batch(m.command(servicesEffect), m.command(etcdEffect))
			}
		}
		if m.views.top().Kind == viewServiceLogs {
			m.logs = m.logs.update(message)
			if m.logs.clearRequested {
				m.logs.clearRequested = false
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.ClearServiceLogs{})
				m.logs = m.logs.setState(m.application.Logs)
				return m, m.command(effect)
			}
			return m, nil
		}
		if m.views.top().Kind == viewServices {
			if message.Keystroke() == "l" && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					label := fallback(service.Name) + "@" + fallback(service.Node)
					m.views = m.views.push(viewFrame{Kind: viewServiceLogs, Label: label + " > logs"})
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.OpenServiceLogs{Request: domain.LogRequest{Node: service.Node, Service: service.Name}})
					m.logs = newLogsModel(m.application.Logs)
					return m, m.command(effect)
				}
			}
			if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.services.filtering {
				if _, ok := m.services.selected(); ok {
					service := m.services.selectedValue()
					m.views = m.views.push(viewFrame{Kind: viewServiceDetail, Label: fallback(service.Name) + "@" + fallback(service.Node)})
					return m, nil
				}
			}
			m.services = m.services.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewEvents {
			m.events = m.events.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewEtcd {
			m.etcd = m.etcd.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewProcesses {
			if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.processes.filtering {
				if _, ok := m.processes.selected(); ok {
					process := m.processes.selectedValue()
					m.views = m.views.push(viewFrame{Kind: viewProcessDetail, Label: fmt.Sprintf("pid %d", process.PID)})
					return m, nil
				}
			}
			m.processes = m.processes.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewDisks {
			if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.disks.filtering {
				if _, ok := m.disks.selected(); ok {
					disk := m.disks.selectedValue()
					m.views = m.views.push(viewFrame{Kind: viewDiskDetail, Label: fmt.Sprintf("disk %s", disk.DeviceName)})
					return m, nil
				}
			}
			m.disks = m.disks.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewNetwork {
			if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.network.filtering {
				if _, ok := m.network.selected(); ok {
					link := m.network.selectedValue()
					m.views = m.views.push(viewFrame{Kind: viewLinkDetail, Label: fmt.Sprintf("link %s", link.Name)})
					return m, nil
				}
			}
			m.network = m.network.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewProblems {
			if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.problems.filtering {
				if diagnosis, ok := m.problems.selected(); ok {
					switch diagnosis.ResourceKind {
					case "node":
						for index, node := range m.nodes.visibleNodes() {
							if node.ID == diagnosis.ResourceID {
								m.nodes.selectedID = node.ID
								m.nodes = m.nodes.normalizeSelection(index)
								break
							}
						}
						m.views = m.views.push(viewFrame{Kind: viewNodeDetail, Label: fallback(m.nodes.selectedValue().DisplayName())})
					case "etcd-member":
						m.views = m.views.push(viewFrame{Kind: viewEtcd, Label: "etcd"})
					}
					return m, nil
				}
			}
			m.problems = m.problems.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewResourceKinds {
			if !m.filtering() && message.Keystroke() == "r" {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenResourceBrowser{})
				m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
				return m, m.command(effect)
			}
			if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.resourceKinds.filtering {
				if kind, ok := m.resourceKinds.selected(); ok {
					m.views = m.views.push(viewFrame{Kind: viewResourceInstances, Label: kind.DisplayType})
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.SelectResourceKind{Kind: kind.Type})
					m.resourceInstances = newResourceInstancesModel(m.application.ResourceBrowser)
					return m, m.command(effect)
				}
			}
			m.resourceKinds = m.resourceKinds.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewResourceInstances {
			if !m.filtering() && message.Keystroke() == "r" {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.SelectResourceKind{Kind: m.application.ResourceBrowser.SelectedKind, Node: m.application.ResourceBrowser.SelectedNode})
				m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
				return m, m.command(effect)
			}
			if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.resourceInstances.filtering {
				if instance, ok := m.resourceInstances.selected(); ok {
					m.views = m.views.push(viewFrame{Kind: viewResourceDetail, Label: instance.ID})
					m.resourceDetail = newResourceDetailModel()
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.OpenResourceInstance{ID: instance.ID})
					return m, m.command(effect)
				}
			}
			m.resourceInstances = m.resourceInstances.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewResourceDetail {
			if message.Keystroke() == "r" {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenResourceInstance{ID: m.application.ResourceBrowser.Detail.ID})
				return m, m.command(effect)
			}
			m.resourceDetail = m.resourceDetail.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewServiceDetail || m.views.top().Kind == viewHelp || m.views.top().Kind == viewProcessDetail || m.views.top().Kind == viewDiskDetail || m.views.top().Kind == viewLinkDetail || m.views.top().Kind == viewOverview {
			return m, nil
		}
		if (message.Keystroke() == "enter" || message.Keystroke() == "d") && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				m.views = m.views.push(viewFrame{Kind: viewNodeDetail, Label: fallback(m.nodes.selectedValue().DisplayName())})
				return m, nil
			}
		}
		if message.Keystroke() == "p" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewProcesses, Label: fallback(node.DisplayName()) + " > processes"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenProcesses{Node: node.Target()})
				m.processes = newProcessesModel(m.application.Processes)
				return m, m.command(effect)
			}
		}
		if message.Keystroke() == "k" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewDisks, Label: fallback(node.DisplayName()) + " > disks"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenDisks{Node: node.Target()})
				m.disks = newDisksModel(m.application.Disks)
				return m, m.command(effect)
			}
		}
		if message.Keystroke() == "n" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewNetwork, Label: fallback(node.DisplayName()) + " > network"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenNetwork{Node: node.Target()})
				m.network = newNetworkModel(m.application.Network)
				return m, m.command(effect)
			}
		}
		if message.Keystroke() == "R" && m.application.WritesEnabled && !m.nodes.filtering {
			if targets := m.nodes.actionTargets(); len(targets) > 0 {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionReboot, Targets: targets})
				return m, m.command(effect)
			}
		}
		if message.Keystroke() == "X" && m.application.WritesEnabled && !m.nodes.filtering {
			if targets := m.nodes.actionTargets(); len(targets) > 0 {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionShutdown, Targets: targets})
				return m, m.command(effect)
			}
		}
		m.nodes = m.nodes.update(message)
		return m, nil
	case shutdownMessage:
		return m, m.shutdown()
	case splashDoneMsg:
		m.splash = false
		return m, nil
	case applicationMessage:
		if message.message == nil {
			return m, nil
		}
		var effect application.Effect
		m.application, effect = application.Update(m.application, message.message)
		m.nodes = m.nodes.setState(m.application.Nodes)
		if m.application.NodeFocus != "" && !m.nodeFocusResolved && m.application.Nodes.Status != application.Loading && m.application.Nodes.Status != application.Idle {
			m.nodeFocusResolved = true
			for index, node := range m.nodes.visibleNodes() {
				if node.Name == m.application.NodeFocus || slices.Contains(node.Addresses, m.application.NodeFocus) {
					m.nodes.selectedID = node.ID
					m.nodes = m.nodes.normalizeSelection(index)
					m.views = m.views.push(viewFrame{Kind: viewNodeDetail, Label: fallback(m.nodes.selectedValue().DisplayName())})
					break
				}
			}
		}
		m.services = m.services.setState(m.application.Services)
		m.events = m.events.setState(m.application.Events)
		m.etcd = m.etcd.setState(m.application.Etcd)
		m.processes = m.processes.setState(m.application.Processes)
		m.disks = m.disks.setState(m.application.Disks)
		m.network = m.network.setState(m.application.Network)
		m.problems = m.problems.setDiagnoses(application.EvaluateHealth(m.application))
		m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
		m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
		if m.pendingContextPicker && len(m.application.Contexts) > 0 {
			m.contexts = newContextsModel(m.application.Contexts, m.application.ContextName)
			m.pendingContextPicker = false
		}
		m.logs = m.logs.setState(m.application.Logs)
		if len(m.application.ActionResults) > 0 {
			m.notice = renderActionResults(m.application.ActionResults)
		} else {
			m.notice = ""
		}
		return m, m.command(effect)
	}

	return m, nil
}

func (m model) View() tea.View {
	prompt := m.activePrompt()
	layout := layoutShell(m.width, m.height, prompt != "")
	headerKind := m.views.top().Kind
	if m.contexts.active {
		headerKind = viewContexts
	}
	header := renderK9sHeader(
		layoutK9sHeader(layout.Width),
		deriveShellMetadata(m.application),
		actionHints(headerKind, m.application.WritesEnabled),
		m.styles.k9s,
	)

	content := ""
	if m.splash {
		content = "t9s\nInspect Talos clusters"
	} else {
		content = m.activeContent(contentSize{Width: layout.Width, Height: layout.ContentHeight})
	}
	flash := m.notice
	if flash == "" {
		switch m.application.Nodes.Status {
		case application.Idle, application.Loading:
			flash = "Loading nodes…"
		case application.Failed:
			flash = failureText(m.application)
		case application.Ready, application.Partial:
			if len(m.application.Nodes.Value.Nodes) == 0 {
				flash = "No nodes"
			}
		}
	}

	breadcrumb := m.views.breadcrumb(m.application.ContextName)
	if m.contexts.active {
		breadcrumb = m.application.ContextName + " > contexts"
	}
	footer := renderK9sFooter(layout.Width, breadcrumb, prompt, flash, m.styles.k9s)
	result := tea.NewView(renderShell(layout, header, content, footer))
	result.AltScreen = true
	result.BackgroundColor = color.Black
	result.WindowTitle = "t9s"
	return result
}

func (m model) activeContent(size contentSize) string {
	kind := m.views.top().Kind
	if m.contexts.active {
		kind = viewContexts
	}
	frame := layoutResourceFrame(size.Width, size.Height, resourceTitle(kind, m.application))
	innerSize := contentSize{Width: frame.InnerWidth, Height: frame.InnerHeight}

	if m.contexts.active {
		return renderResourceFrame(frame, m.contexts.viewSized(innerSize), m.styles.k9s)
	}

	var view strings.Builder
	switch m.views.top().Kind {
	case viewNodeDetail:
		view.WriteString(renderNodeDetail(m.nodes.selectedValue()))
	case viewServices:
		view.WriteString(m.services.viewSized(innerSize))
	case viewServiceDetail:
		view.WriteString(renderServiceDetail(m.services.selectedValue()))
	case viewServiceLogs:
		view.WriteString(m.logs.viewSized(innerSize))
	case viewEvents:
		view.WriteString(m.events.viewSized(innerSize))
	case viewEtcd:
		view.WriteString(m.etcd.viewSized(innerSize))
	case viewProcesses:
		view.WriteString(m.processes.viewSized(innerSize))
	case viewProcessDetail:
		view.WriteString(renderProcessDetail(m.processes.selectedValue()))
	case viewDisks:
		view.WriteString(m.disks.viewSized(innerSize))
	case viewDiskDetail:
		view.WriteString(renderDiskDetail(m.disks.selectedValue()))
	case viewNetwork:
		view.WriteString(m.network.viewSized(innerSize))
	case viewLinkDetail:
		view.WriteString(renderLinkDetail(m.network.selectedValue()))
	case viewOverview:
		view.WriteString(renderOverview(m.application))
	case viewProblems:
		view.WriteString(m.problems.viewSized(innerSize))
	case viewResourceKinds:
		view.WriteString(m.resourceKinds.viewSized(innerSize))
	case viewResourceInstances:
		view.WriteString(m.resourceInstances.viewSized(innerSize))
	case viewResourceDetail:
		sensitive := false
		for _, kind := range m.application.ResourceBrowser.Kinds.Kinds {
			if kind.Type == m.application.ResourceBrowser.SelectedKind {
				sensitive = kind.Sensitive
				break
			}
		}
		view.WriteString(m.resourceDetail.viewSized(innerSize, m.application.ResourceBrowser.Detail, sensitive))
	case viewHelp:
		view.WriteString("HELP\n\n" + renderActionHints(actionHints(viewNodes, m.application.WritesEnabled)) + "\n" + renderActionHints(actionHints(viewNodeDetail, m.application.WritesEnabled)))
	default:
		view.WriteString(m.nodes.viewSized(innerSize))
	}
	return renderResourceFrame(frame, view.String(), m.styles.k9s)
}

func (m model) filtering() bool {
	if m.views.top().Kind == viewServices {
		return m.services.filtering
	}
	if m.views.top().Kind == viewServiceLogs {
		return m.logs.filtering
	}
	if m.views.top().Kind == viewEvents {
		return m.events.filtering
	}
	if m.views.top().Kind == viewEtcd {
		return m.etcd.filtering
	}
	if m.views.top().Kind == viewProcesses {
		return m.processes.filtering
	}
	if m.views.top().Kind == viewDisks {
		return m.disks.filtering
	}
	if m.views.top().Kind == viewNetwork {
		return m.network.filtering
	}
	if m.views.top().Kind == viewProblems {
		return m.problems.filtering
	}
	if m.views.top().Kind == viewResourceKinds {
		return m.resourceKinds.filtering
	}
	if m.views.top().Kind == viewResourceInstances {
		return m.resourceInstances.filtering
	}
	return m.nodes.filtering
}

func (m model) activePrompt() string {
	if m.application.PendingAction != nil {
		return renderPendingActionPrompt(*m.application.PendingAction)
	}
	if prompt := m.palette.view(); prompt != "" {
		return prompt
	}
	if m.views.top().Kind == viewServices && (m.services.filtering || m.services.filter != "") {
		return "/" + m.services.filter
	}
	if m.views.top().Kind == viewServiceLogs && (m.logs.filtering || m.logs.filter != "") {
		return "/" + m.logs.filter
	}
	if m.views.top().Kind == viewNodes && (m.nodes.filtering || m.nodes.filter != "") {
		return "/" + m.nodes.filter
	}
	if m.views.top().Kind == viewEvents && (m.events.filtering || m.events.filter != "") {
		return "/" + m.events.filter
	}
	if m.views.top().Kind == viewEtcd && (m.etcd.filtering || m.etcd.filter != "") {
		return "/" + m.etcd.filter
	}
	if m.views.top().Kind == viewProcesses && (m.processes.filtering || m.processes.filter != "") {
		return "/" + m.processes.filter
	}
	if m.views.top().Kind == viewDisks && (m.disks.filtering || m.disks.filter != "") {
		return "/" + m.disks.filter
	}
	if m.views.top().Kind == viewNetwork && (m.network.filtering || m.network.filter != "") {
		return "/" + m.network.filter
	}
	if m.views.top().Kind == viewProblems && (m.problems.filtering || m.problems.filter != "") {
		return "/" + m.problems.filter
	}
	if m.views.top().Kind == viewResourceKinds && (m.resourceKinds.filtering || m.resourceKinds.filter != "") {
		return "/" + m.resourceKinds.filter
	}
	if m.views.top().Kind == viewResourceInstances && (m.resourceInstances.filtering || m.resourceInstances.filter != "") {
		return "/" + m.resourceInstances.filter
	}
	return ""
}

func (m model) command(effect application.Effect) tea.Cmd {
	if effect == nil || m.runner == nil {
		return nil
	}

	return func() tea.Msg {
		return applicationMessage{message: m.runner.Run(m.lifecycle.effectCtx, effect)}
	}
}

func (m model) watchContext() tea.Cmd {
	return func() tea.Msg {
		<-m.lifecycle.effectCtx.Done()
		return shutdownMessage{}
	}
}

func (m model) shutdown() tea.Cmd {
	return m.lifecycle.cleanup(tea.Quit())
}

func (l *lifecycle) cleanup(next tea.Msg) tea.Cmd {
	return func() tea.Msg {
		l.once.Do(func() {
			l.cancel()
			if l.runner != nil {
				_ = l.runner.Close()
			}
		})

		return next
	}
}

func fallback(value string) string {
	if value == "" {
		return "-"
	}

	return value
}

func failureText(model application.Model) string {
	if model.Notice != "" {
		return model.Notice
	}
	if model.Nodes.Err != "" {
		return model.Nodes.Err
	}

	return "load failed"
}
