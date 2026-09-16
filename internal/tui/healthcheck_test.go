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

func TestHealthcheckCommandOpensViewAndStreamsProgress(t *testing.T) {
	stream := &testkit.FakeClusterHealthStream{Progress: []domain.ClusterHealthProgress{
		{Message: "waiting for etcd"},
		{EOF: true},
	}}
	reader := &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		return stream, nil
	}}
	nodes := domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl, Addresses: []string{"10.0.0.1"}},
		{Name: "worker-1", Role: domain.NodeRoleWorker, Addresses: []string{"10.0.0.2"}},
	}}
	root := newHealthcheckRoot(t, reader, nodes)

	root, _ = updateRoot(root, keyPress(':'))
	for _, character := range "healthcheck" {
		root, _ = updateRoot(root, keyPress(character))
	}
	root, command := updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEnter})

	require.NotNil(t, command)
	assert.Equal(t, viewClusterHealth, root.views.top().Kind)

	// Open effect -> clusterHealthOpened, then arm the first receive.
	root, command = runAppCmd(t, root, command)
	root, command = runAppCmd(t, root, command)
	assert.Contains(t, root.View().Content, "waiting for etcd")
	require.NotNil(t, command, "a non-EOF progress line must re-arm the receive")

	// EOF -> healthy verdict, no further receive.
	root, command = runAppCmd(t, root, command)
	assert.Nil(t, command)
	assert.Contains(t, root.View().Content, "cluster is healthy")

	// Back works and closes the stream.
	root, _ = updateRoot(root, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Equal(t, viewNodes, root.views.top().Kind)
	assert.Equal(t, application.ClusterHealthState{}, root.application.HealthCheck)
}

func TestHealthcheckReconnectRebuildsRequestFromCurrentNodes(t *testing.T) {
	var requests []domain.ClusterHealthRequest
	reader := &testkit.FakeClusterHealthReader{OpenFunc: func(_ context.Context, request domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		requests = append(requests, request)
		return &testkit.FakeClusterHealthStream{Progress: []domain.ClusterHealthProgress{{EOF: true}}}, nil
	}}
	// Start with no nodes loaded, as if the palette was used before :nodes settled.
	root := newHealthcheckRoot(t, reader, domain.NodeSet{})
	root.views = root.views.replaceRoot(viewFrame{Kind: viewClusterHealth, Label: "healthcheck"})
	root.application.HealthCheck = application.ClusterHealthState{Status: application.Ready, Request: domain.ClusterHealthRequest{WaitTimeout: time.Minute}}
	root.healthcheck = newHealthcheckModel(root.application.HealthCheck)

	// Nodes become available after the check started.
	root.application, _ = application.Update(root.application, application.NodesLoaded{Generation: root.application.Generation, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl, Addresses: []string{"10.0.0.1"}},
	}}})

	root, command := updateRoot(root, keyPress('r'))
	require.NotNil(t, command)
	root, _ = updateRoot(root, command())

	require.NotEmpty(t, requests, "reconnect must open a new stream")
	assert.Equal(t, []string{"10.0.0.1"}, requests[len(requests)-1].ControlPlaneNodes, "reconnect must rebuild node lists from the current :nodes snapshot")
}

func TestHealthcheckRendersVerdictAfterEOF(t *testing.T) {
	root := newHealthcheckRoot(t, nil, domain.NodeSet{})
	root.views = root.views.replaceRoot(viewFrame{Kind: viewClusterHealth, Label: "healthcheck"})
	root.application.HealthCheck = application.ClusterHealthState{
		Status:       application.Ready,
		Request:      domain.ClusterHealthRequest{WaitTimeout: time.Minute},
		Lines:        []string{"waiting for etcd"},
		EOF:          true,
		VerdictReady: true,
	}
	root.healthcheck = newHealthcheckModel(root.application.HealthCheck)

	content := root.View().Content

	assert.Contains(t, content, "cluster is healthy")
	assert.Contains(t, content, "waiting for etcd")
}

func TestHealthcheckRendersFailureWithoutRawGRPCText(t *testing.T) {
	root := newHealthcheckRoot(t, nil, domain.NodeSet{})
	root.application.HealthCheck = application.ClusterHealthState{Status: application.Loading, Request: domain.ClusterHealthRequest{WaitTimeout: time.Minute}}
	root.healthcheck = newHealthcheckModel(root.application.HealthCheck)
	raw := fmt.Errorf("rpc error: code = DeadlineExceeded desc = context deadline exceeded")

	root.application, _ = application.Update(root.application, application.ClusterHealthProgressLoaded{Generation: root.application.Generation, Err: raw})
	root.healthcheck = root.healthcheck.setState(root.application.HealthCheck)

	rendered := root.healthcheck.viewSized(contentSize{Width: 80, Height: 10})
	assert.Contains(t, rendered, "cluster health check unavailable")
	assert.NotContains(t, rendered, "rpc error")
	assert.NotContains(t, rendered, "DeadlineExceeded")
}

func runAppCmd(t *testing.T, root model, command tea.Cmd) (model, tea.Cmd) {
	t.Helper()
	require.NotNil(t, command)
	return updateRoot(root, command())
}

func newHealthcheckRoot(t *testing.T, reader ports.ClusterHealthReader, nodes domain.NodeSet) model {
	t.Helper()
	applicationModel := application.Model{Generation: 1, ContextName: "prod"}
	applicationModel, _ = application.Update(applicationModel, application.SessionOpened{
		Generation:    1,
		Nodes:         &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) { return nodes, nil }},
		ClusterHealth: reader,
	})
	applicationModel.Nodes = application.NodeState{Status: application.Ready, Value: nodes}

	root := newModel(t.Context(), false, applicationModel, application.NewRunner(application.Dependencies{}))
	root.splash = false
	return root
}
