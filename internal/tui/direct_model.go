package tui

import (
	"context"
	"fmt"
	"image/color"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type model struct {
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

	cluster             directClusterState
	view                directViewState
	nodes               nodesModel
	services            servicesModel
	events              eventsModel
	etcd                etcdModel
	problems            problemsModel
	contexts            contextsModel
	palette             commandModel
	processes           processesModel
	disks               disksModel
	network             networkModel
	netstat             netstatModel
	mounts              mountsModel
	memory              memoryModel
	time                timeModel
	cpu                 cpuModel
	timePrompt          *timePromptModel
	upgradePrompt       *upgradePromptModel
	etcdPathPrompt      *snapshotPromptModel
	resourceKinds       resourceKindsModel
	resourceInstances   resourceInstancesModel
	resourceDetail      resourceDetailModel
	pendingResourceKind string
	logs                logsModel
	dmesg               logsModel
	healthcheck         healthcheckModel
	logStream           directLogStreamState
	dmesgStream         directDmesgStreamState
	eventFollow         directEventFollowState
	healthStream        directHealthStreamState
	views               viewStack
	mutation            directMutationState
	mutationWork        directMutationWork
	mutationSequence    uint64
	styles              styles
	width               int
	height              int
	splash              bool
	lifecycle           *directLifecycle
}

type directLifecycle struct {
	mu      sync.Mutex
	session *application.Session
}

func (l *directLifecycle) setSession(session *application.Session) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.session = session
	l.mu.Unlock()
}

func (l *directLifecycle) takeSession() *application.Session {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	session := l.session
	l.session = nil
	return session
}

type directLogStreamState struct {
	request directRequest
	stream  ports.ServiceLogStream
	target  domain.LogRequest
	status  directLoadStatus
	err     string
	eof     bool
}

type directDmesgStreamState struct {
	request directRequest
	stream  ports.DmesgStream
	target  domain.DmesgRequest
	status  directLoadStatus
	err     string
	eof     bool
}

type directEventFollowState struct {
	request   directRequest
	stream    ports.EventStream
	attempted bool
	following bool
	err       string
}

type directHealthStreamState struct {
	request directRequest
	stream  ports.ClusterHealthStream
	target  domain.ClusterHealthRequest
}

type directClearLogs struct{ dmesg bool }

// NewWithCleanup constructs the direct TUI over a shared application client.
// The returned command is an idempotent fallback for terminal runtimes that
// exit without delivering Bubble Tea's normal shutdown message.
func NewWithCleanup(parent context.Context, client *application.Client, options LaunchOptions) (tea.Model, tea.Cmd) {
	model := newModel(parent, client, options)
	return model, model.cleanup(tea.Quit())
}

func newModel(parent context.Context, client *application.Client, options LaunchOptions) model {
	if parent == nil {
		parent = context.Background()
	}
	rootCtx, rootCancel := context.WithCancel(parent)
	nodeState := directNodeState{Status: directLoading}
	return model{
		client: client, options: options, requests: newDirectRequests(), lifecycle: &directLifecycle{},
		rootCtx: rootCtx, rootCancel: rootCancel,
		cluster: directClusterState{
			contextName: options.ContextName,
			nodes:       nodeState,
			services:    directServiceState{Status: directLoading},
			events:      directEventState{Status: directLoading},
			etcd:        directEtcdState{Status: directLoading},
			kubernetes:  directKubernetesState{Status: directLoading},
		},
		nodes:             newNodesModel(),
		services:          newServicesModel(),
		events:            newEventsModel(),
		etcd:              newEtcdModel(),
		problems:          newProblemsModel(),
		processes:         newDirectProcessesModel(),
		disks:             newDirectDisksModel(),
		network:           newDirectNetworkModel(),
		netstat:           newDirectNetstatModel(),
		mounts:            newDirectMountsModel(),
		memory:            newDirectMemoryModel(),
		time:              newDirectTimeModel(),
		cpu:               newDirectCPUModel(),
		resourceKinds:     newResourceKindsModel(resourceBrowserState{}),
		resourceInstances: newResourceInstancesModel(resourceBrowserState{}),
		resourceDetail:    newResourceDetailModel(),
		logs:              newDirectServiceLogsModel(),
		dmesg:             newDirectDmesgModel(),
		healthcheck:       newDirectHealthcheckModel(healthcheckDisplayState{}),
		palette:           newCommandModel(),
		views:             newViewStack(viewFrame{Kind: viewNodes, Label: "nodes"}),
		styles:            defaultStyles(), width: 120, height: 40, splash: true,
	}
}

func (m model) Init() tea.Cmd {
	request, _ := m.requests.beginWithContext("contexts", 0, m.rootCtx)
	return tea.Batch(directContextsCommand(m.client, request), splashTimer(), nodeAutoRefreshTick())
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
		m.time = m.time.clampOffset(m.timeViewportHeight())
		m.resizeDirectResourceDetail()
		m.syncDirectStreamViewports()
		return m, nil
	case splashDoneMsg:
		m.splash = false
		return m, nil
	case nodeAutoRefreshTickMsg:
		if m.views.top().Kind == viewNodes && !m.directFilterEditing() && m.session != nil {
			return m, tea.Batch(m.refreshDirectNodes(), nodeAutoRefreshTick())
		}
		return m, nodeAutoRefreshTick()
	case directContextsLoaded:
		if !m.requests.current(message.request) {
			return m, nil
		}
		m.requests.cancelSlot("contexts")
		m.cluster.contexts = append([]domain.ClusterContext(nil), message.contexts...)
		if message.err != nil {
			m.cluster.nodes = directNodeState{Status: directFailed, Err: "Context discovery failed"}
			m.cluster.services = directServiceState{Status: directFailed, Err: "Service data is unavailable"}
			m.cluster.events = directEventState{Status: directFailed, Err: "Event data is unavailable"}
			m.cluster.etcd = directEtcdState{Status: directFailed, Err: "Etcd data is unavailable"}
			m.cluster.kubernetes = directKubernetesState{Status: directFailed, Err: "Kubernetes data is unavailable"}
			m.view.notice = "Unable to discover contexts"
			return m, nil
		}
		selected, err := directResolveContext(message.contexts, m.options.ContextName)
		if err != nil {
			m.cluster.nodes = directNodeState{Status: directFailed, Err: "Selected context is unavailable"}
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
			m.cluster.nodes = directNodeState{Status: directFailed, Err: "Session unavailable"}
			m.cluster.services = directServiceState{Status: directFailed, Err: "Service data is unavailable"}
			m.cluster.events = directEventState{Status: directFailed, Err: "Event data is unavailable"}
			m.cluster.etcd = directEtcdState{Status: directFailed, Err: "Etcd data is unavailable"}
			m.cluster.kubernetes = directKubernetesState{Status: directFailed, Err: "Kubernetes data is unavailable"}
			m.view.notice = "Unable to open session"
			return m, nil
		}
		m.session = message.session
		m.lifecycle.setSession(message.session)
		m.cluster.contextName = message.contextName
		return m, m.startClusterObservations(message.session, message.request.generation)
	case directCurrentInstallImage, directServicePrepared, directNodePrepared, directEtcdPrepared, directMutationProgress, directUpgradeProgress, directMutationCanceled, directMutationFinished:
		return m.updateDirectMutationResult(message)
	case directEventFollowOpened, directEventFollowBatch, directClusterHealthOpened, directClusterHealthBatch:
		return m.updateDirectEventAndHealthResult(message)
	case directNodeServicesLoaded, directEventsLoaded, directEtcdLoaded, directKubernetesNodesLoaded:
		return m.updateDirectClusterResult(message)
	case directProcessesLoaded, directDisksLoaded, directNetworkLoaded, directSocketsLoaded, directMountsLoaded, directMemoryLoaded,
		directCPULoaded, directTimeLoaded, directTimeCheckLoaded:
		m.applyDiagnosticResult(message)
		return m, nil
	case directServiceLogsOpened, directServiceLogBatch, directDmesgOpened, directDmesgBatch, directClearLogs:
		return m.updateDirectLogsResult(message)
	case directResourceKindsLoaded, directResourceInstancesLoaded, directResourceDetailLoaded:
		return m, m.applyResourceResult(message)
	case directRunCommand:
		return m, m.runClusterCommand(message.value)
	case tea.KeyPressMsg:
		m.splash = false
		if message.String() == "ctrl+c" {
			return m, m.cleanup(tea.Quit())
		}
		if command, handled := m.handleDirectMutationInput(message); handled {
			return m, command
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
			case "esc", "q":
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
		if message.String() == ":" && !m.directFilterEditing() {
			var command tea.Cmd
			m.palette, command = m.palette.open()
			return m, command
		}
		if message.String() == "?" && !m.directFilterEditing() {
			m.views = m.views.push(viewFrame{Kind: viewHelp, Label: "help"})
			return m, nil
		}
		if m.views.top().Kind == viewHelp {
			if message.String() == "q" || message.String() == "esc" {
				m.popDirectView()
			}
			return m, nil
		}
		if command, handled := m.handleDiagnosticKey(message); handled {
			return m, command
		}
		if command, handled := m.handleResourceKey(message); handled {
			return m, command
		}
		if m.views.top().Kind == viewNodeDetail || m.views.top().Kind == viewServiceDetail {
			if m.views.top().Kind == viewServiceDetail && !m.mutation.busy && m.mutation.prompt == nil && m.options.WritesEnabled && m.session != nil {
				if kind := directServiceActionKind(message.String()); kind != "" {
					frame := m.views.top()
					service, ok := findServiceByKey(m.cluster.services.Value.Services, frame.ServiceKey)
					if ok {
						return m, m.prepareDirectServiceMutation(service, kind)
					}
				}
			}
			if m.views.top().Kind == viewServiceDetail && message.String() == "l" && m.session != nil {
				if service, ok := findServiceByKey(m.cluster.services.Value.Services, m.views.top().ServiceKey); ok {
					return m, m.openDirectServiceLogs(service)
				}
				m.view.notice = "Service unavailable"
				return m, nil
			}
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
			if !m.nodes.filtering && !m.mutation.busy && m.mutation.prompt == nil && m.upgradePrompt == nil && m.options.WritesEnabled && m.session != nil {
				var kind application.ActionKind
				switch message.String() {
				case "R":
					kind = application.ActionReboot
				case "X":
					kind = application.ActionShutdown
				case "B":
					kind = application.ActionRollback
				case "W":
					kind = application.ActionReset
				case "U":
					if node, ok := m.nodes.selected(m.cluster.nodes.Value.Nodes); ok {
						return m, m.beginDirectUpgrade(node.Target())
					}
				}
				if kind != "" {
					targets := m.nodes.actionTargets(m.cluster.nodes.Value.Nodes)
					if len(targets) > 0 {
						request := application.NodeActionRequest{Kind: kind, Targets: targets}
						if kind == application.ActionReset {
							mode := ports.WipeModeAll
							if len(targets) > 1 {
								mode = ports.WipeModeSystemDisk
							}
							request.Reset = &ports.ResetOptions{Mode: mode, Reboot: true}
						}
						return m, m.prepareDirectNodeMutation(request)
					}
				}
			}
			if command, handled := m.handleNodeDiagnosticKey(message); handled {
				return m, command
			}
			if message.String() == "e" && !m.nodes.filtering && m.session != nil {
				if node, ok := m.nodes.selected(m.cluster.nodes.Value.Nodes); ok {
					return m, m.openDirectDmesg(node.Target())
				}
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
			if message.String() == "l" && !m.services.filtering && m.session != nil {
				if service, ok := m.services.selected(m.cluster.services.Value.Services); ok {
					return m, m.openDirectServiceLogs(service)
				}
			}
			if !m.services.filtering && !m.mutation.busy && m.mutation.prompt == nil && m.options.WritesEnabled && m.session != nil {
				if kind := directServiceActionKind(message.String()); kind != "" {
					if service, ok := m.services.selected(m.cluster.services.Value.Services); ok {
						return m, m.prepareDirectServiceMutation(service, kind)
					}
				}
			}
			m.services = m.services.update(message, m.cluster.services.Value.Services)
		} else if m.views.top().Kind == viewEvents {
			if message.String() == "q" && !m.events.filtering {
				return m, m.cleanup(tea.Quit())
			}
			if message.String() == "r" && !m.events.filtering && m.session != nil {
				if m.eventFollow.following {
					m.events = m.events.update(message, eventRows(m.cluster.events.Value))
					return m, nil
				}
				request, _ := m.requests.beginWithContext("cluster-events", m.generation(), m.sessionCtx)
				return m, directEventsCommand(m.session, request)
			}
			if message.String() == "s" && !m.events.filtering && m.session != nil {
				if m.eventFollow.following {
					m.closeDirectEventFollow()
					return m, nil
				}
				return m, m.openDirectEventFollow()
			}
			m.events = m.events.update(message, eventRows(m.cluster.events.Value))
		} else if m.views.top().Kind == viewClusterHealth {
			if !m.healthcheck.filtering && (message.String() == "q" || message.String() == "esc") {
				previous := m.views
				m.popDirectView()
				m.closeStreamsOutsideView(previous)
				return m, nil
			}
			if message.String() == "r" && !m.healthcheck.filtering && m.session != nil {
				return m, m.reconnectDirectClusterHealth()
			}
			m.healthcheck = m.healthcheck.update(message)
			if m.healthcheck.clearRequested {
				m.healthcheck.clearRequested = false
				state := m.healthcheck.state
				state.Lines = nil
				m.healthcheck = m.healthcheck.setStreamState(state)
			}
			m.syncDirectStreamViewports()
		} else if m.views.top().Kind == viewEtcd {
			if m.mutation.snapshot != nil && !m.etcd.filtering && message.String() == "/" {
				m.mutation.receiptVisible = false
			}
			if m.mutation.snapshot != nil && m.mutation.receiptVisible && m.canShowDirectEtcdReceipt() && !m.mutation.busy && m.mutation.prompt == nil && (message.String() == "esc" || message.String() == "q") {
				m.mutation.receiptVisible = false
				return m, nil
			}
			if message.String() == "q" && !m.etcd.filtering {
				return m, m.cleanup(tea.Quit())
			}
			if message.String() == "r" && !m.etcd.filtering && m.session != nil {
				request, _ := m.requests.beginWithContext("cluster-etcd", m.generation(), m.sessionCtx)
				return m, directEtcdCommand(m.session, request)
			}
			if m.mutation.snapshot != nil && m.mutation.receiptVisible && m.canShowDirectEtcdReceipt() && !m.mutation.busy && m.mutation.prompt == nil && (message.Code == tea.KeyPgDown || message.Code == tea.KeyPgUp) {
				frame := layoutResourceFrame(m.width, layoutShell(m.width, m.height).ContentHeight, "etcd receipt")
				pageSize := max(1, frame.InnerHeight-1)
				lines := etcdReceiptLines(m.mutation, frame.InnerWidth)
				maxOffset := max(0, len(lines)-pageSize)
				if message.Code == tea.KeyPgDown {
					m.mutation.receiptOffset = min(maxOffset, m.mutation.receiptOffset+pageSize)
				} else {
					m.mutation.receiptOffset = max(0, m.mutation.receiptOffset-pageSize)
				}
				return m, nil
			}
			if m.mutation.snapshot != nil && m.canShowDirectEtcdReceipt() && !m.mutation.busy && m.mutation.prompt == nil && message.String() == "v" {
				m.mutation.receiptVisible = !m.mutation.receiptVisible
				return m, nil
			}
			if !m.etcd.filtering && !m.mutation.busy && m.mutation.prompt == nil && m.options.WritesEnabled && m.session != nil {
				if member, ok := m.etcd.selected(m.cluster.etcd.Value.Members); ok {
					var kind application.EtcdActionKind
					switch message.String() {
					case "R":
						kind = application.EtcdActionRemoveMember
					case "L":
						kind = application.EtcdActionLeaveCluster
					case "d":
						kind = application.EtcdActionDefragment
					case "A":
						kind = application.EtcdActionDisarmAlarms
					case "F":
						kind = application.EtcdActionForfeitLeadership
					case "s":
						kind = application.EtcdActionSnapshot
					}
					if kind != "" {
						if kind == application.EtcdActionSnapshot || kind == application.EtcdActionRemoveMember || kind == application.EtcdActionLeaveCluster {
							defaultPath := application.DefaultEtcdSnapshotPath(m.cluster.contextName, member.Hostname, time.Now().UTC())
							pathPrompt := newSnapshotPromptModel(member.Hostname, member.Hostname, defaultPath)
							pathPrompt.kind = kind
							pathPrompt.memberID = member.MemberID
							m.mutation.notice = ""
							m.mutation.etcdKind = ""
							m.mutation.snapshot = nil
							m.mutation.receiptVisible = false
							m.mutation.receiptOffset = 0
							m.palette = m.palette.close()
							m.etcdPathPrompt = &pathPrompt
							return m, nil
						}
						request := application.EtcdActionRequest{Kind: kind, Node: member.Hostname}
						if kind == application.EtcdActionForfeitLeadership {
							request.MemberID, request.MemberHostname = member.MemberID, member.Hostname
						}
						return m, m.prepareDirectEtcdMutation(request, member)
					}
				}
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
		if m.views.top().Kind == viewServiceLogs {
			if (message.String() == "esc" && !m.logs.filtering) || (message.String() == "q" && !m.logs.filtering) {
				m.closeDirectLogs(false)
				m.popDirectView()
				return m, nil
			}
			if message.String() == "r" && m.session != nil && !m.logs.filtering {
				return m, m.reconnectDirectLogs(false)
			}
			m.logs = m.logs.update(message)
			m.syncDirectStreamViewports()
			if m.logs.clearRequested {
				m.logs.clearRequested = false
				return m, func() tea.Msg { return directClearLogs{} }
			}
			return m, nil
		}
		if m.views.top().Kind == viewDmesg {
			if (message.String() == "esc" && !m.dmesg.filtering) || (message.String() == "q" && !m.dmesg.filtering) {
				m.closeDirectLogs(true)
				m.popDirectView()
				return m, nil
			}
			if message.String() == "r" && m.session != nil && !m.dmesg.filtering {
				return m, m.reconnectDirectLogs(true)
			}
			m.dmesg = m.dmesg.update(message)
			m.syncDirectStreamViewports()
			if m.dmesg.clearRequested {
				m.dmesg.clearRequested = false
				return m, func() tea.Msg { return directClearLogs{dmesg: true} }
			}
			return m, nil
		}
	}
	return m, nil
}

func (m model) timeViewportHeight() int {
	layout := layoutShell(m.width, m.height)
	return layoutResourceFrame(layout.Width, layout.ContentHeight, "").InnerHeight
}

func (m *model) startClusterObservations(session *application.Session, generation uint64) tea.Cmd {
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

func (m *model) refreshDirectNodes() tea.Cmd {
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

func directServiceActionKind(key string) application.ServiceActionKind {
	switch key {
	case "S":
		return application.ServiceActionStart
	case "T":
		return application.ServiceActionStop
	case "R":
		return application.ServiceActionRestart
	default:
		return ""
	}
}

func (m *model) prepareDirectServiceMutation(service domain.ServiceSnapshot, kind application.ServiceActionKind) tea.Cmd {
	m.mutation.upgrade = directUpgradeDisplayState{}
	m.mutation.upgradeResult = nil
	m.mutation.upgradeErr = nil
	operation := m.nextMutationOperation()
	m.mutation.operation = operation
	m.mutation.busy = true
	m.mutation.notice = ""
	m.mutation.results = nil
	m.view.notice = ""
	return directPrepareServiceCommand(m.session, m.sessionCtx, operation, m.generation(), application.ServiceActionRequest{Kind: kind, Node: service.Node, Service: service.Name})
}

func (m *model) prepareDirectEtcdMutation(request application.EtcdActionRequest, member domain.EtcdMemberSnapshot) tea.Cmd {
	m.mutation.upgrade = directUpgradeDisplayState{}
	m.mutation.upgradeResult = nil
	m.mutation.upgradeErr = nil
	operation := m.nextMutationOperation()
	m.mutation.operation = operation
	m.mutation.busy = true
	m.mutation.notice = "Preparing mutation…"
	m.mutation.results = nil
	m.mutation.snapshot = nil
	m.mutation.receiptVisible = false
	m.mutation.etcdKind = request.Kind
	m.mutation.receiptOffset = 0
	m.view.notice = ""
	return directPrepareEtcdCommand(m.session, m.sessionCtx, operation, m.generation(), request, member)
}

func (m *model) nextMutationOperation() uint64 {
	m.mutationSequence++
	return m.mutationSequence
}

func (m model) View() tea.View {
	layout := layoutShell(m.width, m.height)
	headerKind := m.views.top().Kind
	if m.contexts.active {
		headerKind = viewContexts
	}
	metadata := deriveShellMetadata(m.cluster.contextName, m.cluster.contexts, m.options.WritesEnabled, m.cluster.nodes.Status, directHealthInput(m.cluster))
	content := ""
	frame := layoutResourceFrame(layout.Width, layout.ContentHeight, directResourceTitle(m, headerKind))
	innerSize := contentSize{Width: frame.InnerWidth, Height: frame.InnerHeight}
	contentFramed := false
	if m.contexts.active {
		content = renderResourceFrame(frame, m.contexts.viewSized(innerSize), m.styles.k9s)
		contentFramed = true
	} else if m.splash {
		content = "t9s\nInspect Talos clusters"
	} else {
		if diagnosticContent, handled := m.directDiagnosticContent(innerSize); handled {
			content = diagnosticContent
		} else if resourceContent, handled := m.directResourceContent(innerSize); handled {
			content = resourceContent
		} else if m.views.top().Kind == viewNodeDetail {
			content = renderNodeDetail(m.nodes.selectedValue(m.cluster.nodes.Value.Nodes))
		} else if m.views.top().Kind == viewServiceDetail {
			if service, ok := findServiceByKey(m.cluster.services.Value.Services, m.views.top().ServiceKey); ok {
				content = renderServiceDetail(service)
			} else {
				content = "SERVICE DETAIL\nService unavailable"
			}
		} else if m.views.top().Kind == viewHelp {
			content = "HELP\n\n" + renderActionHints(actionHints(viewNodes, m.options.WritesEnabled)) + "\n" + renderActionHints(actionHints(viewNodeDetail, m.options.WritesEnabled))
		} else if m.views.top().Kind == viewServiceLogs {
			content = m.logs.viewSized(innerSize)
		} else if m.views.top().Kind == viewDmesg {
			content = m.dmesg.viewSized(innerSize)
		} else if m.views.top().Kind == viewServices {
			content = m.services.viewSized(innerSize, m.cluster.services.Value,
				m.cluster.services.Status == directLoading, m.cluster.services.Status == directFailed, m.cluster.services.Err)
		} else if m.views.top().Kind == viewEvents {
			content = m.events.viewFollowSized(innerSize, m.cluster.events,
				m.eventFollow.attempted, m.eventFollow.following, m.eventFollow.err)
		} else if m.views.top().Kind == viewClusterHealth {
			content = m.healthcheck.viewSized(innerSize)
		} else if m.views.top().Kind == viewEtcd {
			content = m.etcd.viewSized(innerSize, m.cluster.etcd)
		} else if m.views.top().Kind == viewOverview {
			content = withHealthNotices(renderOverview(directHealthInput(m.cluster)), directHealthNotices(m.cluster))
		} else if m.views.top().Kind == viewProblems {
			notices := directHealthNotices(m.cluster)
			content = withHealthNotices(m.problems.viewSized(contentSize{Width: innerSize.Width, Height: max(0, innerSize.Height-len(notices))}, application.EvaluateObservations(directHealthInput(m.cluster))), notices)
		} else {
			content = m.nodes.viewSized(innerSize, m.cluster.nodes)
		}
	}
	if prompt := m.mutation.prompt; prompt != nil && (prompt.nodeKind == application.ActionReset || prompt.nodeKind == application.ActionWipeDevice) {
		title := "reset"
		if prompt.nodeKind == application.ActionWipeDevice {
			title = "wipe device"
		}
		frame := layoutResourceFrame(layout.Width, layout.ContentHeight, title)
		content = renderResourceFrame(frame, renderDirectMutationOverlay(prompt, frame.InnerHeight), m.styles.k9s)
		contentFramed = true
	} else if prompt := m.mutation.prompt; prompt != nil && prompt.etcd != nil {
		frame := layoutResourceFrame(layout.Width, layout.ContentHeight, "etcd action")
		content = renderResourceFrame(frame, renderDirectEtcdMutationOverlay(prompt, frame.InnerWidth), m.styles.k9s)
		contentFramed = true
	} else if m.mutation.snapshot != nil && m.mutation.receiptVisible && !m.mutation.busy && m.mutation.prompt == nil && m.canShowDirectEtcdReceipt() {
		frame := layoutResourceFrame(layout.Width, layout.ContentHeight, "etcd receipt")
		content = renderResourceFrame(frame, renderDirectEtcdReceiptOverlay(m.mutation, frame.InnerWidth, frame.InnerHeight), m.styles.k9s)
		contentFramed = true
	}
	if !m.splash && !contentFramed {
		content = renderResourceFrame(frame, content, m.styles.k9s)
	}
	flash := m.view.notice
	if flash == "" {
		switch m.cluster.nodes.Status {
		case directLoading:
			flash = "Loading nodes…"
		case directFailed:
			flash = m.cluster.nodes.Err
		case directReady, directPartial:
			if len(m.cluster.nodes.Value.Nodes) == 0 {
				flash = "No nodes"
			}
		}
	}
	writesAvailable := m.options.WritesEnabled && !m.mutation.busy && m.mutation.prompt == nil && m.upgradePrompt == nil
	writesAvailable = writesAvailable && m.etcdPathPrompt == nil
	header := renderK9sHeader(layoutK9sHeader(layout.Width), metadata, directActionHints(headerKind, writesAvailable), m.styles.k9s)
	breadcrumb := m.views.breadcrumb(m.cluster.contextName)
	if m.contexts.active {
		breadcrumb = m.cluster.contextName + " > contexts"
	}
	prompt := ""
	if m.mutation.prompt != nil {
		prompt = renderDirectMutationPrompt(m.mutation.prompt)
	} else if m.upgradePrompt != nil {
		prompt = m.upgradePrompt.view()
	} else if m.etcdPathPrompt != nil {
		prompt = m.etcdPathPrompt.view()
	} else if m.timePrompt != nil {
		prompt = m.timePrompt.view()
	} else if m.palette.active {
		prompt = m.palette.view()
	} else if !m.palette.active && !m.contexts.active && m.mutation.upgrade.Target != "" && m.mutation.notice != "" && !m.directFilterEditing() {
		prompt = m.mutation.notice
	} else if !m.palette.active && m.views.top().Kind == viewNodes && (m.nodes.filtering || m.nodes.filter != "") {
		prompt = "/" + m.nodes.filter
	} else if !m.palette.active && m.views.top().Kind == viewServices && (m.services.filtering || m.services.filter != "") {
		prompt = "/" + m.services.filter
	} else if !m.palette.active && m.views.top().Kind == viewEvents && (m.events.filtering || m.events.filter != "") {
		prompt = "/" + m.events.filter
	} else if !m.palette.active && m.views.top().Kind == viewClusterHealth && (m.healthcheck.filtering || m.healthcheck.filter != "") {
		prompt = "/" + m.healthcheck.filter
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
	} else if !m.palette.active && m.views.top().Kind == viewResourceKinds && (m.resourceKinds.filtering || m.resourceKinds.filter != "") {
		prompt = "/" + m.resourceKinds.filter
	} else if !m.palette.active && m.views.top().Kind == viewResourceInstances && (m.resourceInstances.filtering || m.resourceInstances.filter != "") {
		prompt = "/" + m.resourceInstances.filter
	} else if !m.palette.active && m.views.top().Kind == viewServiceLogs && (m.logs.filtering || m.logs.filter != "") {
		prompt = "/" + m.logs.filter
	} else if !m.palette.active && m.views.top().Kind == viewDmesg && (m.dmesg.filtering || m.dmesg.filter != "") {
		prompt = "/" + m.dmesg.filter
	} else if m.mutation.snapshot != nil && m.mutation.receiptVisible && !m.mutation.busy && m.canShowDirectEtcdReceipt() {
		prompt = "Receipt details · PgUp/PgDn · Esc/q hide · v toggle"
	} else if m.mutation.snapshot != nil && !m.mutation.busy && m.canShowDirectEtcdReceipt() {
		prompt = m.mutation.notice + " · v receipt"
	} else if m.mutation.notice != "" {
		prompt = m.mutation.notice
	} else if m.mutation.busy {
		prompt = "Mutation in progress…"
	}
	footer := renderK9sFooter(layout.Width, breadcrumb, prompt, flash, m.styles.k9s)
	result := tea.NewView(renderShell(layout, header, content, footer))
	result.AltScreen = true
	result.BackgroundColor = color.Black
	result.WindowTitle = "t9s"
	return result
}

func (m model) canShowDirectEtcdReceipt() bool {
	return m.views.top().Kind == viewEtcd && !m.etcd.filtering && !m.palette.active && !m.contexts.active && m.etcdPathPrompt == nil && m.timePrompt == nil
}

func (m model) directFilterEditing() bool {
	switch m.views.top().Kind {
	case viewNodes:
		return m.nodes.filtering
	case viewServices:
		return m.services.filtering
	case viewEvents:
		return m.events.filtering
	case viewClusterHealth:
		return m.healthcheck.filtering
	case viewEtcd:
		return m.etcd.filtering
	case viewProblems:
		return m.problems.filtering
	case viewProcesses:
		return m.processes.filtering
	case viewDisks:
		return m.disks.filtering
	case viewNetwork:
		return m.network.filtering
	case viewNetstat:
		return m.netstat.filtering
	case viewMounts:
		return m.mounts.filtering
	case viewMemory:
		return m.memory.filtering
	case viewCPU:
		return m.cpu.filtering
	case viewTime:
		return m.time.filtering
	case viewResourceKinds:
		return m.resourceKinds.filtering
	case viewResourceInstances:
		return m.resourceInstances.filtering
	case viewServiceLogs:
		return m.logs.filtering
	case viewDmesg:
		return m.dmesg.filtering
	default:
		return false
	}
}

func (m *model) beginSessionOpen(contextName string) tea.Cmd {
	m.requests.cancelSlot("upgrade-image")
	m.requests.cancelSlot("upgrade-prepare")
	m.closeDirectLogs(false)
	m.closeDirectLogs(true)
	m.closeDirectEventFollow()
	m.closeDirectHealth()
	m.pendingResourceKind = ""
	m.mutation.prompt = nil
	m.upgradePrompt = nil
	m.mutation.upgradePending = false
	m.mutation.upgrade = directUpgradeDisplayState{}
	m.mutation.upgradeResult = nil
	m.mutation.upgradeErr = nil
	m.etcdPathPrompt = nil
	m.mutation.busy = false
	m.mutation.results = nil
	m.mutation.snapshot = nil
	m.mutation.receiptVisible = false
	m.mutation.notice = ""
	m.mutation.operation = m.nextMutationOperation()
	m.mutationWork = directMutationWork{}
	m.closeDirectSession()
	m.requests.cancelSlot("session-open")
	m.generationCounter++
	m.cluster.contextName = contextName
	m.cluster.nodes = directNodeState{Status: directLoading}
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
	return actionHints(kind, writesEnabled)
}

func (m model) fallbackCleanup() {
	m.closeDirectLogs(false)
	m.closeDirectLogs(true)
	m.closeDirectEventFollow()
	m.closeDirectHealth()
	m.eventFollow = directEventFollowState{}
	m.healthcheck = newDirectHealthcheckModel(healthcheckDisplayState{})
	if m.rootCancel != nil {
		m.rootCancel()
	}
	if m.sessionRequestCancel != nil {
		m.sessionRequestCancel()
	}
	m.closeDirectSession()
	if session := m.lifecycle.takeSession(); session != nil {
		_ = session.Close()
	}
	m.requests.cancelAll()
}

func (m *model) closeDirectSession() {
	if m.sessionCancel != nil {
		m.sessionCancel()
		m.sessionCancel = nil
	}
	if m.session != nil {
		_ = m.session.Close()
		if m.lifecycle != nil {
			m.lifecycle.setSession(nil)
		}
		m.session = nil
	}
}

func (m model) cleanup(next tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m.fallbackCleanup()
		return next
	}
}

func (m model) generation() uint64 { return m.generationCounter }

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
