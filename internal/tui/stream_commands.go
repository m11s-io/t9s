package tui

import (
	"context"

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
	return directOpenServiceLogsCommand(m.session, request, target)
}

func (m *directModel) openDirectDmesg(node string) tea.Cmd {
	target := domain.DmesgRequest{Node: node, Follow: true, Tail: true}
	request, _ := m.requests.beginWithContext("dmesg", m.generation(), m.sessionCtx)
	m.dmesgStream = directDmesgStreamState{request: request, target: target, status: application.Loading}
	m.dmesg = newDirectDmesgModel()
	m.views = m.views.push(viewFrame{Kind: viewDmesg, Label: fallback(node) + " > dmesg"})
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
		return directOpenDmesgCommand(m.session, request, target)
	}
	target := m.logStream.target
	m.closeDirectLogs(false)
	request, _ := m.requests.beginWithContext("service-logs", m.generation(), m.sessionCtx)
	m.logStream = directLogStreamState{request: request, target: target, status: application.Loading}
	m.logs = m.logs.setStreamState(application.Loading, nil, "", false)
	return directOpenServiceLogsCommand(m.session, request, target)
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
