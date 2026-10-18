package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectCPUKeySamplesAndRefreshesThroughSession(t *testing.T) {
	reads := 0
	var target string
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{CPUReader: &testkit.FakeCPUReader{SampleFunc: func(_ context.Context, node string) (domain.CPUSnapshot, error) {
			target = node
			reads++
			return domain.CPUSnapshot{Aggregate: domain.CPUUtilization{Known: true, BusyPercent: float64(reads * 10)}}, nil
		}}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()

	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "worker-id", Name: "worker-1"}})
	model.session, model.sessionCtx = session, context.Background()
	model, command := updateDirect(model, keyPress('u'))
	require.Equal(t, viewCPU, model.views.top().Kind)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, "worker-1", target)
	assert.Contains(t, model.View().Content, "10.0% busy")

	model, command = updateDirect(model, keyPress('r'))
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, 2, reads)
	assert.Contains(t, model.View().Content, "20.0% busy")
}

func TestDirectCPUFailureAndCancellationUseSafePresentation(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.push(viewFrame{Kind: viewCPU, NodeID: "node-id", NodeTarget: "node"})
	request, _ := model.requests.begin("node:node-id:cpu", model.generation())
	model, _ = updateDirect(model, directCPULoaded{request: request.stamp, nodeID: "node-id", node: "node", err: errors.New("rpc secret details")})
	assert.Contains(t, model.View().Content, "CPU sample unavailable")
	assert.NotContains(t, model.View().Content, "rpc secret")

	request, _ = model.requests.begin("node:node-id:cpu", model.generation())
	model, _ = updateDirect(model, directCPULoaded{request: request.stamp, nodeID: "node-id", node: "node", value: domain.CPUSnapshot{ProcessCountsKnown: true, RunningProcesses: 3}, err: context.Canceled})
	assert.Contains(t, model.View().Content, "CPU sample canceled")
	assert.Contains(t, model.View().Content, "Running processes: 3")
}

func TestDirectCPURetryShowsLoadingAndClearsOldReadError(t *testing.T) {
	session := openCPUTimeSession(t)
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.session, model.sessionCtx = session, context.Background()
	model.views = model.views.push(viewFrame{Kind: viewCPU, NodeID: "node-id", NodeTarget: "node"})
	model.cpu = model.cpu.setResult("node", domain.CPUSnapshot{}, errors.New("old failure"))

	model, command := updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	assert.Equal(t, directLoading, model.cpu.status)
	assert.Empty(t, model.cpu.err)
	assert.Contains(t, model.View().Content, "Sampling CPU")
}

func TestDirectTimeCheckValidatesInputAndUsesSession(t *testing.T) {
	var gotNode, gotServer string
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{TimeReader: &testkit.FakeTimeReader{
			ReadFunc: func(context.Context, string) (domain.TimeSnapshot, error) {
				return domain.TimeSnapshot{SyncKnown: true, Synced: true}, nil
			},
			CheckFunc: func(_ context.Context, node, server string) (domain.TimeCheckResult, error) {
				gotNode, gotServer = node, server
				return domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: server}}, nil
			},
		}}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	defer session.Close()
	model := newDirectNodesModel(t, LaunchOptions{}, []domain.NodeSnapshot{{ID: "node-id", Name: "node"}})
	model.session, model.sessionCtx = session, context.Background()
	model, command := updateDirect(model, keyPress('t'))
	require.Equal(t, viewTime, model.views.top().Kind)
	model, _ = applyDirectCommand(t, model, command)

	model, _ = updateDirect(model, keyPress('c'))
	require.NotNil(t, model.timePrompt)
	model.timePrompt.input.SetValue("https://bad.example")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, command)
	assert.NotNil(t, model.timePrompt)
	assert.NotEmpty(t, model.timePrompt.err)

	model.timePrompt.input.SetValue(" ntp.example ")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	require.Nil(t, model.timePrompt)
	model, _ = applyDirectCommand(t, model, command)
	assert.Equal(t, "node", gotNode)
	assert.Equal(t, "ntp.example", gotServer)
	assert.Contains(t, model.View().Content, "ntp.example")
}

func TestDirectTimePermissionFailureAndPartialValuesAreShownSafely(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.push(viewFrame{Kind: viewTime, NodeID: "node-id", NodeTarget: "node"})
	read, _ := model.requests.begin("node:node-id:time", model.generation())
	model, _ = updateDirect(model, directTimeLoaded{request: read.stamp, nodeID: "node-id", node: "node", value: domain.TimeSnapshot{SyncKnown: true, Synced: true, TimeSample: domain.TimeSample{Problems: []string{"server measurement unavailable"}}}})
	assert.Contains(t, model.View().Content, "Synchronized: yes")
	assert.Contains(t, model.View().Content, "server measurement unavailable")

	check, _ := model.requests.begin("node:node-id:time-check", model.generation())
	model, _ = updateDirect(model, directTimeCheckLoaded{request: check.stamp, nodeID: "node-id", node: "node", server: "ntp.example", err: ports.ErrTimePermissionDenied})
	assert.Contains(t, model.View().Content, "time check permission denied")
	assert.NotContains(t, model.View().Content, "permission denied: ")
}

func TestDirectTimeRetryShowsLoadingAndKeepsExplicitCheckResult(t *testing.T) {
	session := openCPUTimeSession(t)
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.session, model.sessionCtx = session, context.Background()
	model.views = model.views.push(viewFrame{Kind: viewTime, NodeID: "node-id", NodeTarget: "node"})
	model.time = model.time.setReadResult("node", domain.TimeSnapshot{}, errors.New("old failure"))
	model.time.checkStatus = directReady
	model.time.check = domain.TimeCheckResult{TimeSample: domain.TimeSample{Server: "ntp.example"}}

	model, command := updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	assert.Equal(t, directLoading, model.time.status)
	assert.Empty(t, model.time.err)
	assert.Equal(t, directReady, model.time.checkStatus)
	assert.Equal(t, "ntp.example", model.time.check.Server)
	assert.Contains(t, model.View().Content, "Loading time status")
	assert.Contains(t, model.View().Content, "ntp.example")
}

func TestDirectTimeReadProblemIsRenderedOnce(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.push(viewFrame{Kind: viewTime, NodeID: "node-id", NodeTarget: "node"})
	request, _ := model.requests.begin("node:node-id:time", model.generation())
	model, _ = updateDirect(model, directTimeLoaded{request: request.stamp, nodeID: "node-id", node: "node", value: domain.TimeSnapshot{TimeSample: domain.TimeSample{Problems: []string{"clock sample unavailable"}}}})
	assert.Equal(t, 1, strings.Count(model.View().Content, "Problem: clock sample unavailable"))
}

func openCPUTimeSession(t *testing.T) *application.Session {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			CPUReader:  &testkit.FakeCPUReader{},
			TimeReader: &testkit.FakeTimeReader{},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestDirectCPUAndTimeRejectStaleResultsAndCancelWhenViewCloses(t *testing.T) {
	for _, test := range []struct {
		name string
		kind viewKind
		slot string
		late tea.Msg
	}{
		{name: "cpu", kind: viewCPU, slot: "node:node-id:cpu", late: directCPULoaded{nodeID: "node-id", node: "node", value: domain.CPUSnapshot{Aggregate: domain.CPUUtilization{Known: true, BusyPercent: 99}}}},
		{name: "time", kind: viewTime, slot: "node:node-id:time", late: directTimeLoaded{nodeID: "node-id", node: "node", value: domain.TimeSnapshot{SyncKnown: true, Synced: false}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := newDirectModel(context.Background(), nil, LaunchOptions{})
			model.views = model.views.push(viewFrame{Kind: test.kind, NodeID: "node-id", NodeTarget: "node"})
			request, _ := model.requests.beginWithContext(test.slot, model.generation(), context.Background())
			model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
			assert.False(t, model.requests.current(request.stamp))
			assert.NotNil(t, request.ctx.Err())
			model, _ = updateDirect(model, test.late)
			assert.NotContains(t, model.View().Content, "99.0%")
		})
	}
}
