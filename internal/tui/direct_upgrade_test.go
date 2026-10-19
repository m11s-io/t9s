package tui

import (
	"context"
	"errors"
	"testing"

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

type gatedDirectUpgradeStream struct {
	results  <-chan ports.UpgradeResult
	release  <-chan struct{}
	terminal ports.UpgradeResult
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
