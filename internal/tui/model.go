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
	dmesg             logsModel
	netstat           netstatModel
	mounts            mountsModel
	memory            memoryModel
	problems          problemsModel
	resourceKinds     resourceKindsModel
	resourceInstances resourceInstancesModel
	resourceDetail    resourceDetailModel
	logs              logsModel
	palette           commandModel
	contexts          contextsModel
	upgradePrompt     *upgradePromptModel
	snapshotPrompt    *snapshotPromptModel
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
		dmesg:             newDmesgModel(applicationModel.Dmesg),
		netstat:           newNetstatModel(applicationModel.Netstat),
		mounts:            newMountsModel(applicationModel.Mounts),
		memory:            newMemoryModel(applicationModel.Memory),
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
		return tea.Batch(m.command(m.initial), splashTimer(), m.watchContext(), nodeAutoRefreshTick())
	}

	return tea.Batch(m.command(m.initial), splashTimer(), nodeAutoRefreshTick())
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
		key := message.String()
		if key == "ctrl+c" {
			return m, m.shutdown()
		}
		if m.application.PendingAction != nil || m.application.PendingServiceAction != nil || m.application.PendingEtcdAction != nil {
			if key == "y" {
				// Confirm through the reducer first. A refused confirm (for
				// example an action that would drop etcd below quorum, or one
				// whose state changed since the prompt opened) leaves the
				// pending prompt in place and must not build or dispatch any
				// mutation effects.
				if m.application.PendingAction != nil {
					pending := *m.application.PendingAction
					var confirmEffect application.Effect
					m.application, confirmEffect = application.Update(m.application, application.ConfirmPendingAction{})
					if m.application.PendingAction != nil {
						return m, m.command(confirmEffect)
					}
					// A confirmed action has fired for the currently marked rows;
					// clear marks so a subsequent R/X doesn't silently re-target
					// already-actioned nodes.
					m.nodes.marked = nil
					effects := application.BuildActionEffects(m.application, pending)
					cmds := make([]tea.Cmd, 0, len(effects)+1)
					if cmd := m.command(confirmEffect); cmd != nil {
						cmds = append(cmds, cmd)
					}
					for _, effect := range effects {
						cmds = append(cmds, m.command(effect))
					}
					return m, tea.Batch(cmds...)
				}
				if m.application.PendingEtcdAction != nil {
					// Confirm through the reducer: a blocked confirm is refused and
					// leaves the prompt in place, and the reducer is what builds the
					// effect, so nothing fires before a successful confirm. A
					// destructive action stays pending across the snapshot stage, so
					// dispatch whatever effect the reducer returned.
					if m.application.PendingEtcdAction.Blocked != "" {
						return m, nil
					}
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.ConfirmEtcdAction{})
					return m, m.command(effect)
				}
				pending := *m.application.PendingServiceAction
				var confirmEffect application.Effect
				m.application, confirmEffect = application.Update(m.application, application.ConfirmPendingAction{})
				if m.application.PendingServiceAction != nil {
					return m, m.command(confirmEffect)
				}
				effect := application.BuildServiceActionEffect(m.application, pending)
				cmds := make([]tea.Cmd, 0, 2)
				if cmd := m.command(confirmEffect); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if cmd := m.command(effect); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			}
			var effect application.Effect
			m.application, effect = application.Update(m.application, application.CancelPendingAction{})
			return m, m.command(effect)
		}
		if key == "esc" && !m.contexts.active && !m.palette.active && m.upgradePrompt == nil && m.snapshotPrompt == nil && !m.filtering() {
			wasLogs := m.views.top().Kind == viewServiceLogs
			wasDmesg := m.views.top().Kind == viewDmesg
			if views, ok := m.views.pop(); ok {
				m.views = views
				if wasLogs {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.CloseServiceLogs{})
					m.logs = m.logs.setState(m.application.Logs)
					return m, m.command(effect)
				}
				if wasDmesg {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.CloseDmesg{})
					m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
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
			switch key {
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
		if m.upgradePrompt != nil {
			switch key {
			case "esc":
				m.upgradePrompt = nil
				return m, nil
			case "enter":
				target := m.upgradePrompt.target
				image := m.upgradePrompt.input.Value()
				m.upgradePrompt = nil
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionUpgrade, Targets: []string{target}, Image: image})
				return m, m.command(effect)
			}
			var command tea.Cmd
			*m.upgradePrompt, command = m.upgradePrompt.update(message)
			return m, command
		}
		if m.snapshotPrompt != nil {
			switch key {
			case "esc":
				m.snapshotPrompt = nil
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.CancelEtcdSnapshotPrompt{})
				return m, m.command(effect)
			case "enter":
				node := m.snapshotPrompt.node
				member := m.snapshotPrompt.member
				path := m.snapshotPrompt.input.Value()
				m.snapshotPrompt = nil
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.ConfirmEtcdSnapshotPrompt{Node: node, MemberHostname: member, Path: path})
				// A synchronous validation failure (invalid path) never emits a
				// message, so surface it here; runtime outcomes arrive via the
				// message branch below.
				if notice := renderEtcdSnapshotNotice(m.application.EtcdSnapshot); notice != "" {
					m.notice = notice
				}
				return m, m.command(effect)
			}
			var command tea.Cmd
			*m.snapshotPrompt, command = m.snapshotPrompt.update(message)
			return m, command
		}
		switch key {
		case ":":
			if !m.filtering() {
				m.notice = ""
				m.palette, _ = m.palette.open()
				return m, nil
			}
		case "q":
			if !m.filtering() {
				wasLogs := m.views.top().Kind == viewServiceLogs
				wasDmesg := m.views.top().Kind == viewDmesg
				if views, ok := m.views.pop(); ok {
					m.views = views
					if wasLogs {
						var effect application.Effect
						m.application, effect = application.Update(m.application, application.CloseServiceLogs{})
						m.logs = m.logs.setState(m.application.Logs)
						return m, m.command(effect)
					}
					if wasDmesg {
						var effect application.Effect
						m.application, effect = application.Update(m.application, application.CloseDmesg{})
						m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
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
			if !m.filtering() && m.views.top().Kind == viewDmesg {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.ReconnectDmesg{})
				m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewNetstat {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshNetstat{})
				m.netstat = m.netstat.setState(m.application.Netstat)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewMounts {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshMounts{})
				m.mounts = m.mounts.setState(m.application.Mounts)
				return m, m.command(effect)
			}
			if !m.filtering() && m.views.top().Kind == viewMemory {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RefreshMemory{})
				m.memory = m.memory.setState(m.application.Memory)
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
		if m.views.top().Kind == viewDmesg {
			m.dmesg = m.dmesg.update(message)
			if m.dmesg.clearRequested {
				m.dmesg.clearRequested = false
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.ClearDmesg{})
				m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
				return m, m.command(effect)
			}
			return m, nil
		}
		if m.views.top().Kind == viewNetstat {
			m.netstat = m.netstat.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewMounts {
			m.mounts = m.mounts.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewMemory {
			m.memory = m.memory.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewServices {
			if key == "l" && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					label := fallback(service.Name) + "@" + fallback(service.Node)
					m.views = m.views.push(viewFrame{Kind: viewServiceLogs, Label: label + " > logs"})
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.OpenServiceLogs{Request: domain.LogRequest{Node: service.Node, Service: service.Name}})
					m.logs = newLogsModel(m.application.Logs)
					return m, m.command(effect)
				}
			}
			if (key == "enter" || key == "d") && !m.services.filtering {
				if _, ok := m.services.selected(); ok {
					service := m.services.selectedValue()
					m.views = m.views.push(viewFrame{Kind: viewServiceDetail, Label: fallback(service.Name) + "@" + fallback(service.Node)})
					return m, nil
				}
			}
			if key == "S" && m.writeActionsEnabled() && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RequestServiceAction{Kind: application.ServiceActionStart, Node: service.Node, Service: service.Name})
					return m, m.command(effect)
				}
			}
			if key == "T" && m.writeActionsEnabled() && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RequestServiceAction{Kind: application.ServiceActionStop, Node: service.Node, Service: service.Name})
					return m, m.command(effect)
				}
			}
			if key == "R" && m.writeActionsEnabled() && !m.services.filtering {
				if service, ok := m.services.selected(); ok {
					var effect application.Effect
					m.application, effect = application.Update(m.application, application.RequestServiceAction{Kind: application.ServiceActionRestart, Node: service.Node, Service: service.Name})
					return m, m.command(effect)
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
			// The etcd sub-model is purely presentational and cannot emit
			// application messages, so the write keys are handled here before
			// delegating the remaining keys (filter, navigation) to it.
			if m.writeActionsEnabled() && !m.etcd.filtering {
				if member, ok := m.etcd.selected(); ok {
					switch key {
					case "s":
						if member.MemberID != 0 && member.Hostname != "" {
							var effect application.Effect
							m.application, effect = application.Update(m.application, application.RequestEtcdSnapshotPrompt{Node: member.Hostname, MemberHostname: member.Hostname})
							return m, m.command(effect)
						}
					case "d":
						return m.requestEtcdAction(application.EtcdActionDefragment, member)
					case "A":
						return m.requestEtcdAction(application.EtcdActionDisarmAlarms, member)
					case "R":
						return m.requestEtcdAction(application.EtcdActionRemoveMember, member)
					case "L":
						return m.requestEtcdAction(application.EtcdActionLeaveCluster, member)
					}
				}
			}
			m.etcd = m.etcd.update(message)
			return m, nil
		}
		if m.views.top().Kind == viewProcesses {
			if (key == "enter" || key == "d") && !m.processes.filtering {
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
			if (key == "enter" || key == "d") && !m.disks.filtering {
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
			if (key == "enter" || key == "d") && !m.network.filtering {
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
			if (key == "enter" || key == "d") && !m.problems.filtering {
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
			if !m.filtering() && key == "r" {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenResourceBrowser{})
				m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
				return m, m.command(effect)
			}
			if (key == "enter" || key == "d") && !m.resourceKinds.filtering {
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
			if !m.filtering() && key == "r" {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.SelectResourceKind{Kind: m.application.ResourceBrowser.SelectedKind, Node: m.application.ResourceBrowser.SelectedNode})
				m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
				return m, m.command(effect)
			}
			if (key == "enter" || key == "d") && !m.resourceInstances.filtering {
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
			if key == "r" {
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
		if (key == "enter" || key == "d") && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				m.views = m.views.push(viewFrame{Kind: viewNodeDetail, Label: fallback(m.nodes.selectedValue().DisplayName())})
				return m, nil
			}
		}
		if key == "p" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewProcesses, Label: fallback(node.DisplayName()) + " > processes"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenProcesses{Node: node.Target()})
				m.processes = newProcessesModel(m.application.Processes)
				return m, m.command(effect)
			}
		}
		if key == "k" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewDisks, Label: fallback(node.DisplayName()) + " > disks"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenDisks{Node: node.Target()})
				m.disks = newDisksModel(m.application.Disks)
				return m, m.command(effect)
			}
		}
		if key == "n" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewNetwork, Label: fallback(node.DisplayName()) + " > network"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenNetwork{Node: node.Target()})
				m.network = newNetworkModel(m.application.Network)
				return m, m.command(effect)
			}
		}
		if key == "e" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewDmesg, Label: fallback(node.DisplayName()) + " > dmesg"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenDmesg{Request: domain.DmesgRequest{Node: node.Target(), Follow: true, Tail: true}})
				m.dmesg = newDmesgModel(m.application.Dmesg)
				return m, m.command(effect)
			}
		}
		if key == "s" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewNetstat, Label: fallback(node.DisplayName()) + " > netstat"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenNetstat{Node: node.Target()})
				m.netstat = newNetstatModel(m.application.Netstat)
				return m, m.command(effect)
			}
		}
		if key == "m" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewMounts, Label: fallback(node.DisplayName()) + " > mounts"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenMounts{Node: node.Target()})
				m.mounts = newMountsModel(m.application.Mounts)
				return m, m.command(effect)
			}
		}
		if key == "f" && !m.nodes.filtering {
			if _, ok := m.nodes.selected(); ok {
				node := m.nodes.selectedValue()
				m.views = m.views.push(viewFrame{Kind: viewMemory, Label: fallback(node.DisplayName()) + " > memory"})
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.OpenMemory{Node: node.Target()})
				m.memory = newMemoryModel(m.application.Memory)
				return m, m.command(effect)
			}
		}
		if key == "R" && m.writeActionsEnabled() && !m.nodes.filtering {
			if targets := m.nodes.actionTargets(); len(targets) > 0 {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionReboot, Targets: targets})
				return m, m.command(effect)
			}
		}
		if key == "X" && m.writeActionsEnabled() && !m.nodes.filtering {
			if targets := m.nodes.actionTargets(); len(targets) > 0 {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionShutdown, Targets: targets})
				return m, m.command(effect)
			}
		}
		if key == "B" && m.writeActionsEnabled() && !m.nodes.filtering {
			if targets := m.nodes.actionTargets(); len(targets) > 0 {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestAction{Kind: application.ActionRollback, Targets: targets})
				return m, m.command(effect)
			}
		}
		if key == "U" && m.writeActionsEnabled() && !m.nodes.filtering && m.upgradePrompt == nil {
			if node, ok := m.nodes.selected(); ok {
				var effect application.Effect
				m.application, effect = application.Update(m.application, application.RequestUpgradePrompt{Target: node.Target()})
				return m, m.command(effect)
			}
		}
		if key == "space" && !m.writeActionsEnabled() && !m.nodes.filtering {
			// Row-marking is a write-action affordance (feeds R/X); keep it
			// inert while writes are disabled so the nodes screen behaves
			// exactly as it did before this feature, per spec.
			return m, nil
		}
		m.nodes = m.nodes.update(message)
		return m, nil
	case shutdownMessage:
		return m, m.shutdown()
	case splashDoneMsg:
		m.splash = false
		return m, nil
	case nodeAutoRefreshTickMsg:
		if m.filtering() || m.views.top().Kind != viewNodes {
			return m, nodeAutoRefreshTick()
		}
		var nodesEffect, kubernetesEffect application.Effect
		m.application, nodesEffect = application.Update(m.application, application.RefreshNodes{})
		m.application, kubernetesEffect = application.Update(m.application, application.RefreshKubernetesNodes{})
		m.nodes = m.nodes.setState(m.application.Nodes)
		return m, tea.Batch(m.command(nodesEffect), m.command(kubernetesEffect), nodeAutoRefreshTick())
	case applicationMessage:
		if message.message == nil {
			return m, nil
		}
		if _, ok := message.message.(application.SelectContext); ok {
			// A different Talos context may reuse the same node IDs/hostnames
			// (e.g. cp-1 across clusters); never let a mark or an in-flight
			// upgrade/snapshot prompt carry across a context switch and silently
			// mistarget the new cluster.
			m.nodes.marked = nil
			m.upgradePrompt = nil
			m.snapshotPrompt = nil
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
		m.dmesg = m.dmesg.setDmesgState(m.application.Dmesg)
		m.netstat = m.netstat.setState(m.application.Netstat)
		m.mounts = m.mounts.setState(m.application.Mounts)
		m.memory = m.memory.setState(m.application.Memory)
		m.problems = m.problems.setDiagnoses(application.EvaluateHealth(m.application))
		m.resourceKinds = m.resourceKinds.setState(m.application.ResourceBrowser)
		m.resourceInstances = m.resourceInstances.setState(m.application.ResourceBrowser)
		if m.pendingContextPicker && len(m.application.Contexts) > 0 {
			m.contexts = newContextsModel(m.application.Contexts, m.application.ContextName)
			m.pendingContextPicker = false
		}
		m.logs = m.logs.setState(m.application.Logs)
		if upgradeNotice := renderUpgradeNotice(m.application.Upgrade); upgradeNotice != "" {
			m.notice = upgradeNotice
		} else if len(m.application.ActionResults) > 0 {
			m.notice = renderActionResults(m.application.ActionResults, m.application.ActionTotal)
		} else {
			m.notice = ""
		}
		switch message.message.(type) {
		case application.EtcdSnapshotSucceeded, application.EtcdSnapshotFailed:
			if notice := renderEtcdSnapshotNotice(m.application.EtcdSnapshot); notice != "" {
				m.notice = notice
			}
		}
		var promptFocus tea.Cmd
		if opened, ok := message.message.(application.UpgradePromptOpened); ok &&
			opened.Generation == m.application.Generation &&
			m.application.PendingAction == nil && m.application.PendingServiceAction == nil &&
			m.views.top().Kind == viewNodes {
			prompt := newUpgradePromptModel(opened.Target, opened.Image)
			m.upgradePrompt = &prompt
			promptFocus = m.upgradePrompt.input.Focus()
		}
		if opened, ok := message.message.(application.EtcdSnapshotPromptOpened); ok &&
			opened.Generation == m.application.Generation &&
			m.application.PendingAction == nil && m.application.PendingServiceAction == nil && m.application.PendingEtcdAction == nil &&
			m.views.top().Kind == viewEtcd {
			prompt := newSnapshotPromptModel(opened.Node, opened.MemberHostname, opened.DefaultPath)
			m.snapshotPrompt = &prompt
			promptFocus = m.snapshotPrompt.input.Focus()
		}
		return m, tea.Batch(m.command(effect), promptFocus)
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
		actionHints(headerKind, m.writeActionsEnabled()),
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
	case viewDmesg:
		view.WriteString(m.dmesg.viewSized(innerSize))
	case viewNetstat:
		view.WriteString(m.netstat.viewSized(innerSize))
	case viewMounts:
		view.WriteString(m.mounts.viewSized(innerSize))
	case viewMemory:
		view.WriteString(m.memory.viewSized(innerSize))
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
		view.WriteString("HELP\n\n" + renderActionHints(actionHints(viewNodes, m.writeActionsEnabled())) + "\n" + renderActionHints(actionHints(viewNodeDetail, m.writeActionsEnabled())))
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
	if m.views.top().Kind == viewDmesg {
		return m.dmesg.filtering
	}
	if m.views.top().Kind == viewNetstat {
		return m.netstat.filtering
	}
	if m.views.top().Kind == viewMounts {
		return m.mounts.filtering
	}
	if m.views.top().Kind == viewMemory {
		return m.memory.filtering
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
	if m.application.PendingServiceAction != nil {
		return renderPendingServiceActionPrompt(*m.application.PendingServiceAction)
	}
	if m.application.PendingEtcdAction != nil {
		return renderPendingEtcdActionPrompt(*m.application.PendingEtcdAction)
	}
	if m.upgradePrompt != nil {
		return m.upgradePrompt.view()
	}
	if m.snapshotPrompt != nil {
		return m.snapshotPrompt.view()
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
	if m.views.top().Kind == viewDmesg && (m.dmesg.filtering || m.dmesg.filter != "") {
		return "/" + m.dmesg.filter
	}
	if m.views.top().Kind == viewNetstat && (m.netstat.filtering || m.netstat.filter != "") {
		return "/" + m.netstat.filter
	}
	if m.views.top().Kind == viewMounts && (m.mounts.filtering || m.mounts.filter != "") {
		return "/" + m.mounts.filter
	}
	if m.views.top().Kind == viewMemory && (m.memory.filtering || m.memory.filter != "") {
		return "/" + m.memory.filter
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

func (m model) writeActionsEnabled() bool {
	return m.application.WritesEnabled && !m.application.Upgrade.Active
}

// requestEtcdAction opens the maintenance confirm prompt for the selected
// member. The RPC is addressed to the member's own node: defragment and
// disarm act on the node they are sent to, so a different target would
// operate on the wrong member.
func (m model) requestEtcdAction(kind application.EtcdActionKind, member domain.EtcdMemberSnapshot) (tea.Model, tea.Cmd) {
	if member.Hostname == "" {
		return m, nil
	}
	var effect application.Effect
	m.application, effect = application.Update(m.application, application.RequestEtcdAction{
		Kind:           kind,
		MemberID:       member.MemberID,
		MemberHostname: member.Hostname,
		Node:           member.Hostname,
	})
	return m, m.command(effect)
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
