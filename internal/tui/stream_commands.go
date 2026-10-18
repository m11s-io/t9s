package tui

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func (m *directModel) openDirectServiceLogs(service domain.ServiceSnapshot) tea.Cmd {
	target := domain.LogRequest{Node: service.Node, Service: service.Name}
	request, _ := m.requests.beginWithContext("service-logs", m.generation(), m.sessionCtx)
	m.logStream = directLogStreamState{request: request, target: target, status: application.Loading}
	m.logs = newDirectServiceLogsModel()
	m.views = m.views.push(viewFrame{Kind: viewServiceLogs, Label: fallback(service.Name) + "@" + fallback(service.Node) + " > logs"})
	m.syncDirectStreamViewports()
	return directOpenServiceLogsCommand(m.session, request, target)
}

func (m *directModel) openDirectDmesg(node string) tea.Cmd {
	target := domain.DmesgRequest{Node: node, Follow: true, Tail: true}
	request, _ := m.requests.beginWithContext("dmesg", m.generation(), m.sessionCtx)
	m.dmesgStream = directDmesgStreamState{request: request, target: target, status: application.Loading}
	m.dmesg = newDirectDmesgModel()
	m.views = m.views.push(viewFrame{Kind: viewDmesg, Label: fallback(node) + " > dmesg"})
	m.syncDirectStreamViewports()
	return directOpenDmesgCommand(m.session, request, target)
}

func (m *directModel) closeDirectLogs(dmesg bool) {
	if dmesg {
		m.requests.cancelSlot("dmesg")
		if m.dmesgStream.stream != nil {
			_ = m.dmesgStream.stream.Close()
			m.dmesgStream.stream = nil
		}
	} else {
		m.requests.cancelSlot("service-logs")
		if m.logStream.stream != nil {
			_ = m.logStream.stream.Close()
			m.logStream.stream = nil
		}
	}
}

func (m *directModel) reconnectDirectLogs(dmesg bool) tea.Cmd {
	if dmesg {
		target := m.dmesgStream.target
		m.closeDirectLogs(true)
		request, _ := m.requests.beginWithContext("dmesg", m.generation(), m.sessionCtx)
		m.dmesgStream = directDmesgStreamState{request: request, target: target, status: application.Loading}
		m.dmesg = m.dmesg.setStreamState(application.Loading, nil, "", false)
		m.syncDirectStreamViewports()
		return directOpenDmesgCommand(m.session, request, target)
	}
	target := m.logStream.target
	m.closeDirectLogs(false)
	request, _ := m.requests.beginWithContext("service-logs", m.generation(), m.sessionCtx)
	m.logStream = directLogStreamState{request: request, target: target, status: application.Loading}
	m.logs = m.logs.setStreamState(application.Loading, nil, "", false)
	m.syncDirectStreamViewports()
	return directOpenServiceLogsCommand(m.session, request, target)
}

func (m *directModel) reconnectDirectClusterHealth() tea.Cmd {
	target := pureClusterHealthRequestFromNodes(m.cluster.nodes.Value.Nodes, m.healthStream.target.WaitTimeout)
	m.closeDirectHealth()
	request, _ := m.requests.beginWithContext("cluster-health", m.generation(), m.sessionCtx)
	m.healthStream = directHealthStreamState{request: request, target: target}
	m.healthcheck = newDirectHealthcheckModel(healthcheckDisplayState{Status: application.Loading, Request: target})
	m.syncDirectStreamViewports()
	return directOpenClusterHealthCommand(m.session, request, target)
}

func (m *directModel) syncDirectStreamViewports() {
	layout := layoutShell(m.width, m.height)
	size := contentSize{Width: layout.Width, Height: layout.ContentHeight}
	if m.views.top().Kind == viewServiceLogs {
		m.logs = m.logs.syncViewport(size)
	}
	if m.views.top().Kind == viewDmesg {
		m.dmesg = m.dmesg.syncViewport(size)
	}
	if m.views.top().Kind == viewClusterHealth {
		m.healthcheck = m.healthcheck.syncViewport(size)
	}
}

func appendBoundedLines(current, batch []string, limit int) []string {
	if len(batch) >= limit {
		return append([]string(nil), batch[len(batch)-limit:]...)
	}
	result := append(append([]string(nil), current...), batch...)
	if len(result) > limit {
		result = append([]string(nil), result[len(result)-limit:]...)
	}
	return result
}

func (m *directModel) closeStreamsOutsideView(previous viewStack) {
	if previous.top().Kind == viewServiceLogs && m.views.top().Kind != viewServiceLogs {
		m.closeDirectLogs(false)
	}
	if previous.top().Kind == viewDmesg && m.views.top().Kind != viewDmesg {
		m.closeDirectLogs(true)
	}
	if previous.top().Kind == viewEvents && m.views.top().Kind != viewEvents {
		m.closeDirectEventFollow()
	}
	if previous.top().Kind == viewClusterHealth && m.views.top().Kind != viewClusterHealth {
		m.closeDirectHealth()
	}
}

type directServiceLogsOpened struct {
	request directRequestStamp
	stream  ports.ServiceLogStream
	err     error
}

type directServiceLogBatch struct {
	request directRequestStamp
	batch   domain.LogBatch
	err     error
}

type directDmesgOpened struct {
	request directRequestStamp
	stream  ports.DmesgStream
	err     error
}

type directDmesgBatch struct {
	request directRequestStamp
	batch   domain.DmesgBatch
	err     error
}

func directOpenServiceLogsCommand(session *application.Session, request directRequest, target domain.LogRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		stream, err := session.OpenServiceLogs(ctx, target)
		return directServiceLogsOpened{request: request.stamp, stream: stream, err: err}
	})
}

func directServiceLogNextCommand(stream ports.ServiceLogStream, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		batch, err := stream.Next(ctx)
		return directServiceLogBatch{request: request.stamp, batch: batch, err: err}
	})
}

func directOpenDmesgCommand(session *application.Session, request directRequest, target domain.DmesgRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		stream, err := session.OpenDmesg(ctx, target)
		return directDmesgOpened{request: request.stamp, stream: stream, err: err}
	})
}

func directDmesgNextCommand(stream ports.DmesgStream, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		batch, err := stream.Next(ctx)
		return directDmesgBatch{request: request.stamp, batch: batch, err: err}
	})
}

func (m *directModel) openDirectEventFollow() tea.Cmd {
	m.requests.cancelSlot("cluster-events")
	request, _ := m.requests.beginWithContext("cluster-events-follow", m.generation(), m.sessionCtx)
	m.eventFollow = directEventFollowState{request: request, attempted: true, following: true}
	m.cluster.events.Value.Events = appendBoundedEvents(nil, m.cluster.events.Value.Events, 2000)
	m.cluster.events.Value.Problems = nil
	m.cluster.events.Status = directReady
	m.cluster.events.Err = ""
	m.events = m.events.normalizeSelection(eventRows(m.cluster.events.Value), len(eventRows(m.cluster.events.Value))-1)
	return directFollowEventsCommand(m.session, request)
}

func (m *directModel) closeDirectEventFollow() {
	m.requests.cancelSlot("cluster-events-follow")
	if m.eventFollow.stream != nil {
		_ = m.eventFollow.stream.Close()
		m.eventFollow.stream = nil
	}
	m.eventFollow.following = false
}

func (m *directModel) applyDirectEventBatch(batch domain.EventSet, err error) {
	m.cluster.events.Value.Events = appendBoundedEvents(m.cluster.events.Value.Events, batch.Events, 2000)
	for _, problem := range batch.Problems {
		found := false
		for _, old := range m.cluster.events.Value.Problems {
			if old.Node == problem.Node {
				found = true
				break
			}
		}
		if !found {
			m.cluster.events.Value.Problems = append(m.cluster.events.Value.Problems, problem)
		}
	}
	if len(m.cluster.events.Value.Problems) > 0 {
		m.cluster.events.Status = directPartial
		m.cluster.events.Err = "Some event streams are unavailable"
	}
	if err != nil && !errors.Is(err, io.EOF) {
		m.eventFollow.err = "Event stream is unavailable"
		m.cluster.events.Status = directPartial
		m.cluster.events.Err = m.eventFollow.err
	}
	rows := eventRows(m.cluster.events.Value)
	closest := m.events.control.Cursor()
	if m.eventFollow.following {
		m.events.selectedKey = ""
		closest = len(rows) - 1
	}
	m.events = m.events.normalizeSelection(rows, closest)
}

func appendBoundedEvents(current, batch []domain.EventSnapshot, limit int) []domain.EventSnapshot {
	if len(batch) >= limit {
		return append([]domain.EventSnapshot(nil), batch[len(batch)-limit:]...)
	}
	result := append(append([]domain.EventSnapshot(nil), current...), batch...)
	if len(result) > limit {
		result = append([]domain.EventSnapshot(nil), result[len(result)-limit:]...)
	}
	return result
}

func (m *directModel) openDirectClusterHealth(timeout time.Duration) (directModel, tea.Cmd) {
	if m.session == nil {
		m.view.notice = "Session unavailable"
		return *m, nil
	}
	requestTarget := pureClusterHealthRequestFromNodes(m.cluster.nodes.Value.Nodes, timeout)
	request, _ := m.requests.beginWithContext("cluster-health", m.generation(), m.sessionCtx)
	m.healthStream = directHealthStreamState{request: request, target: requestTarget}
	m.healthcheck = newDirectHealthcheckModel(healthcheckDisplayState{Status: application.Loading, Request: requestTarget})
	m.views = m.views.push(viewFrame{Kind: viewClusterHealth, Label: "healthcheck"})
	m.syncDirectStreamViewports()
	return *m, directOpenClusterHealthCommand(m.session, request, requestTarget)
}

func (m *directModel) closeDirectHealth() {
	m.requests.cancelSlot("cluster-health")
	if m.healthStream.stream != nil {
		_ = m.healthStream.stream.Close()
		m.healthStream.stream = nil
	}
}

func pureClusterHealthRequestFromNodes(nodes []domain.NodeSnapshot, timeout time.Duration) domain.ClusterHealthRequest {
	var controlPlane, workers []string
	for _, node := range nodes {
		target := &workers
		switch node.Role {
		case domain.NodeRoleControl:
			target = &controlPlane
		case domain.NodeRoleWorker:
		default:
			continue
		}
		added := false
		for _, address := range node.Addresses {
			if _, err := netip.ParseAddr(address); err == nil {
				*target = append(*target, address)
				added = true
				break
			}
		}
		if !added && node.Name != "" {
			if _, err := netip.ParseAddr(node.Name); err == nil {
				*target = append(*target, node.Name)
			}
		}
	}
	return domain.ClusterHealthRequest{ControlPlaneNodes: controlPlane, WorkerNodes: workers, WaitTimeout: timeout}
}

func directFollowEventsCommand(session *application.Session, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		stream, err := session.FollowEvents(ctx)
		return directEventFollowOpened{request: request.stamp, stream: stream, err: err}
	})
}

func directEventFollowNextCommand(stream ports.EventStream, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		batch, err := stream.Next(ctx)
		return directEventFollowBatch{request: request.stamp, batch: batch, err: err}
	})
}

func directOpenClusterHealthCommand(session *application.Session, request directRequest, target domain.ClusterHealthRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		stream, err := session.OpenClusterHealth(ctx, target)
		return directClusterHealthOpened{request: request.stamp, stream: stream, err: err}
	})
}

func directClusterHealthNextCommand(stream ports.ClusterHealthStream, request directRequest) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		progress, err := stream.Next(ctx)
		return directClusterHealthBatch{request: request.stamp, progress: progress, err: err}
	})
}
