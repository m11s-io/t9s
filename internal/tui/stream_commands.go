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

func (m *model) openDirectServiceLogs(service domain.ServiceSnapshot) tea.Cmd {
	target := domain.LogRequest{Node: service.Node, Service: service.Name}
	request, _ := m.requests.beginWithContext("service-logs", m.generation(), m.sessionCtx)
	m.logStream = directLogStreamState{request: request, target: target, status: directLoading}
	m.logs = newDirectServiceLogsModel()
	m.views = m.views.push(viewFrame{Kind: viewServiceLogs, Label: fallback(service.Name) + "@" + fallback(service.Node) + " > logs"})
	m.syncDirectStreamViewports()
	return directOpenServiceLogsCommand(m.session, request, target)
}

func (m *model) openDirectDmesg(node string) tea.Cmd {
	target := domain.DmesgRequest{Node: node, Follow: true, Tail: true}
	request, _ := m.requests.beginWithContext("dmesg", m.generation(), m.sessionCtx)
	m.dmesgStream = directDmesgStreamState{request: request, target: target, status: directLoading}
	m.dmesg = newDirectDmesgModel()
	m.views = m.views.push(viewFrame{Kind: viewDmesg, Label: fallback(node) + " > dmesg"})
	m.syncDirectStreamViewports()
	return directOpenDmesgCommand(m.session, request, target)
}

func (m *model) closeDirectLogs(dmesg bool) {
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

func (m *model) reconnectDirectLogs(dmesg bool) tea.Cmd {
	if dmesg {
		target := m.dmesgStream.target
		m.closeDirectLogs(true)
		request, _ := m.requests.beginWithContext("dmesg", m.generation(), m.sessionCtx)
		m.dmesgStream = directDmesgStreamState{request: request, target: target, status: directLoading}
		m.dmesg = m.dmesg.setStreamState(directLoading, nil, "", false)
		m.syncDirectStreamViewports()
		return directOpenDmesgCommand(m.session, request, target)
	}
	target := m.logStream.target
	m.closeDirectLogs(false)
	request, _ := m.requests.beginWithContext("service-logs", m.generation(), m.sessionCtx)
	m.logStream = directLogStreamState{request: request, target: target, status: directLoading}
	m.logs = m.logs.setStreamState(directLoading, nil, "", false)
	m.syncDirectStreamViewports()
	return directOpenServiceLogsCommand(m.session, request, target)
}

func (m *model) reconnectDirectClusterHealth() tea.Cmd {
	target := pureClusterHealthRequestFromNodes(m.cluster.nodes.Value.Nodes, m.healthStream.target.WaitTimeout)
	m.closeDirectHealth()
	request, _ := m.requests.beginWithContext("cluster-health", m.generation(), m.sessionCtx)
	m.healthStream = directHealthStreamState{request: request, target: target}
	m.healthcheck = newDirectHealthcheckModel(healthcheckDisplayState{Status: directLoading, Request: target})
	m.syncDirectStreamViewports()
	return directOpenClusterHealthCommand(m.session, request, target)
}

func (m *model) syncDirectStreamViewports() {
	layout := m.shellLayout()
	frame := layoutResourceFrame(layout.Width, layout.ContentHeight, directResourceTitle(*m, m.views.top().Kind))
	size := contentSize{Width: frame.InnerWidth, Height: frame.InnerHeight}
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

func (m *model) closeStreamsOutsideView(previous viewStack) {
	if previous.contains(viewServiceLogs) && !m.views.contains(viewServiceLogs) {
		m.closeDirectLogs(false)
	}
	if previous.contains(viewDmesg) && !m.views.contains(viewDmesg) {
		m.closeDirectLogs(true)
	}
	if previous.contains(viewEvents) && !m.views.contains(viewEvents) {
		m.closeDirectEventFollow()
	}
	if previous.contains(viewClusterHealth) && !m.views.contains(viewClusterHealth) {
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

func (m *model) openDirectEventFollow() tea.Cmd {
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

func (m *model) closeDirectEventFollow() {
	m.requests.cancelSlot("cluster-events-follow")
	if m.eventFollow.stream != nil {
		_ = m.eventFollow.stream.Close()
		m.eventFollow.stream = nil
	}
	m.eventFollow.following = false
}

func (m *model) applyDirectEventBatch(batch domain.EventSet, err error) {
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

func (m *model) openDirectClusterHealth(timeout time.Duration) (model, tea.Cmd) {
	if m.session == nil {
		m.view.notice = "Session unavailable"
		return *m, nil
	}
	requestTarget := pureClusterHealthRequestFromNodes(m.cluster.nodes.Value.Nodes, timeout)
	request, _ := m.requests.beginWithContext("cluster-health", m.generation(), m.sessionCtx)
	m.healthStream = directHealthStreamState{request: request, target: requestTarget}
	m.healthcheck = newDirectHealthcheckModel(healthcheckDisplayState{Status: directLoading, Request: requestTarget})
	m.views = m.views.push(viewFrame{Kind: viewClusterHealth, Label: "healthcheck"})
	m.syncDirectStreamViewports()
	return *m, directOpenClusterHealthCommand(m.session, request, requestTarget)
}

func (m *model) closeDirectHealth() {
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

func (m model) updateDirectEventAndHealthResult(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case directEventFollowOpened:
		current := m.requests.current(message.request) && message.request.generation == m.generation() && message.request == m.eventFollow.request.stamp && m.eventFollow.following
		if !current || !m.views.contains(viewEvents) {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			if m.requests.current(message.request) {
				m.requests.cancelSlot("cluster-events-follow")
			}
			return m, nil
		}
		if message.err != nil || message.stream == nil {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			m.eventFollow.following = false
			m.eventFollow.err = "Event stream is unavailable"
			m.cluster.events.Status = directPartial
			m.cluster.events.Err = m.eventFollow.err
			m.requests.cancelSlot("cluster-events-follow")
			return m, nil
		}
		m.eventFollow.stream = message.stream
		return m, directEventFollowNextCommand(message.stream, m.eventFollow.request)
	case directEventFollowBatch:
		if !m.requests.current(message.request) || message.request.generation != m.generation() || message.request != m.eventFollow.request.stamp || !m.eventFollow.following {
			return m, nil
		}
		m.applyDirectEventBatch(message.batch, message.err)
		if message.err != nil || m.eventFollow.err != "" {
			m.eventFollow.following = false
			if m.eventFollow.stream != nil {
				_ = m.eventFollow.stream.Close()
				m.eventFollow.stream = nil
			}
			m.requests.cancelSlot("cluster-events-follow")
			return m, nil
		}
		return m, directEventFollowNextCommand(m.eventFollow.stream, m.eventFollow.request)
	case directClusterHealthOpened:
		current := m.requests.current(message.request) && message.request.generation == m.generation() && message.request == m.healthStream.request.stamp
		if !current || !m.views.contains(viewClusterHealth) {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			if m.requests.current(message.request) {
				m.requests.cancelSlot("cluster-health")
			}
			return m, nil
		}
		if message.err != nil || message.stream == nil {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			m.healthcheck = m.healthcheck.setStreamState(healthcheckDisplayState{Status: directFailed, Request: m.healthStream.target, Err: "Cluster health check is unavailable"})
			m.requests.cancelSlot("cluster-health")
			return m, nil
		}
		m.healthStream.stream = message.stream
		m.healthcheck = m.healthcheck.setStreamState(healthcheckDisplayState{Status: directReady, Request: m.healthStream.target})
		return m, directClusterHealthNextCommand(message.stream, m.healthStream.request)
	case directClusterHealthBatch:
		if !m.requests.current(message.request) || message.request.generation != m.generation() || message.request != m.healthStream.request.stamp {
			return m, nil
		}
		state := m.healthcheck.state
		if message.progress.Message != "" {
			state.Lines = appendBoundedLines(state.Lines, []string{message.progress.Message}, 500)
		}
		if (message.err != nil && !errors.Is(message.err, io.EOF)) || message.progress.Err != "" {
			state.Status = directFailed
			state.Err = "Cluster health check is unavailable"
			if message.progress.Err != "" {
				state.Err = message.progress.Err
			}
			state.VerdictReady = false
			m.healthcheck = m.healthcheck.setStreamState(state)
			m.syncDirectStreamViewports()
			m.closeDirectHealth()
			return m, nil
		}
		if message.progress.EOF {
			state.EOF = true
			state.VerdictReady = true
			state.Status = directReady
			m.healthcheck = m.healthcheck.setStreamState(state)
			m.syncDirectStreamViewports()
			m.closeDirectHealth()
			return m, nil
		}
		m.healthcheck = m.healthcheck.setStreamState(state)
		m.syncDirectStreamViewports()
		if errors.Is(message.err, io.EOF) {
			state.EOF = true
			m.healthcheck = m.healthcheck.setStreamState(state)
			m.closeDirectHealth()
			return m, nil
		}
		return m, directClusterHealthNextCommand(m.healthStream.stream, m.healthStream.request)
	}
	return m, nil
}

func (m model) updateDirectLogsResult(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case directServiceLogsOpened:
		current := m.requests.current(message.request)
		if !current || message.request.generation != m.generation() || !m.views.contains(viewServiceLogs) {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			if current {
				m.requests.cancelSlot("service-logs")
			}
			return m, nil
		}
		if message.err != nil {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			m.logStream.status, m.logStream.err = directFailed, "Service logs are unavailable"
			m.logs = m.logs.setStreamState(directFailed, nil, m.logStream.err, false)
			m.syncDirectStreamViewports()
			m.requests.cancelSlot("service-logs")
			return m, nil
		}
		m.logStream.stream = message.stream
		m.logStream.status = directReady
		return m, directServiceLogNextCommand(message.stream, m.logStream.request)
	case directServiceLogBatch:
		if !m.requests.current(message.request) || message.request.generation != m.generation() || message.request != m.logStream.request.stamp {
			return m, nil
		}
		lines := appendBoundedLines(m.logs.state.Lines, message.batch.Lines, 5000)
		if message.batch.Err != "" {
			m.logStream.status, m.logStream.err = directFailed, message.batch.Err
		}
		if message.err != nil && !errors.Is(message.err, io.EOF) {
			m.logStream.status, m.logStream.err = directFailed, "Service log stream failed"
		}
		m.logStream.eof = message.batch.EOF || errors.Is(message.err, io.EOF)
		m.logs = m.logs.setStreamState(m.logStream.status, lines, m.logStream.err, m.logStream.eof)
		m.syncDirectStreamViewports()
		if !m.logStream.eof && m.logStream.status != directFailed {
			return m, directServiceLogNextCommand(m.logStream.stream, m.logStream.request)
		}
		m.requests.cancelSlot("service-logs")
		return m, nil
	case directDmesgOpened:
		current := m.requests.current(message.request)
		if !current || message.request.generation != m.generation() || !m.views.contains(viewDmesg) {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			if current {
				m.requests.cancelSlot("dmesg")
			}
			return m, nil
		}
		if message.err != nil {
			if message.stream != nil {
				_ = message.stream.Close()
			}
			m.dmesgStream.status, m.dmesgStream.err = directFailed, "Dmesg stream is unavailable"
			m.dmesg = m.dmesg.setStreamState(directFailed, nil, m.dmesgStream.err, false)
			m.syncDirectStreamViewports()
			m.requests.cancelSlot("dmesg")
			return m, nil
		}
		m.dmesgStream.stream = message.stream
		m.dmesgStream.status = directReady
		return m, directDmesgNextCommand(message.stream, m.dmesgStream.request)
	case directDmesgBatch:
		if !m.requests.current(message.request) || message.request.generation != m.generation() || message.request != m.dmesgStream.request.stamp {
			return m, nil
		}
		lines := appendBoundedLines(m.dmesg.state.Lines, message.batch.Lines, 5000)
		if message.batch.Err != "" {
			m.dmesgStream.status, m.dmesgStream.err = directFailed, message.batch.Err
		}
		if message.err != nil && !errors.Is(message.err, io.EOF) {
			m.dmesgStream.status, m.dmesgStream.err = directFailed, "Dmesg stream failed"
		}
		m.dmesgStream.eof = message.batch.EOF || errors.Is(message.err, io.EOF)
		m.dmesg = m.dmesg.setStreamState(m.dmesgStream.status, lines, m.dmesgStream.err, m.dmesgStream.eof)
		m.syncDirectStreamViewports()
		if !m.dmesgStream.eof && m.dmesgStream.status != directFailed {
			return m, directDmesgNextCommand(m.dmesgStream.stream, m.dmesgStream.request)
		}
		m.requests.cancelSlot("dmesg")
		return m, nil
	case directClearLogs:
		if message.dmesg {
			m.dmesgStream.err = ""
			m.dmesg = m.dmesg.setStreamState(m.dmesgStream.status, nil, "", false)
			m.syncDirectStreamViewports()
		} else {
			m.logStream.err = ""
			m.logs = m.logs.setStreamState(m.logStream.status, nil, "", false)
			m.syncDirectStreamViewports()
		}
		return m, nil
	}
	return m, nil
}
