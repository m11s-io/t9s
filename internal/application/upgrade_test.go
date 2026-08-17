package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type controlledUpgradeStream struct {
	results <-chan ports.UpgradeResult
	cancel  func()
	once    sync.Once
}

func (s *controlledUpgradeStream) Results() <-chan ports.UpgradeResult { return s.results }
func (s *controlledUpgradeStream) Cancel() {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
}

func TestUpgradeBridgeStreamsProgressAndRefreshesNodesOnSuccess(t *testing.T) {
	results := make(chan ports.UpgradeResult, 3)
	results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulling installer", Current: 25, Total: 100}}
	results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: "installing"}}
	results <- ports.UpgradeResult{Done: true}
	close(results)
	var nodeLoads int
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, Nodes: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) { nodeLoads++; return domain.NodeSet{}, nil }}, NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
		return &controlledUpgradeStream{results: results}
	}}})

	started := application.BuildActionEffects(model, application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "image:v1.13.3"})[0](t.Context(), application.Dependencies{})
	start, ok := started.(application.UpgradeStarted)
	require.True(t, ok)
	assert.Equal(t, model.Generation, start.Generation)
	assert.Equal(t, "cp-1", start.Target)
	model, next := application.Update(model, start)
	require.NotNil(t, next)
	assert.True(t, model.Upgrade.Active)

	progress := next(t.Context(), application.Dependencies{})
	assert.Equal(t, application.UpgradeProgressed{Generation: model.Generation, Target: "cp-1", Event: ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulling installer", Current: 25, Total: 100}}, progress)
	model, next = application.Update(model, progress)
	assert.Equal(t, ports.UpgradePulling, model.Upgrade.Event.Phase)
	progress = next(t.Context(), application.Dependencies{})
	assert.Equal(t, application.UpgradeProgressed{Generation: model.Generation, Target: "cp-1", Event: ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: "installing"}}, progress)
	model, next = application.Update(model, progress)
	assert.Equal(t, ports.UpgradeInstalling, model.Upgrade.Event.Phase)

	completed := next(t.Context(), application.Dependencies{})
	assert.Equal(t, application.UpgradeSucceeded{Generation: model.Generation, Target: "cp-1"}, completed)
	model, refresh := application.Update(model, completed)
	assert.False(t, model.Upgrade.Active)
	assert.Equal(t, []application.ActionResult{{Target: "cp-1"}}, model.ActionResults)
	require.NotNil(t, refresh)
	_ = refresh(t.Context(), application.Dependencies{})
	assert.Equal(t, 1, nodeLoads)
}

func TestUpgradeBridgeFailsWhenStreamClosesWithoutTerminalResult(t *testing.T) {
	results := make(chan ports.UpgradeResult)
	close(results)
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
		return &controlledUpgradeStream{results: results}
	}}})
	started := application.BuildActionEffects(model, application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "image:v1.13.3"})[0](t.Context(), application.Dependencies{})
	model, next := application.Update(model, started)
	failed := next(t.Context(), application.Dependencies{})
	require.IsType(t, application.UpgradeFailed{}, failed)
	assert.ErrorContains(t, failed.(application.UpgradeFailed).Err, "closed without a terminal result")
	model, _ = application.Update(model, failed)
	assert.False(t, model.Upgrade.Active)
	assert.Equal(t, "upgrade failed", model.ActionResults[0].Err)
}

func TestUpgradeBridgeIgnoresStaleProgressMessages(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.Generation++
	got, effect := application.Update(model, application.UpgradeProgressed{Generation: model.Generation - 1, Target: "cp-1", Event: ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "stale"}})
	assert.Equal(t, model, got)
	assert.Nil(t, effect)
}

func TestUpgradeBridgeGatesCompetingWriteRequestsWhileActive(t *testing.T) {
	results := make(chan ports.UpgradeResult)
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
		return &controlledUpgradeStream{results: results}
	}}})
	started := application.BuildActionEffects(model, application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "image:v1.13.3"})[0](t.Context(), application.Dependencies{})
	model, _ = application.Update(model, started)
	for _, message := range []application.Message{application.RequestAction{Kind: application.ActionReboot, Targets: []string{"cp-2"}}, application.RequestServiceAction{Kind: application.ServiceActionRestart, Node: "cp-2", Service: "kubelet"}, application.RequestUpgradePrompt{Target: "cp-2"}} {
		got, effect := application.Update(model, message)
		assert.True(t, got.Upgrade.Active)
		assert.Equal(t, "cp-1", got.Upgrade.Target)
		assert.Nil(t, got.PendingAction)
		assert.Nil(t, got.PendingServiceAction)
		assert.Nil(t, effect)
	}
	close(results)
}

func TestUpgradeBridgeCancellationFailsAndCancelsControllerStream(t *testing.T) {
	results := make(chan ports.UpgradeResult)
	canceled := make(chan struct{})
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
		return &controlledUpgradeStream{results: results, cancel: func() { close(canceled); close(results) }}
	}}})
	ctx, cancel := context.WithCancel(t.Context())
	started := application.BuildActionEffects(model, application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "image:v1.13.3"})[0](ctx, application.Dependencies{})
	model, next := application.Update(model, started)
	cancel()
	failed := next(ctx, application.Dependencies{})
	require.IsType(t, application.UpgradeFailed{}, failed)
	assert.ErrorIs(t, failed.(application.UpgradeFailed).Err, context.Canceled)
	require.Eventually(t, func() bool {
		select {
		case <-canceled:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestUpgradeBridgeRejectsBlankImageWithoutCallingController(t *testing.T) {
	called := false
	model, _ := application.NewModel("prod")
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
		called = true
		return nil
	}}})

	effects := application.BuildActionEffects(model, application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}})
	require.Len(t, effects, 1)
	message := effects[0](t.Context(), application.Dependencies{})
	require.IsType(t, application.UpgradeStarted{}, message)
	model, next := application.Update(model, message)
	failed := next(t.Context(), application.Dependencies{})
	require.IsType(t, application.UpgradeFailed{}, failed)
	assert.ErrorContains(t, failed.(application.UpgradeFailed).Err, "image is required")
	assert.False(t, called)
}

func TestUpgradeBridgeReturnsStartedBeforeBlockingStreamConstruction(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
		close(entered)
		<-release
		results := make(chan ports.UpgradeResult, 1)
		results <- ports.UpgradeResult{Done: true}
		close(results)
		return &controlledUpgradeStream{results: results}
	}}})

	returned := make(chan application.Message, 1)
	go func() {
		returned <- application.BuildActionEffects(model, application.PendingAction{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "image:v1.13.3"})[0](t.Context(), application.Dependencies{})
	}()
	require.Eventually(t, func() bool {
		select {
		case message := <-returned:
			returned <- message
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	started := <-returned
	model, _ = application.Update(model, started)
	assert.True(t, model.Upgrade.Active)
	got, effect := application.Update(model, application.RequestAction{Kind: application.ActionReboot, Targets: []string{"cp-2"}})
	assert.True(t, got.Upgrade.Active)
	assert.Equal(t, "cp-1", got.Upgrade.Target)
	assert.Nil(t, got.PendingAction)
	assert.Nil(t, effect)
	require.Eventually(t, func() bool {
		select {
		case <-entered:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestUpgradeBridgeDoesNotPersistUntrustedErrorText(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.Upgrade = application.UpgradeState{Active: true, Target: "cp-1"}
	model, _ = application.Update(model, application.UpgradeFailed{Generation: model.Generation, Target: "cp-1", Err: errors.New("request failed\n\x1b[31mBearer super-secret-token\x1b[0m")})

	assert.Equal(t, "upgrade failed", model.Upgrade.Err)
	assert.Equal(t, "upgrade failed", model.ActionResults[0].Err)
	assert.NotContains(t, model.Upgrade.Err, "super-secret-token")
	assert.NotContains(t, model.Upgrade.Err, "\n")
	assert.False(t, strings.Contains(model.Upgrade.Err, "\x1b"))
}
