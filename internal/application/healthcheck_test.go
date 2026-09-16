package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenClusterHealthStartsStreamingAndLoadsProgress(t *testing.T) {
	stream := &testkit.FakeClusterHealthStream{Progress: []domain.ClusterHealthProgress{
		{Message: "waiting for etcd"},
		{EOF: true},
	}}
	reader := &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		return stream, nil
	}}
	model := application.Model{Generation: 7}
	model = withClusterHealthReader(model, reader)

	model, effect := application.Update(model, application.OpenClusterHealth{Request: domain.ClusterHealthRequest{
		ControlPlaneNodes: []string{"10.0.0.1"},
		WaitTimeout:       time.Minute,
	}})
	assert.Equal(t, application.Loading, model.HealthCheck.Status)
	assert.Equal(t, time.Minute, model.HealthCheck.Request.WaitTimeout)
	require.NotNil(t, effect)

	runner := application.NewRunner(application.Dependencies{})
	opened := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, opened)
	require.NotNil(t, effect, "opening the stream must arm the first receive")

	loaded := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, loaded)
	assert.Equal(t, []string{"waiting for etcd"}, model.HealthCheck.Lines)
	require.NotNil(t, effect, "the next receive is armed only after the prior progress is reduced")

	eof := runner.Run(context.Background(), effect)
	model, effect = application.Update(model, eof)
	assert.Nil(t, effect)
	assert.True(t, model.HealthCheck.EOF)
	assert.True(t, model.HealthCheck.VerdictReady)
	assert.Equal(t, application.Ready, model.HealthCheck.Status)
}

func TestClusterHealthTranscriptIsCapped(t *testing.T) {
	progress := make([]domain.ClusterHealthProgress, 0, 502)
	for index := range 501 {
		progress = append(progress, domain.ClusterHealthProgress{Message: fmt.Sprintf("line-%d", index)})
	}
	progress = append(progress, domain.ClusterHealthProgress{EOF: true})
	stream := &testkit.FakeClusterHealthStream{Progress: progress}
	reader := &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		return stream, nil
	}}
	model := application.Model{Generation: 7}
	model = withClusterHealthReader(model, reader)
	runner := application.NewRunner(application.Dependencies{})

	model, effect := application.Update(model, application.OpenClusterHealth{Request: domain.ClusterHealthRequest{WaitTimeout: time.Minute}})
	require.NotNil(t, effect)
	model, effect = application.Update(model, runner.Run(context.Background(), effect))
	require.NotNil(t, effect)

	for effect != nil && !model.HealthCheck.EOF {
		model, effect = application.Update(model, runner.Run(context.Background(), effect))
	}

	assert.True(t, model.HealthCheck.EOF)
	require.Len(t, model.HealthCheck.Lines, 500, "the retained transcript must be bounded")
	assert.Equal(t, "line-1", model.HealthCheck.Lines[0], "the oldest line must be dropped first")
}

func TestClusterHealthFailsOnMidStreamError(t *testing.T) {
	model := application.Model{Generation: 3}

	model, effect := application.Update(model, application.ClusterHealthProgressLoaded{
		Generation: 3, StreamGeneration: 1, Err: errors.New("rpc error: code = DeadlineExceeded desc = timed out"),
	})

	assert.Nil(t, effect, "a failed stream must not re-arm")
	assert.Equal(t, application.Failed, model.HealthCheck.Status)
	assert.Equal(t, "cluster health check unavailable", model.HealthCheck.Err)
	assert.NotContains(t, model.HealthCheck.Err, "DeadlineExceeded")
}

func TestClusterHealthProgressErrorFailsStream(t *testing.T) {
	model := application.Model{Generation: 3}

	model, effect := application.Update(model, application.ClusterHealthProgressLoaded{
		Generation: 3, StreamGeneration: 1, Progress: domain.ClusterHealthProgress{Err: "cluster health check error"},
	})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.HealthCheck.Status)
	assert.Equal(t, "cluster health check unavailable", model.HealthCheck.Err)
}

func TestCloseClusterHealthClearsStateAndClosesStream(t *testing.T) {
	stream := &testkit.FakeClusterHealthStream{}
	reader := &testkit.FakeClusterHealthReader{OpenFunc: func(context.Context, domain.ClusterHealthRequest) (ports.ClusterHealthStream, error) {
		return stream, nil
	}}
	model := application.Model{Generation: 7}
	model = withClusterHealthReader(model, reader)
	runner := application.NewRunner(application.Dependencies{})

	model, effect := application.Update(model, application.OpenClusterHealth{Request: domain.ClusterHealthRequest{WaitTimeout: time.Minute}})
	opened := runner.Run(context.Background(), effect)
	model, _ = application.Update(model, opened)

	model, effect = application.Update(model, application.CloseClusterHealth{})
	require.NotNil(t, effect)
	assert.Equal(t, application.ClusterHealthState{}, model.HealthCheck)
	assert.False(t, stream.Closed())

	_ = runner.Run(context.Background(), effect)
	assert.True(t, stream.Closed())
}

func TestSelectContextResetsClusterHealth(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", HealthCheck: application.ClusterHealthState{
		Status:  application.Ready,
		Request: domain.ClusterHealthRequest{WaitTimeout: time.Minute},
		Lines:   []string{"x"},
	}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Equal(t, application.ClusterHealthState{}, model.HealthCheck)
}

func TestClearClusterHealthKeepsState(t *testing.T) {
	model := application.Model{HealthCheck: application.ClusterHealthState{
		Status: application.Ready,
		Lines:  []string{"one", "two"},
	}}

	model, effect := application.Update(model, application.ClearClusterHealth{})

	assert.Nil(t, effect)
	assert.Nil(t, model.HealthCheck.Lines)
}

func TestClusterHealthRequestUsesNodeAddressesNotHostnames(t *testing.T) {
	nodes := []domain.NodeSnapshot{
		{Name: "cp-1", Role: domain.NodeRoleControl, Addresses: []string{"10.0.0.5"}},
		{Name: "cp-2", Role: domain.NodeRoleControl},
		{Name: "10.0.0.6", Role: domain.NodeRoleWorker, Addresses: []string{"10.0.0.6", "10.0.0.7"}},
		{Name: "worker-host", Role: domain.NodeRoleWorker},
	}

	request := application.ClusterHealthRequestFromNodes(nodes, 2*time.Minute)

	assert.Equal(t, []string{"10.0.0.5"}, request.ControlPlaneNodes, "a hostname-only control-plane node is dropped")
	assert.Equal(t, []string{"10.0.0.6"}, request.WorkerNodes)
	assert.Equal(t, 2*time.Minute, request.WaitTimeout)
}

// withClusterHealthReader seeds a Model's unexported clusterHealthReader field
// via the SessionOpened message, since application.Model has no exported
// setter and this test file is package application_test.
func withClusterHealthReader(model application.Model, reader ports.ClusterHealthReader) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, ClusterHealth: reader})
	return model
}
