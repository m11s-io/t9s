package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type directResourceKindsLoaded struct {
	request directRequestStamp
	values  domain.ResourceKindSet
	err     error
}
type directResourceInstancesLoaded struct {
	request            directRequestStamp
	nodeID, node, kind string
	values             domain.ResourceInstanceSet
	err                error
}
type directResourceDetailLoaded struct {
	request                           directRequestStamp
	nodeID, node, kind, namespace, id string
	value                             domain.ResourceInstanceSnapshot
	err                               error
}

func directResourceKindsCommand(session *application.Session, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.ResourceKinds(ctx)
		return directResourceKindsLoaded{request: request.stamp, values: values, err: err}
	})
}
func directResourceInstancesCommand(session *application.Session, request directRequest, nodeID, node, kind string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		values, err := session.Resources(ctx, node, kind)
		return directResourceInstancesLoaded{request: request.stamp, nodeID: nodeID, node: node, kind: kind, values: values, err: err}
	})
}
func directResourceDetailCommand(session *application.Session, request directRequest, nodeID, node, kind, namespace, id string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		value, err := session.Resource(ctx, node, kind, id)
		return directResourceDetailLoaded{request: request.stamp, nodeID: nodeID, node: node, kind: kind, namespace: namespace, id: id, value: value, err: err}
	})
}

func (m *directModel) applyResourceResult(message tea.Msg) tea.Cmd {
	switch result := message.(type) {
	case directResourceKindsLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || m.views.top().Kind != viewResourceKinds {
			return nil
		}
		m.requests.cancelSlot("resources:kinds")
		status, errText := directReady, ""
		if result.err != nil {
			errText = "Resource kinds are unavailable"
			if len(result.values.Kinds) > 0 {
				status = directPartial
			} else if len(m.resourceKinds.values.Kinds) > 0 {
				status = directPartial
				result.values = m.resourceKinds.values
			} else {
				status = directFailed
			}
		}
		m.resourceKinds = m.resourceKinds.setKinds(result.values, status, errText)
		if m.pendingResourceKind != "" {
			pending := m.pendingResourceKind
			m.pendingResourceKind = ""
			if result.err == nil {
				if kind, ok := findResourceKind(result.values.Kinds, pending); ok {
					return m.openDirectResourceInstances(kind)
				} else {
					m.view.notice = "Resource kind is unavailable"
				}
			}
		}
	case directResourceInstancesLoaded:
		if !m.requests.current(result.request) || result.request.generation != m.generation() || !m.directResourceInstancesOpen(result.nodeID, result.kind) {
			return nil
		}
		m.requests.cancelSlot(directResourceInstanceSlot(result.nodeID, result.kind))
		status, errText := directReady, ""
		if result.err != nil {
			errText = "Resource instances are unavailable"
			if len(result.values.Instances) > 0 {
				status = directPartial
			} else if len(m.resourceInstances.values.Instances) > 0 {
				status = directPartial
				result.values = m.resourceInstances.values
			} else {
				status = directFailed
			}
		}
		m.resourceInstances = m.resourceInstances.setInstances(result.values, status, errText)
	case directResourceDetailLoaded:
		frame := m.views.top()
		if !m.requests.current(result.request) || result.request.generation != m.generation() || frame.Kind != viewResourceDetail || frame.NodeID != result.nodeID || frame.ResourceKind != result.kind || frame.ResourceNamespace != result.namespace || frame.ResourceID != result.id {
			return nil
		}
		m.requests.cancelSlot(directResourceDetailSlot(result.nodeID, result.kind, result.namespace, result.id))
		status, errText := directReady, ""
		if result.err != nil {
			errText = "Resource detail is unavailable"
			if result.value.YAML != "" {
				status = directPartial
			} else if m.resourceDetail.value.YAML != "" {
				status = directPartial
				result.value = m.resourceDetail.value
			} else {
				status = directFailed
			}
		}
		sensitive := resourceKindSensitive(m.resourceKinds.values.Kinds, result.kind)
		m.resourceDetail = m.resourceDetail.setResult(result.value, status, errText, sensitive)
	}
	return nil
}

func directResourceInstanceSlot(nodeID, kind string) string {
	return "resources:instances:" + nodeID + ":" + kind
}
func directResourceDetailSlot(nodeID, kind, namespace, id string) string {
	return "resources:detail:" + nodeID + ":" + kind + ":" + namespace + ":" + id
}

func (m directModel) directResourceInstancesOpen(nodeID, kind string) bool {
	frame := m.views.top()
	if frame.Kind == viewResourceInstances {
		return frame.NodeID == nodeID && frame.ResourceKind == kind
	}
	if frame.Kind == viewResourceDetail && frame.NodeID == nodeID && frame.ResourceKind == kind && len(m.views) > 1 {
		parent := m.views[len(m.views)-2]
		return parent.Kind == viewResourceInstances && parent.NodeID == nodeID && parent.ResourceKind == kind
	}
	return false
}

func findResourceKind(values []domain.ResourceKindSnapshot, query string) (domain.ResourceKindSnapshot, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	for _, value := range values {
		if strings.ToLower(value.Type) == query || strings.ToLower(value.DisplayType) == query {
			return value, true
		}
		for _, alias := range value.Aliases {
			if strings.ToLower(alias) == query {
				return value, true
			}
		}
	}
	return domain.ResourceKindSnapshot{}, false
}
func resourceKindSensitive(values []domain.ResourceKindSnapshot, kind string) bool {
	for _, value := range values {
		if value.Type == kind {
			return value.Sensitive
		}
	}
	return false
}

func (m *directModel) openDirectResources(kind string) tea.Cmd {
	if m.session == nil {
		m.view.notice = "Session unavailable"
		return nil
	}
	m.views = m.views.push(viewFrame{Kind: viewResourceKinds, Label: "resources"})
	m.resourceKinds = newResourceKindsModel(application.ResourceBrowserState{})
	if kind != "" {
		m.pendingResourceKind = kind
	}
	request, _ := m.requests.beginWithContext("resources:kinds", m.generation(), m.sessionCtx)
	return directResourceKindsCommand(m.session, request)
}

func (m *directModel) openDirectResourceInstances(kind domain.ResourceKindSnapshot) tea.Cmd {
	node, ok := m.nodes.selected(m.cluster.nodes.Value.Nodes)
	if !ok {
		m.view.notice = "Select a node to list resources"
		return nil
	}
	nodeID := directDiagnosticNodeID(node)
	frame := viewFrame{Kind: viewResourceInstances, Label: kind.DisplayType, NodeID: nodeID, NodeTarget: node.Target(), ResourceKind: kind.Type}
	m.views = m.views.push(frame)
	m.resourceInstances = newResourceInstancesModel(application.ResourceBrowserState{})
	request, _ := m.requests.beginWithContext(directResourceInstanceSlot(nodeID, kind.Type), m.generation(), m.sessionCtx)
	return directResourceInstancesCommand(m.session, request, nodeID, node.Target(), kind.Type)
}

func (m *directModel) refreshDirectResources(frame viewFrame) tea.Cmd {
	switch frame.Kind {
	case viewResourceKinds:
		request, _ := m.requests.beginWithContext("resources:kinds", m.generation(), m.sessionCtx)
		m.resourceKinds.status, m.resourceKinds.err = directLoading, ""
		return directResourceKindsCommand(m.session, request)
	case viewResourceInstances:
		request, _ := m.requests.beginWithContext(directResourceInstanceSlot(frame.NodeID, frame.ResourceKind), m.generation(), m.sessionCtx)
		m.resourceInstances.status, m.resourceInstances.err = directLoading, ""
		return directResourceInstancesCommand(m.session, request, frame.NodeID, frame.NodeTarget, frame.ResourceKind)
	case viewResourceDetail:
		request, _ := m.requests.beginWithContext(directResourceDetailSlot(frame.NodeID, frame.ResourceKind, frame.ResourceNamespace, frame.ResourceID), m.generation(), m.sessionCtx)
		m.resourceDetail.status, m.resourceDetail.err = directLoading, ""
		return directResourceDetailCommand(m.session, request, frame.NodeID, frame.NodeTarget, frame.ResourceKind, frame.ResourceNamespace, frame.ResourceID)
	}
	return nil
}

func (m *directModel) handleResourceKey(message tea.KeyPressMsg) (tea.Cmd, bool) {
	frame, key := m.views.top(), message.String()
	switch frame.Kind {
	case viewResourceKinds:
		if !m.resourceKinds.filtering && (key == "q" || key == "esc") {
			m.popDirectResourceView()
			return nil, true
		}
		if key == "r" && !m.resourceKinds.filtering {
			return m.refreshDirectResources(frame), true
		}
		if (key == "enter" || key == "d") && !m.resourceKinds.filtering {
			if kind, ok := m.resourceKinds.selected(); ok {
				return m.openDirectResourceInstances(kind), true
			}
			return nil, true
		}
		m.resourceKinds = m.resourceKinds.update(message)
		return nil, true
	case viewResourceInstances:
		if !m.resourceInstances.filtering && (key == "q" || key == "esc") {
			m.popDirectResourceView()
			return nil, true
		}
		if key == "r" && !m.resourceInstances.filtering {
			return m.refreshDirectResources(frame), true
		}
		if (key == "enter" || key == "d") && !m.resourceInstances.filtering {
			if item, ok := m.resourceInstances.selected(); ok {
				m.views = m.views.push(viewFrame{Kind: viewResourceDetail, Label: item.ID, NodeID: frame.NodeID, NodeTarget: frame.NodeTarget, ResourceKind: frame.ResourceKind, ResourceNamespace: item.Namespace, ResourceID: item.ID})
				m.resourceDetail = newResourceDetailModel()
				request, _ := m.requests.beginWithContext(directResourceDetailSlot(frame.NodeID, frame.ResourceKind, item.Namespace, item.ID), m.generation(), m.sessionCtx)
				return directResourceDetailCommand(m.session, request, frame.NodeID, frame.NodeTarget, frame.ResourceKind, item.Namespace, item.ID), true
			}
			return nil, true
		}
		m.resourceInstances = m.resourceInstances.update(message)
		return nil, true
	case viewResourceDetail:
		if key == "q" || key == "esc" {
			m.popDirectResourceView()
			return nil, true
		}
		if key == "r" {
			return m.refreshDirectResources(frame), true
		}
		m.resourceDetail = m.resourceDetail.update(message)
		return nil, true
	}
	return nil, false
}

func (m *directModel) popDirectResourceView() {
	frame := m.views.top()
	if len(m.views) <= 1 {
		return
	}
	m.views, _ = m.views.pop()
	switch frame.Kind {
	case viewResourceKinds:
		m.requests.cancelSlot("resources:kinds")
	case viewResourceInstances:
		m.requests.cancelSlot(directResourceInstanceSlot(frame.NodeID, frame.ResourceKind))
	case viewResourceDetail:
		m.requests.cancelSlot(directResourceDetailSlot(frame.NodeID, frame.ResourceKind, frame.ResourceNamespace, frame.ResourceID))
	}
}

func (m *directModel) cancelClosedDirectResources(previous viewStack) {
	for _, frame := range previous {
		if frame.Kind != viewResourceKinds && frame.Kind != viewResourceInstances && frame.Kind != viewResourceDetail {
			continue
		}
		stillOpen := false
		for _, current := range m.views {
			if current.Kind == frame.Kind && current.NodeID == frame.NodeID && current.ResourceKind == frame.ResourceKind && current.ResourceNamespace == frame.ResourceNamespace && current.ResourceID == frame.ResourceID {
				stillOpen = true
				break
			}
		}
		if stillOpen {
			continue
		}
		switch frame.Kind {
		case viewResourceKinds:
			m.requests.cancelSlot("resources:kinds")
		case viewResourceInstances:
			m.requests.cancelSlot(directResourceInstanceSlot(frame.NodeID, frame.ResourceKind))
		case viewResourceDetail:
			m.requests.cancelSlot(directResourceDetailSlot(frame.NodeID, frame.ResourceKind, frame.ResourceNamespace, frame.ResourceID))
		}
	}
}

func (m directModel) directResourceContent(size contentSize) (string, bool) {
	switch m.views.top().Kind {
	case viewResourceKinds:
		return m.resourceKinds.viewSized(size), true
	case viewResourceInstances:
		return m.resourceInstances.viewSized(size), true
	case viewResourceDetail:
		return m.resourceDetail.directViewSized(size), true
	default:
		return "", false
	}
}
