package tui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writesEnabledTestModel wires a real application.Runner (not nil) because
// the confirm ("y") path's tea.Cmd closures call m.runner.Run(...) when
// invoked by the test — a nil runner would panic there even though it's
// harmless for the R/X/cancel paths, which never reach m.runner.Run.
func writesEnabledTestModel(t *testing.T, controller *testkit.FakeNodeController) model {
	t.Helper()
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = true
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
			{ID: "n2", Name: "worker-1", Role: domain.NodeRoleWorker},
		}},
	})
	appModel, _ = application.Update(appModel, application.SessionOpened{Generation: appModel.Generation, NodeController: controller})
	runner := application.NewRunner(application.Dependencies{})
	root := newModel(t.Context(), false, appModel, runner)
	root.nodes = root.nodes.setState(root.application.Nodes)
	return root
}

func TestRebootKeyWithWritesDisabledIsInert(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))

	updated, _ := root.Update(keyPress('R'))

	assert.Nil(t, updated.(model).application.PendingAction)
}

func TestRebootKeyWithWritesEnabledOpensConfirmPrompt(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})

	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingAction)
	assert.Equal(t, application.ActionReboot, rootModel.application.PendingAction.Kind)
	assert.Contains(t, rootModel.application.PendingAction.Targets, "cp-1")
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
}

func TestRebootKeyHandlesKittyShiftEncoding(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})

	updated, _ := root.Update(shiftKeyPress('R'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingAction, "shift+r (Kitty protocol, e.g. Ghostty) must open the confirm prompt, same as legacy \"R\"")
	assert.Equal(t, application.ActionReboot, rootModel.application.PendingAction.Kind)
}

func TestShutdownKeyHandlesKittyShiftEncoding(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})

	updated, _ := root.Update(shiftKeyPress('X'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingAction, "shift+x (Kitty protocol, e.g. Ghostty) must open the confirm prompt, same as legacy \"X\"")
	assert.Equal(t, application.ActionShutdown, rootModel.application.PendingAction.Kind)
}

// On the Kitty keyboard protocol (e.g. Ghostty), shifted punctuation is
// reported as the unshifted base key (";") plus a separate Shift modifier
// and Text set to the shifted character (":"), rather than the single ":"
// rune a legacy terminal sends. This is the realistic Kitty-protocol
// fixture for the command-palette key, distinct from shiftKeyPress (which
// models shifted letters, not shifted punctuation).
func TestCommandPaletteKeyHandlesKittyShiftedPunctuationEncoding(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))

	updated, _ := root.Update(tea.KeyPressMsg{Code: ';', Text: ":", Mod: tea.ModShift})
	rootModel := updated.(model)

	assert.True(t, rootModel.palette.active, "shift+; (Kitty protocol) producing \":\" must open the command palette, same as legacy \":\"")
}

func TestSpaceKeyWithWritesDisabledDoesNotMarkRow(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
		}},
	})
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))
	root.nodes = root.nodes.setState(root.application.Nodes)

	updated, _ := root.Update(keyPress(' '))
	rootModel := updated.(model)

	assert.False(t, rootModel.nodes.isMarked("n1"), "space must be inert on the nodes screen while writes are disabled")
}

func TestSpaceKeyWithWritesDisabledStillFiltersWhenFilteringActive(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
		}},
	})
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))
	root.nodes = root.nodes.setState(root.application.Nodes)
	root.nodes = root.nodes.startFilter("cp")

	updated, _ := root.Update(keyPress(' '))
	rootModel := updated.(model)

	assert.Equal(t, "cp ", rootModel.nodes.filter, "space must still reach the active node filter while writes are disabled")
}

func TestSpaceKeyWithWritesEnabledStillMarksRow(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})

	updated, _ := root.Update(keyPress(' '))
	rootModel := updated.(model)

	assert.True(t, rootModel.nodes.isMarked("n1"), "space must still mark rows once writes are enabled")
}

func TestMarksClearOnContextSwitch(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})
	updated, _ := root.Update(keyPress(' '))
	rootModel := updated.(model)
	require.True(t, rootModel.nodes.isMarked("n1"))

	updated, _ = rootModel.Update(applicationMessage{message: application.SelectContext{Name: "dev"}})
	rootModel = updated.(model)

	assert.False(t, rootModel.nodes.isMarked("n1"), "a mark must not survive a context switch and risk cross-cluster mistargeting")
}

func TestMarksClearAfterConfirmedAction(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{
		RebootFunc: func(_ context.Context, _ string, _ ports.RebootMode) error { return nil },
	})
	updated, _ := root.Update(keyPress(' '))
	rootModel := updated.(model)
	require.NotEmpty(t, rootModel.nodes.marked)

	updated, _ = rootModel.Update(keyPress('R'))
	rootModel = updated.(model)
	require.NotNil(t, rootModel.application.PendingAction)

	updated, _ = rootModel.Update(keyPress('y'))
	rootModel = updated.(model)

	assert.Empty(t, rootModel.nodes.marked, "marks must be cleared once a confirmed action has fired so a second R/X doesn't re-target already-actioned nodes")
}

func TestRenderPendingActionPromptSurvivesTruncationWithLongTargetsAndWarning(t *testing.T) {
	targets := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		targets = append(targets, fmt.Sprintf("control-plane-node-with-a-long-hostname-%02d", i))
	}
	pending := application.PendingAction{
		Kind:    application.ActionShutdown, // longest verb
		Targets: targets,
		// The actual longest warning computeActionWarning produces (the
		// etcd quorum-loss message).
		Warning: "control-plane node(s); would drop etcd to 1/3 — below quorum (need 2)",
	}

	prompt := renderPendingActionPrompt(pending)
	require.LessOrEqual(t, len(prompt), 80, "the prompt itself must fit an 80-column terminal without relying on footer truncation")
	truncated := fitK9sCell(prompt, 80)

	assert.Contains(t, truncated, "(y/n)")
	assert.Contains(t, truncated, "below quorum")
}

func TestRenderPendingActionPromptShortWarningIsUnaffected(t *testing.T) {
	pending := application.PendingAction{
		Kind:    application.ActionReboot,
		Targets: []string{"worker-1"},
		Warning: "control-plane node(s)",
	}

	prompt := renderPendingActionPrompt(pending)

	assert.Equal(t, "!! control-plane node(s) — Reboot 1 node(s)? (y/n)", prompt)
}

func TestNonYKeyCancelsPendingAction(t *testing.T) {
	root := writesEnabledTestModel(t, &testkit.FakeNodeController{})
	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	updated, _ = rootModel.Update(keyPress('n'))
	rootModel = updated.(model)

	assert.Nil(t, rootModel.application.PendingAction)
}

func TestConfirmingRebootRunsControllerAndReportsSuccess(t *testing.T) {
	controller := &testkit.FakeNodeController{
		RebootFunc: func(_ context.Context, _ string, _ ports.RebootMode) error { return nil },
	}
	root := writesEnabledTestModel(t, controller)
	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	updated, cmd := rootModel.Update(keyPress('y'))
	rootModel = updated.(model)
	require.NotNil(t, cmd)
	msg := cmd()
	updated, _ = rootModel.Update(msg)
	rootModel = updated.(model)

	assert.Nil(t, rootModel.application.PendingAction)
	assert.Contains(t, rootModel.notice, "1/1 succeeded")
}

func TestConfirmingRebootReportsFailureWithTargetAndError(t *testing.T) {
	controller := &testkit.FakeNodeController{
		RebootFunc: func(_ context.Context, _ string, _ ports.RebootMode) error { return errors.New("connection refused") },
	}
	root := writesEnabledTestModel(t, controller)
	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)

	updated, cmd := rootModel.Update(keyPress('y'))
	rootModel = updated.(model)
	msg := cmd()
	updated, _ = rootModel.Update(msg)
	rootModel = updated.(model)

	assert.Contains(t, rootModel.notice, "cp-1")
	assert.Contains(t, rootModel.notice, "connection refused")
}

func TestActionResultsFooterDenominatorIsConfirmedTargetCountNotResultsSoFar(t *testing.T) {
	controller := &testkit.FakeNodeController{
		RebootFunc: func(_ context.Context, _ string, _ ports.RebootMode) error { return nil },
	}
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = true
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
			{ID: "n2", Name: "cp-2", Role: domain.NodeRoleControl},
			{ID: "n3", Name: "cp-3", Role: domain.NodeRoleControl},
		}},
	})
	appModel, _ = application.Update(appModel, application.SessionOpened{Generation: appModel.Generation, NodeController: controller})
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))
	root.nodes = root.nodes.setState(root.application.Nodes)
	root.nodes = root.nodes.update(keyPress(' '))
	root.nodes = root.nodes.moveSelection(1)
	root.nodes = root.nodes.update(keyPress(' '))
	root.nodes = root.nodes.moveSelection(1)
	root.nodes = root.nodes.update(keyPress(' '))

	updated, _ := root.Update(keyPress('R'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.application.PendingAction)
	require.Len(t, rootModel.application.PendingAction.Targets, 3)

	updated, cmd := rootModel.Update(keyPress('y'))
	rootModel = updated.(model)
	require.NotNil(t, cmd)

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok, "confirming a 3-target bulk action must batch 3 independent effects")
	require.Len(t, batch, 3)

	// Only the first of three results has arrived; the footer must still
	// report against the full confirmed target count (3), not len(results)
	// (which would misleadingly read "1/1 succeeded" as if fully complete).
	firstResult := batch[0]()
	updated, _ = rootModel.Update(firstResult)
	rootModel = updated.(model)

	assert.Contains(t, rootModel.notice, "1/3 succeeded")
	assert.NotContains(t, rootModel.notice, "1/1 succeeded")
}
