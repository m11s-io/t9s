package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

type directServiceDetailLoaded struct {
	request directRequestStamp
	key     string
	value   domain.ServiceDetail
	err     error
}

type serviceDetailModel struct {
	status  directLoadStatus
	summary domain.ServiceSnapshot
	value   domain.ServiceDetail
	err     string
	scroll  int
}

func newServiceDetailModel(summary domain.ServiceSnapshot, loading bool) serviceDetailModel {
	status := directReady
	if loading {
		status = directLoading
	}
	return serviceDetailModel{status: status, summary: summary}
}

func (m serviceDetailModel) setResult(value domain.ServiceDetail, err error) serviceDetailModel {
	m.value = value
	m.err = ""
	m.status = directReady
	if err != nil {
		m.err = "Service detail is unavailable: " + sanitizeUntrustedText(err.Error())
		if value.Service != "" || len(value.Events) > 0 || value.State != "" {
			m.status = directPartial
		} else {
			m.status = directFailed
		}
	} else if len(value.Problems) > 0 {
		m.status, m.err = directPartial, sanitizeUntrustedText(strings.Join(value.Problems, "; "))
	}
	return m
}

func (m serviceDetailModel) update(message tea.KeyPressMsg, viewportHeight int) serviceDetailModel {
	capacity := max(1, m.eventCapacity(viewportHeight))
	switch message.String() {
	case "up", "k":
		m.scroll = max(0, m.scroll-1)
	case "down", "j":
		m.scroll = min(max(0, len(m.value.Events)-capacity), m.scroll+1)
	}
	switch message.Code {
	case tea.KeyPgUp:
		m.scroll = max(0, m.scroll-capacity)
	case tea.KeyPgDown:
		m.scroll = min(max(0, len(m.value.Events)-capacity), m.scroll+capacity)
	}
	return m
}

func (m serviceDetailModel) eventCapacity(height int) int {
	fixedRows := 7
	if m.status == directPartial {
		fixedRows++
	}
	return max(0, height-fixedRows)
}

func (m serviceDetailModel) viewSized(size contentSize) string {
	if m.summary.Name == "" {
		return "Service unavailable"
	}
	if m.status == directLoading {
		return renderServiceDetail(m.summary) + "\n\nLoading service event history…"
	}
	if m.status == directFailed {
		return "Service detail unavailable: " + fallback(m.err)
	}
	value := m.value
	if value.Service == "" {
		value = domain.ServiceDetail{Node: m.summary.Node, Service: m.summary.Name, State: m.summary.State, Healthy: m.summary.Healthy}
	}
	content := renderDetail([]detailField{
		{label: "NODE", value: fallback(value.Node)},
		{label: "SERVICE", value: fallback(value.Service)},
		{label: "STATE", value: fallback(value.State)},
		{label: "HEALTH", value: serviceHealthSymbol(value.Healthy)},
	})
	capacity := m.eventCapacity(size.Height)
	start := min(max(0, m.scroll), max(0, len(value.Events)-capacity))
	end := min(len(value.Events), start+capacity)
	var events strings.Builder
	events.WriteString("EVENT HISTORY")
	if len(value.Events) == 0 {
		events.WriteString("\nNo events reported")
	} else {
		for _, event := range value.Events[start:end] {
			events.WriteByte('\n')
			timestamp := "unknown"
			if event.TimeKnown {
				timestamp = event.Time.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
			}
			events.WriteString(timestamp + "  " + fallback(event.State) + "  " + sanitizeUntrustedText(event.Message))
		}
	}
	if m.status == directPartial {
		return "DETAIL PARTIAL: " + fallback(m.err) + "\n" + content + "\n\n" + events.String()
	}
	return content + "\n\n" + events.String()
}

func directServiceDetailSlot(key string) string { return "service-detail:" + key }

func directServiceIdentity(frame viewFrame) (string, string) {
	node, service := frame.NodeTarget, frame.ServiceName
	if node == "" || service == "" {
		parts := strings.SplitN(frame.ServiceKey, "\x00", 2)
		if len(parts) == 2 {
			if node == "" {
				node = parts[0]
			}
			if service == "" {
				service = parts[1]
			}
		}
	}
	return node, service
}

func (m *model) openDirectServiceDetail(service domain.ServiceSnapshot) tea.Cmd {
	key := serviceKey(service)
	frame := viewFrame{Kind: viewServiceDetail, Label: fallback(service.Name) + "@" + fallback(service.Node), ServiceKey: key, NodeID: service.Node, NodeTarget: service.Node, ServiceName: service.Name}
	m.views = m.views.push(frame)
	m.serviceDetail = newServiceDetailModel(service, m.session != nil)
	if m.session == nil {
		return nil
	}
	request, _ := m.requests.beginWithContext(directServiceDetailSlot(key), m.generation(), m.sessionCtx)
	return directServiceDetailCommand(m.session, request, key, service.Node, service.Name)
}

func directServiceDetailCommand(session *application.Session, request directRequest, key, node, service string) tea.Cmd {
	return directCommand(request.ctx, func(ctx context.Context) tea.Msg {
		value, err := session.ServiceDetail(ctx, node, service)
		return directServiceDetailLoaded{request: request.stamp, key: key, value: value, err: err}
	})
}

func (m *model) refreshDirectServiceDetail(frame viewFrame) tea.Cmd {
	node, service := directServiceIdentity(frame)
	if node == "" || service == "" {
		m.serviceDetail.status, m.serviceDetail.err = directFailed, "Service identity is unavailable"
		return nil
	}
	request, _ := m.requests.beginWithContext(directServiceDetailSlot(frame.ServiceKey), m.generation(), m.sessionCtx)
	m.serviceDetail.status, m.serviceDetail.err = directLoading, ""
	return directServiceDetailCommand(m.session, request, frame.ServiceKey, node, service)
}

func (m *model) applyDirectServiceDetail(message directServiceDetailLoaded) {
	if !m.requests.current(message.request) || message.request.generation != m.generation() || !m.directServiceDetailOpen(message.key) {
		return
	}
	m.requests.cancelSlot(directServiceDetailSlot(message.key))
	m.serviceDetail = m.serviceDetail.setResult(message.value, message.err)
}

func (m model) directServiceDetailOpen(key string) bool {
	for _, frame := range m.views {
		if frame.Kind == viewServiceDetail && frame.ServiceKey == key {
			return true
		}
	}
	return false
}

func formatServiceEvent(event domain.ServiceEventSnapshot) string {
	if !event.TimeKnown {
		return fmt.Sprintf("unknown %s %s", event.State, sanitizeUntrustedText(event.Message))
	}
	return fmt.Sprintf("%s %s %s", event.Time.UTC().Format("2006-01-02T15:04:05Z07:00"), event.State, sanitizeUntrustedText(event.Message))
}
