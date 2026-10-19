package tui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type directUpgradeTestStream struct {
	results  chan ports.UpgradeResult
	terminal ports.UpgradeResult
}

func (s *directUpgradeTestStream) Results() <-chan ports.UpgradeResult { return s.results }
func (s *directUpgradeTestStream) Cancel()                             {}
func (s *directUpgradeTestStream) Wait(context.Context) (ports.UpgradeResult, error) {
	return s.terminal, nil
}

func directUpgradeSession(t *testing.T, currentImage string, currentErr error, upgrade func(context.Context, string, string) ports.UpgradeStream) *application.Session {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}, nil
			}},
			NodeController: &testkit.FakeNodeController{
				CurrentInstallImageFunc: func(context.Context, string) (string, error) { return currentImage, currentErr },
				UpgradeStreamFunc:       upgrade,
			},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func directUpgradeModel(t *testing.T, session *application.Session) directModel {
	t.Helper()
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	nodes := domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}}}
	model.cluster.nodes = application.NodeState{Status: application.Ready, Value: nodes}
	model.nodes = model.nodes.normalizeSelection(nodes.Nodes, 0)
	return model
}

func TestDirectUpgradePrefillsFromCurrentImageAndPreparesEditedImage(t *testing.T) {
	var target, image string
	session := directUpgradeSession(t, "installer:v1.14.0", nil, func(_ context.Context, gotTarget, gotImage string) ports.UpgradeStream {
		target, image = gotTarget, gotImage
		return &directUpgradeTestStream{results: make(chan ports.UpgradeResult), terminal: ports.UpgradeResult{Outcome: ports.UpgradeOutcomeApplied, Done: true}}
	})
	model := directUpgradeModel(t, session)
	model, command := updateDirect(model, keyPress('U'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.upgradePrompt)
	assert.Equal(t, "installer:v1.14.0", model.upgradePrompt.input.Value())
	model.upgradePrompt.input.SetValue("installer:v1.15.0")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Equal(t, application.ActionUpgrade, model.mutation.prompt.nodeKind)
	assert.Contains(t, ansi.Strip(model.View().Content), "Upgrade to installer:v1.15.0")
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	for model.mutation.busy {
		model, command = updateDirect(model, command())
	}
	assert.Equal(t, "cp-1", target)
	assert.Equal(t, "installer:v1.15.0", image)
	assert.Contains(t, model.mutation.notice, "Upgrade cp-1 applied")
}

func TestDirectUpgradeCurrentImageFailureIsNormalizedAndReplaceable(t *testing.T) {
	session := directUpgradeSession(t, "", errors.New("Bearer secret-token"), nil)
	model := directUpgradeModel(t, session)
	model, command := updateDirect(model, keyPress('U'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.upgradePrompt)
	assert.Empty(t, model.upgradePrompt.input.Value())
	content := ansi.Strip(model.View().Content)
	assert.NotContains(t, content, "secret-token")
	assert.Contains(t, model.view.notice, "current install image is unavailable")
	model.upgradePrompt.input.SetValue("installer:v1.15.0")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command, "an operator can replace a missing prefill")
}

func TestDirectUpgradePromptCancelAndStalePreparationDoNotExecute(t *testing.T) {
	session := directUpgradeSession(t, "installer:v1.14.0", nil, func(context.Context, string, string) ports.UpgradeStream {
		t.Error("canceled or stale upgrade must not execute")
		return nil
	})
	model := directUpgradeModel(t, session)
	model, command := updateDirect(model, keyPress('U'))
	model, _ = applyDirectCommand(t, model, command)
	imagePrompt := model.upgradePrompt
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, model.upgradePrompt)
	model, _ = updateDirect(model, directCurrentInstallImage{operation: model.mutation.operation - 1, generation: model.generation(), image: "stale:v1"})
	assert.Nil(t, model.upgradePrompt)
	assert.NotSame(t, imagePrompt, model.upgradePrompt)
}

func TestDirectUpgradeReadOnlyAffordanceDoesNotReadOrPrepare(t *testing.T) {
	session := directUpgradeSession(t, "installer:v1.14.0", nil, nil)
	model := directUpgradeModel(t, session)
	model.options.WritesEnabled = false
	model, command := updateDirect(model, keyPress('U'))
	assert.Nil(t, command)
	assert.Nil(t, model.upgradePrompt)
	assert.False(t, model.mutation.busy)
}

func TestDirectUpgradeTerminalOutcomesRemainDistinctAndErrorsStayInspectable(t *testing.T) {
	recoveryErr := errors.New("uncordon failed: secret transport details")
	cases := []struct {
		name    string
		result  ports.UpgradeResult
		want    string
		outcome domain.OperationOutcome
	}{
		{name: "accepted", result: ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAccepted, Done: true}, want: "accepted — completion not verified", outcome: domain.OperationAccepted},
		{name: "applied", result: ports.UpgradeResult{Outcome: ports.UpgradeOutcomeApplied, Done: true}, want: "Upgrade cp-1 applied", outcome: domain.OperationCompleted},
		{name: "recovery warning", result: ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAppliedWithRecoveryWarning, Warning: recoveryErr.Error(), Err: recoveryErr, Done: true}, want: "recovery warning", outcome: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := directUpgradeModel(t, directUpgradeSession(t, "installer:v1.14.0", nil, nil))
			model.mutation.busy = true
			model.mutation.operation = 3
			model, _ = updateDirect(model, directMutationFinished{
				operation: 3, generation: model.generation(),
				upgradeTarget: "cp-1", upgrade: &test.result,
			})
			assert.False(t, model.mutation.busy)
			assert.Contains(t, model.mutation.notice, test.want)
			assert.Equal(t, test.outcome, model.mutation.upgrade.Outcome)
			if test.name == "recovery warning" {
				require.NotNil(t, model.mutation.upgradeResult)
				assert.ErrorIs(t, model.mutation.upgradeResult.Err, recoveryErr)
				assert.NotContains(t, model.mutation.notice, "secret transport details")
			}
		})
	}
}

func TestDirectUpgradeProgressIsBestEffortAndTerminalWaitFinishes(t *testing.T) {
	waitRelease := make(chan struct{})
	results := make(chan ports.UpgradeResult, 2)
	results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulling installer", Current: 3, Total: 10}}
	results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: "installing"}}
	close(results)
	stream := &gatedDirectUpgradeStream{results: results, release: waitRelease, terminal: ports.UpgradeResult{Outcome: ports.UpgradeOutcomeApplied, Done: true}}
	session := directUpgradeSession(t, "installer:v1.14.0", nil, func(context.Context, string, string) ports.UpgradeStream { return stream })
	model := directUpgradeModel(t, session)
	model.mutation.operation = 1
	model.mutation.busy = true
	model.mutation.total = 1
	model.mutationWork = directMutationWork{operation: 1, generation: model.generation(), ctx: context.Background(), session: session, plan: &directMutationPrompt{
		operation: 1, generation: model.generation(), nodeKind: application.ActionUpgrade, node: mustPrepareUpgradeForDirectTest(t, session),
		assessment: application.Assessment{Targets: []string{"cp-1"}}, confirm: application.Confirmation{Approved: true}, session: session,
	}, events: make(chan directMutationWorkItem, 2)}
	command := directMutationStartCommand(model.mutationWork)
	model, command = updateDirect(model, command())
	require.Contains(t, model.mutation.notice, "pulling 30%")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, viewNodeDetail, model.views.top().Kind, "read and navigation remain available during the accepted upgrade")
	model, _ = updateDirect(model, keyPress('U'))
	assert.True(t, model.mutation.busy)
	assert.Nil(t, model.upgradePrompt, "a second write prompt cannot open while upgrade is active")
	close(waitRelease)
	for model.mutation.busy {
		model, command = updateDirect(model, command())
	}
	assert.Contains(t, model.mutation.notice, "Upgrade cp-1 applied")
}

func TestDirectUpgradeAwaitReturnsOnSessionSwitchWhileWaitRetainsTerminal(t *testing.T) {
	stream := newCancelObservedUpgradeStream()
	stream.results <- ports.UpgradeResult{Event: &ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulling installer"}}
	session := directUpgradeSession(t, "installer:v1.14.0", nil, func(context.Context, string, string) ports.UpgradeStream { return stream })
	model := directUpgradeModel(t, session)
	operationCtx, cancel := context.WithCancel(context.Background())
	model.sessionCtx, model.sessionCancel = operationCtx, cancel
	work := directUpgradeWorkForTest(t, model, session, operationCtx)
	model.mutationWork = work
	model.mutation.operation, model.mutation.busy = work.operation, true
	model.mutation.operation, model.mutation.busy = work.operation, true
	startMessage := make(chan tea.Msg, 1)
	go func() { startMessage <- directMutationStartCommand(work)() }()
	select {
	case <-stream.waitStarted:
	case <-time.After(time.Second):
		t.Fatal("upgrade Wait observer did not start")
	}
	var first tea.Msg
	select {
	case first = <-startMessage:
	case <-time.After(time.Second):
		t.Fatal("best-effort progress did not reach the UI")
	}
	require.IsType(t, directUpgradeProgress{}, first)
	_, await := updateDirect(model, first)
	require.NotNil(t, await)
	model.beginSessionOpen("next-context") // cancels the session that owns the accepted upgrade
	require.ErrorIs(t, operationCtx.Err(), context.Canceled)
	returned := make(chan tea.Msg, 1)
	go func() { returned <- await() }()
	select {
	case message := <-returned:
		assert.IsType(t, directMutationCanceled{}, message)
	case <-time.After(100 * time.Millisecond):
		// Release simulated remote cleanup so a failed assertion cannot leak test goroutines.
		close(stream.release)
		<-returned
		t.Fatal("await Tea command remained blocked after session cancellation")
	}
	select {
	case <-stream.cancelled:
	case <-time.After(time.Second):
		t.Fatal("session cancellation did not cancel the upgrade stream")
	}
	select {
	case <-stream.waitFinished:
		t.Fatal("TUI cancellation released the retained Wait before cleanup completed")
	default:
	}
	close(stream.release)
	select {
	case <-stream.waitFinished:
	case <-time.After(time.Second):
		t.Fatal("retained Wait observer did not finish after cancellation cleanup")
	}
	updated, _ := updateDirect(model, directMutationFinished{operation: work.operation, generation: work.generation,
		upgradeTarget: "cp-1", upgrade: &ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAccepted, Done: true}})
	assert.Equal(t, "next-context", updated.cluster.contextName)
	assert.NotContains(t, updated.mutation.notice, "Upgrade cp-1")
}

func TestDirectUpgradeAdmissionAssessmentChangeIsKnownPreDispatchFailure(t *testing.T) {
	var changed atomic.Bool
	var dispatches atomic.Int32
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				version := "v1.14.0"
				if changed.Load() {
					version = "v1.14.1"
				}
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl, Version: version}}}, nil
			}},
			NodeController: &testkit.FakeNodeController{
				CurrentInstallImageFunc: func(context.Context, string) (string, error) { return "installer:v1.14.0", nil },
				UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
					dispatches.Add(1)
					return &directUpgradeTestStream{results: make(chan ports.UpgradeResult), terminal: ports.UpgradeResult{Outcome: ports.UpgradeOutcomeApplied, Done: true}}
				},
			},
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, err)
	defer session.Close()
	model := directUpgradeModel(t, session)
	model, command := updateDirect(model, keyPress('U'))
	model, _ = applyDirectCommand(t, model, command)
	model.upgradePrompt.input.SetValue("installer:v1.15.0")
	model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	changed.Store(true)
	model, command = updateDirect(model, keyPress('y'))
	for model.mutation.busy {
		model, command = updateDirect(model, command())
	}
	assert.Zero(t, dispatches.Load(), "assessment change is detected before the controller receives UpgradeStream")
	assert.Contains(t, model.mutation.notice, "assessment changed")
	assert.NotContains(t, model.mutation.notice, "unconfirmed")
	assert.NotContains(t, model.mutation.notice, "remote outcome")
	assert.Contains(t, ansi.Strip(model.View().Content), "assessment changed")
}

func TestDirectUpgradeStatusOutranksInactiveFilterButPreservesEditingPrompt(t *testing.T) {
	model := directUpgradeModel(t, directUpgradeSession(t, "installer:v1.14.0", nil, nil))
	model.mutation.operation, model.mutation.busy = 7, true
	model.nodes.filter = "cp"
	model, _ = updateDirect(model, directUpgradeProgress{operation: 7, generation: model.generation(), target: "cp-1", event: ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulling installer", Current: 1, Total: 2}})
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "Upgrading cp-1: pulling 50%")
	model.nodes.filtering = true
	model.nodes.filter = "worker"
	assert.Contains(t, ansi.Strip(model.View().Content), "/worker", "active filter entry remains the footer prompt")
	model.nodes.filtering = false
	model.nodes.filter = "cp"
	recovery := ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAppliedWithRecoveryWarning, Warning: "private adapter detail", Err: errors.New("private adapter detail"), Done: true}
	model, _ = updateDirect(model, directMutationFinished{operation: 7, generation: model.generation(), upgradeTarget: "cp-1", upgrade: &recovery})
	content = ansi.Strip(model.View().Content)
	assert.Contains(t, content, "recovery warning")
	assert.NotContains(t, content, "private adapter detail")
}

type gatedDirectUpgradeStream struct {
	results  <-chan ports.UpgradeResult
	release  <-chan struct{}
	terminal ports.UpgradeResult
}

type cancelObservedUpgradeStream struct {
	results      chan ports.UpgradeResult
	cancelled    chan struct{}
	waitStarted  chan struct{}
	waitFinished chan struct{}
	release      chan struct{}
}

func newCancelObservedUpgradeStream() *cancelObservedUpgradeStream {
	return &cancelObservedUpgradeStream{results: make(chan ports.UpgradeResult, 1), cancelled: make(chan struct{}), waitStarted: make(chan struct{}), waitFinished: make(chan struct{}), release: make(chan struct{})}
}

func (s *cancelObservedUpgradeStream) Results() <-chan ports.UpgradeResult { return s.results }
func (s *cancelObservedUpgradeStream) Cancel()                             { close(s.cancelled) }
func (s *cancelObservedUpgradeStream) Wait(ctx context.Context) (ports.UpgradeResult, error) {
	close(s.waitStarted)
	defer close(s.waitFinished)
	select {
	case <-s.release:
		return ports.UpgradeResult{Outcome: ports.UpgradeOutcomeAccepted, Err: context.Canceled, Done: true}, nil
	case <-ctx.Done():
		return ports.UpgradeResult{}, ctx.Err()
	}
}

func directUpgradeWorkForTest(t *testing.T, model directModel, session *application.Session, ctx context.Context) directMutationWork {
	t.Helper()
	return directMutationWork{operation: 3, generation: model.generation(), ctx: ctx, session: session, plan: &directMutationPrompt{
		operation: 3, generation: model.generation(), nodeKind: application.ActionUpgrade, node: mustPrepareUpgradeForDirectTest(t, session),
		assessment: application.Assessment{Targets: []string{"cp-1"}}, confirm: application.Confirmation{Approved: true}, session: session,
	}, events: make(chan directMutationWorkItem, 2)}
}

func (s *gatedDirectUpgradeStream) Results() <-chan ports.UpgradeResult { return s.results }
func (s *gatedDirectUpgradeStream) Cancel()                             {}
func (s *gatedDirectUpgradeStream) Wait(ctx context.Context) (ports.UpgradeResult, error) {
	select {
	case <-s.release:
		return s.terminal, nil
	case <-ctx.Done():
		return ports.UpgradeResult{}, ctx.Err()
	}
}

func mustPrepareUpgradeForDirectTest(t *testing.T, session *application.Session) *application.PreparedNodeAction {
	t.Helper()
	plan, err := session.PrepareNodeAction(context.Background(), application.NodeActionRequest{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "installer:v1.15.0"})
	require.NoError(t, err)
	return plan
}
