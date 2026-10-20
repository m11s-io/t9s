package tui

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectServiceDetailLoadsCapturedNodeAndServiceAndRefreshes(t *testing.T) {
	var requested []string
	reader := &testkit.FakeServiceReader{DetailFunc: func(_ context.Context, node, service string) (domain.ServiceDetail, error) {
		requested = append(requested, node+"/"+service)
		return domain.ServiceDetail{Node: node, Service: service, State: "Running", Events: []domain.ServiceEventSnapshot{{
			Time: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), TimeKnown: true, State: "Running", Message: "service started",
		}}}, nil
	}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{ServiceReader: reader}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()

	model := newModel(context.Background(), client, LaunchOptions{})
	model.splash = false
	model.session, model.sessionCtx = session, context.Background()
	service := domain.ServiceSnapshot{Node: "node-a", Name: "kubelet", State: "Running"}
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{service}}}
	model.services = newServicesModel().normalizeSelection(model.cluster.services.Value.Services, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})

	model, command := updateDirect(model, keyPress('d'))
	require.Equal(t, viewServiceDetail, model.views.top().Kind)
	model, _ = applyDirectCommand(t, model, command)
	assert.Contains(t, model.View().Content, "service started")
	assert.Equal(t, []string{"node-a/kubelet"}, requested)

	model, command = updateDirect(model, keyPress('r'))
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, []string{"node-a/kubelet", "node-a/kubelet"}, requested)
}

func TestDirectServiceDetailIgnoresLateResultForPreviousSelection(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	serviceA := domain.ServiceSnapshot{Node: "node-a", Name: "api"}
	serviceB := domain.ServiceSnapshot{Node: "node-b", Name: "worker"}
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{serviceA, serviceB}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServiceDetail, Label: "worker@node-b", ServiceKey: serviceKey(serviceB)})
	model.serviceDetail = newServiceDetailModel(serviceB, true)
	request, _ := model.requests.begin(directServiceDetailSlot(serviceKey(serviceA)), model.generation())

	model, _ = updateDirect(model, directServiceDetailLoaded{request: request.stamp, key: serviceKey(serviceA), value: domain.ServiceDetail{Node: "node-a", Service: "api", State: "Stopped", Events: []domain.ServiceEventSnapshot{{Message: "stale event"}}}})
	assert.NotContains(t, model.View().Content, "stale event")
}

func TestDirectServiceDetailCanScrollToLastEventAtNormalTerminalHeight(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "ready"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			model := newModel(context.Background(), nil, LaunchOptions{})
			model.splash = false
			service := domain.ServiceSnapshot{Node: "node-a", Name: "api", State: "Running"}
			model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{service}}}
			model.views = model.views.replaceRoot(viewFrame{Kind: viewServiceDetail, Label: "api@node-a", ServiceKey: serviceKey(service)})
			events := make([]domain.ServiceEventSnapshot, 50)
			for index := range events {
				events[index] = domain.ServiceEventSnapshot{Message: fmt.Sprintf("event-%02d", index)}
			}
			value := domain.ServiceDetail{Node: service.Node, Service: service.Name, State: service.State, Events: events}
			if partial {
				value.Problems = []string{"some events unavailable"}
			}
			model.serviceDetail = newServiceDetailModel(service, false).setResult(value, nil)
			model, _ = updateDirect(model, tea.WindowSizeMsg{Width: 100, Height: 24})

			for range 60 {
				model, _ = updateDirect(model, keyPress('j'))
			}

			assert.Contains(t, model.View().Content, "event-49")
		})
	}
}

func TestDirectServiceDetailResultSurvivesHelpOverlay(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	service := domain.ServiceSnapshot{Node: "node-a", Name: "api"}
	model.cluster.services = directServiceState{Status: directReady, Value: domain.ServiceSet{Services: []domain.ServiceSnapshot{service}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServiceDetail, Label: "api@node-a", ServiceKey: serviceKey(service)})
	model.serviceDetail = newServiceDetailModel(service, true)
	request, _ := model.requests.begin(directServiceDetailSlot(serviceKey(service)), model.generation())
	model, _ = updateDirect(model, keyPress('?'))

	model, _ = updateDirect(model, directServiceDetailLoaded{request: request.stamp, key: serviceKey(service), value: domain.ServiceDetail{
		Node: service.Node, Service: service.Name, State: "Running", Events: []domain.ServiceEventSnapshot{{Message: "result arrived under help"}},
	}})
	model, _ = updateDirect(model, keyPress('q'))

	assert.Contains(t, model.View().Content, "result arrived under help")
}

func TestDirectServiceDetailCommandNavigationCancelsRead(t *testing.T) {
	model := newModel(context.Background(), nil, LaunchOptions{})
	service := domain.ServiceSnapshot{Node: "node-a", Name: "api"}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewServiceDetail, Label: "api@node-a", ServiceKey: serviceKey(service)})
	request, _ := model.requests.beginWithContext(directServiceDetailSlot(serviceKey(service)), model.generation(), context.Background())

	model.runClusterCommand("services")

	assert.ErrorIs(t, request.ctx.Err(), context.Canceled)
}
