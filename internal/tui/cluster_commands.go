package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type directRunCommand struct{ value string }

// runClusterCommand handles the root-owned cluster navigation aliases while
// each screen is migrated in its own task.
func (m *model) runClusterCommand(value string) tea.Cmd {
	if value == "" {
		return nil
	}
	previous := m.views
	m.mutation.receiptVisible = false
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), ":"))
	var command tea.Cmd
	resolved := resolveCommand(value)
	if previous.top().Kind == viewKubernetesUpgrade && resolved != commandKubernetesUpgrade {
		m.kubernetesUpgrade.close(m.session)
	}
	if resolved != commandUnknown && resolved != commandContexts {
		m.contexts.active = false
	}
	switch resolved {
	case commandNodes:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
	case commandServices:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	case commandEvents:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	case commandEtcd:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	case commandBGPPeers:
		m.views = m.views.push(viewFrame{Kind: viewBGPPeers, Label: "bgp peers"})
	case commandOverview:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewOverview, Label: "overview"})
	case commandProblems:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewProblems, Label: "problems"})
	case commandResources:
		kind, _ := resourcesCommandArgument(value)
		command = m.openDirectResources(kind)
	case commandClusterHealth:
		timeout := defaultClusterHealthWait
		if parsed, ok := healthcheckCommandArgument(value); ok {
			timeout = parsed
		}
		_, command = m.openDirectClusterHealth(timeout)
	case commandConfiguration:
		if !m.options.WritesEnabled || m.session == nil {
			m.view.notice = "Configuration apply requires an open session with --enable-writes"
			break
		}
		m.configuration.discard(m.session)
		m.configuration = newConfigurationModel(m.nodes.actionTargets(m.cluster.nodes.Value.Nodes), m.cluster.nodes.Value.Nodes)
		m.views = m.views.replaceRoot(viewFrame{Kind: viewConfiguration, Label: "configuration"})
	case commandKubernetesUpgrade:
		if m.session == nil {
			m.view.notice = "Open a Talos context before preparing a Kubernetes upgrade"
			break
		}
		m.kubernetesUpgrade.close(m.session)
		m.kubernetesUpgrade = newKubernetesUpgradeModel()
		m.views = m.views.replaceRoot(viewFrame{Kind: viewKubernetesUpgrade, Label: "upgrade-k8s"})
	case commandContexts:
		m.contexts = newContextsModel(m.cluster.contexts, m.cluster.contextName)
	case commandHelp:
		m.views = m.views.push(viewFrame{Kind: viewHelp, Label: "help"})
		return nil
	default:
		m.view.notice = unknownCommandNotice(value)
	}
	m.cancelClosedDirectDiagnostics(previous)
	m.cancelClosedDirectResources(previous)
	m.closeStreamsOutsideView(previous)
	return command
}

func directContextKnown(contexts []domain.ClusterContext, name string) bool {
	for _, clusterContext := range contexts {
		if clusterContext.Name == name {
			return true
		}
	}
	return false
}

func (m model) updateDirectClusterResult(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case directNodeProgress:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		if len(message.nodes.Nodes) > 0 {
			m.splash = false
			m.cluster.nodes = directNodeState{Status: directReady, Value: application.EnrichNodes(message.nodes, m.cluster.kubernetes.Nodes)}
			m.nodes = m.nodes.normalizeSelection(m.cluster.nodes.Value.Nodes, m.nodes.control.Cursor())
		}
		return m, waitForNodeProgress(message.ctx, message.updates)
	case directNodeServicesLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		m.splash = false
		m.requests.cancelSlot("cluster-nodes-services")
		nodeStatus := directNodeLoadStatus(message.nodes)
		state := directNodeState{Status: directReady, Value: message.nodes}
		if nodeStatus == directPartial {
			state.Status = directPartial
			state.Err = "Some nodes report problems"
		}
		serviceStatus := directServiceLoadStatus(message.services)
		serviceState := directServiceState{Status: serviceStatus, Value: message.services}
		if serviceStatus == directPartial {
			serviceState.Err = "Some services report problems"
		}
		if message.err != nil {
			state.Status = directFailed
			state.Err = "Node data is unavailable"
			if nodeStatus == directPartial || len(message.nodes.Nodes) > 0 || !message.nodes.ObservedAt.IsZero() {
				state.Status = directPartial
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
				if m.views.top().Kind == viewNodes {
					node := m.nodes.selectedValue(state.Value.Nodes)
					m.views = m.views.push(viewFrame{Kind: viewNodeDetail, Label: fallback(node.DisplayName())})
				}
			}
			m.options.NodeFocus = ""
		}
		if len(state.Value.Nodes) > 0 && m.session != nil && m.session.SupportsNodeStatus() {
			m.cluster.nodeStatus = directNodeStatusState{Status: directLoading, Value: m.cluster.nodeStatus.Value}
			statusRequest, _ := m.requests.beginWithContext("cluster-node-status", m.generation(), m.sessionCtx)
			return m, directNodeStatusesCommand(m.session, statusRequest, state.Value.Nodes)
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
	case directNodeStatusesLoaded:
		if !m.requests.current(message.request) || message.request.generation != m.generation() {
			return m, nil
		}
		m.requests.cancelSlot("cluster-node-status")
		state := directNodeStatusState{Status: directReady, Value: message.values}
		if len(message.values.Problems) > 0 {
			state.Status = directPartial
			state.Err = "Some Talos status resources are unavailable"
		}
		if message.err != nil && len(message.values.Statuses) == 0 {
			state.Status = directFailed
			state.Err = "Talos status resources are unavailable"
		} else if message.err != nil {
			state.Status = directPartial
			state.Err = "Some Talos status resources are unavailable"
		}
		m.cluster.nodeStatus = state
		return m, nil
	}
	return m, nil
}

func (m *model) refreshDirectSummary() tea.Cmd {
	etcdRequest, _ := m.requests.beginWithContext("cluster-etcd", m.generation(), m.sessionCtx)
	var events tea.Cmd
	if !m.eventFollow.following {
		request, _ := m.requests.beginWithContext("cluster-events", m.generation(), m.sessionCtx)
		events = directEventsCommand(m.session, request)
	}
	return tea.Batch(m.refreshDirectNodes(), directEtcdCommand(m.session, etcdRequest), events)
}
