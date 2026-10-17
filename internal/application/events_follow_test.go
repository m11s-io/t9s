package application_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventFollowAppendsBoundsAndRejectsStoppedStream(t *testing.T) {
	stream := &testkit.FakeEventStream{Batches: []domain.EventSet{{Events: []domain.EventSnapshot{{Node: "cp-1", Message: "new"}}}}}
	reader := &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }}
	model := withEventReader(application.Model{Generation: 7, Events: application.EventState{Status: application.Ready, Value: domain.EventSet{Events: []domain.EventSnapshot{{Message: "snapshot"}}}}}, reader)
	runner := application.NewRunner(application.Dependencies{})
	model, effect := application.Update(model, application.ToggleEventFollow{})
	require.True(t, model.Events.Following)
	require.NotNil(t, effect)
	model, effect = application.Update(model, runner.Run(t.Context(), effect))
	require.NotNil(t, effect)
	batch := runner.Run(t.Context(), effect)
	model, effect = application.Update(model, batch)
	require.Len(t, model.Events.Value.Events, 2)
	assert.Equal(t, "new", model.Events.Value.Events[1].Message)
	model, closeEffect := application.Update(model, application.StopEventFollow{})
	require.NotNil(t, closeEffect)
	runner.Run(t.Context(), closeEffect)
	assert.True(t, stream.Closed())
	model, effect = application.Update(model, batch)
	assert.Nil(t, effect)
	require.Len(t, model.Events.Value.Events, 2, "late batches must not append after stop")
	assert.False(t, model.Events.Following)
}

func TestEventFollowCapsBufferAndPreservesNodeFailures(t *testing.T) {
	events := make([]domain.EventSnapshot, 2200)
	for i := range events {
		events[i].Message = fmt.Sprint(i)
	}
	stream := &testkit.FakeEventStream{Batches: []domain.EventSet{{Events: events, Problems: []domain.EventProblem{{Node: "cp-2", Message: "event stream unavailable"}}}}}
	model := withEventReader(application.Model{Generation: 1}, &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }})
	runner := application.NewRunner(application.Dependencies{})
	model, effect := application.Update(model, application.ToggleEventFollow{})
	model, effect = application.Update(model, runner.Run(t.Context(), effect))
	model, _ = application.Update(model, runner.Run(t.Context(), effect))
	require.Len(t, model.Events.Value.Events, 2000)
	assert.Equal(t, "200", model.Events.Value.Events[0].Message)
	require.Len(t, model.Events.Value.Problems, 1)
	assert.Equal(t, "cp-2", model.Events.Value.Problems[0].Node)
	assert.Equal(t, application.Partial, model.Events.Status)
}

func TestEventFollowEOFStopsWithoutDiscardingRows(t *testing.T) {
	stream := &testkit.FakeEventStream{Err: io.EOF}
	model := withEventReader(application.Model{Generation: 1, Events: application.EventState{Value: domain.EventSet{Events: []domain.EventSnapshot{{Message: "kept"}}}}}, &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }})
	runner := application.NewRunner(application.Dependencies{})
	model, effect := application.Update(model, application.ToggleEventFollow{})
	model, effect = application.Update(model, runner.Run(t.Context(), effect))
	model, effect = application.Update(model, runner.Run(t.Context(), effect))
	assert.False(t, model.Events.Following)
	require.Len(t, model.Events.Value.Events, 1)
	require.NotNil(t, effect)
	runner.Run(t.Context(), effect)
	assert.True(t, stream.Closed())
}

func TestEventFollowIgnoresSnapshotResultsAndClosesOnContextChange(t *testing.T) {
	stream := &testkit.FakeEventStream{}
	model := withEventReader(application.Model{Generation: 1, ContextName: "old"}, &testkit.FakeEventReader{OpenFunc: func(context.Context) (ports.EventStream, error) { return stream, nil }})
	runner := application.NewRunner(application.Dependencies{})
	model, effect := application.Update(model, application.ToggleEventFollow{})
	opened := runner.Run(t.Context(), effect)
	model, _ = application.Update(model, opened)
	model, _ = application.Update(model, application.EventsLoaded{Generation: 1, Events: domain.EventSet{Events: []domain.EventSnapshot{{Message: "late snapshot"}}}})
	assert.Empty(t, model.Events.Value.Events)
	model, effect = application.Update(model, application.SelectContext{Name: "new"})
	require.NotNil(t, effect)
	runner.Run(t.Context(), effect)
	assert.True(t, stream.Closed())
	model, effect = application.Update(model, opened)
	require.NotNil(t, effect, "late opened stream must be closed")
	runner.Run(t.Context(), effect)
	assert.Empty(t, model.Events.Value.Events)
	assert.False(t, model.Events.Following)
}

func TestEventFollowStopCancelsPendingOpen(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	reader := &testkit.FakeEventReader{OpenFunc: func(ctx context.Context) (ports.EventStream, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}}
	model := withEventReader(application.Model{Generation: 1}, reader)
	runner := application.NewRunner(application.Dependencies{})
	model, effect := application.Update(model, application.ToggleEventFollow{})
	result := make(chan application.Message, 1)
	go func() { result <- runner.Run(t.Context(), effect) }()
	<-started
	model, closeEffect := application.Update(model, application.StopEventFollow{})
	if closeEffect != nil {
		runner.Run(t.Context(), closeEffect)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("stopping follow did not cancel the pending Open")
	}
	model, closeEffect = application.Update(model, <-result)
	if closeEffect != nil {
		runner.Run(t.Context(), closeEffect)
	}
	assert.False(t, model.Events.Following)
}

func TestEventFollowBoundsExistingSnapshotOnStart(t *testing.T) {
	events := make([]domain.EventSnapshot, 2200)
	events[200].Message = "oldest retained"
	model := application.Model{Generation: 1, Events: application.EventState{Value: domain.EventSet{Events: events}}}
	model, _ = application.Update(model, application.ToggleEventFollow{})
	require.Len(t, model.Events.Value.Events, 2000)
	assert.Equal(t, "oldest retained", model.Events.Value.Events[0].Message)
}

func TestEventFollowRunnerCloseCancelsRPCContext(t *testing.T) {
	var streamCtx context.Context
	reader := &testkit.FakeEventReader{OpenFunc: func(ctx context.Context) (ports.EventStream, error) {
		streamCtx = ctx
		return &testkit.FakeEventStream{}, nil
	}}
	runner := application.NewRunner(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) { return domain.NodeSet{}, nil }}, EventReader: reader}, nil
	}}})
	defer runner.Close()
	model := application.Model{Generation: 1, ContextName: "old"}
	model, effect := application.Update(model, application.ContextsLoaded{Generation: 1, ContextName: "old"})
	model, _ = application.Update(model, runner.Run(t.Context(), effect))
	model, effect = application.Update(model, application.ToggleEventFollow{})
	model, _ = application.Update(model, runner.Run(t.Context(), effect))
	require.NotNil(t, streamCtx)
	require.NoError(t, runner.Close())
	select {
	case <-streamCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("event RPC context survived runner shutdown")
	}
}

func TestEventFollowLateStartupSnapshotStillLoadsEtcd(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			model := application.Model{Generation: 1, Nodes: application.NodeState{Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleControl}}}}}
			model, _ = application.Update(model, application.SessionOpened{Generation: 1, Etcd: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
				return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1"}}}, nil
			}}})
			model, _ = application.Update(model, application.ToggleEventFollow{})
			var snapshot application.Message = application.EventsLoaded{Generation: 1, Events: domain.EventSet{Events: []domain.EventSnapshot{{Message: "stale"}}}}
			if failed {
				snapshot = application.EventsFailed{Generation: 1, Err: fmt.Errorf("unreachable")}
			}
			model, effect := application.Update(model, snapshot)
			assert.Empty(t, model.Events.Value.Events)
			require.NotNil(t, effect, "discarding stale events must preserve startup's etcd/Kubernetes continuation")
			runner := application.NewRunner(application.Dependencies{})
			model, _ = application.Update(model, runner.Run(t.Context(), effect))
			require.Len(t, model.Etcd.Value.Members, 1)
			assert.Equal(t, "cp-1", model.Etcd.Value.Members[0].Hostname)
		})
	}
}
