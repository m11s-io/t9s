package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetTestRoot(t *testing.T, controller *testkit.FakeNodeController, diskReader ports.DiskReader) model {
	t.Helper()
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = true
	appModel, _ = application.Update(appModel, application.SessionOpened{
		Generation:     appModel.Generation,
		NodeController: controller,
		Disks:          diskReader,
	})
	appModel, _ = application.Update(appModel, application.NodesLoaded{
		Generation: appModel.Generation,
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "n1", Name: "cp-1", Role: domain.NodeRoleControl},
			{ID: "n2", Name: "worker-1", Role: domain.NodeRoleWorker},
		}},
	})
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))
	root.nodes = root.nodes.setState(root.application.Nodes)
	return root
}

func sampleDiskReader() *testkit.FakeDiskReader {
	return &testkit.FakeDiskReader{ListFunc: func(context.Context, string) (domain.DiskSet, error) {
		return domain.DiskSet{Disks: []domain.DiskSnapshot{
			{DeviceName: "sda", SystemDisk: true},
			{DeviceName: "sdb"},
		}}, nil
	}}
}

func openResetPrompt(t *testing.T, root model) model {
	t.Helper()
	updated, cmd := root.Update(keyPress('W'))
	require.NotNil(t, cmd)
	msg := cmd()
	updated, _ = updated.(model).Update(msg)
	root = updated.(model)
	require.NotNil(t, root.resetPrompt)
	return root
}

func TestResetKeyWithWritesDisabledIsInert(t *testing.T) {
	appModel, _ := application.NewModel("prod")
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))

	updated, cmd := root.Update(keyPress('W'))

	assert.Nil(t, updated.(model).resetPrompt)
	assert.Nil(t, cmd)
}

func TestResetKeyWithWritesEnabledRequestsPreview(t *testing.T) {
	root := openResetPrompt(t, resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader()))

	assert.Equal(t, []string{"cp-1"}, root.resetPrompt.targets)
	assert.Contains(t, root.resetPrompt.view(contentSize{Width: 80, Height: 24}), "sdb")
}

func TestResetPromptRequiresExactToken(t *testing.T) {
	root := openResetPrompt(t, resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader()))
	root.resetPrompt.input.SetValue("cp-2")

	updated, _ := root.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	root = updated.(model)

	assert.Nil(t, root.application.PendingAction)
	require.NotNil(t, root.resetPrompt, "a mismatched token must keep the overlay open")
	assert.NotEmpty(t, root.resetPrompt.err)
}

func TestResetPromptEnterOpensPendingActionWithOptions(t *testing.T) {
	root := openResetPrompt(t, resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader()))
	root.application.Etcd = application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", StatusKnown: true},
		{Hostname: "cp-2", StatusKnown: true},
		{Hostname: "cp-3", StatusKnown: true},
	}}}
	root.resetPrompt.options.Graceful = true
	root.resetPrompt.input.SetValue("cp-1")

	updated, _ := root.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	root = updated.(model)

	require.NotNil(t, root.application.PendingAction)
	assert.Equal(t, application.ActionReset, root.application.PendingAction.Kind)
	require.NotNil(t, root.application.PendingAction.Reset)
	assert.True(t, root.application.PendingAction.Reset.Graceful)
	assert.Equal(t, []string{"cp-1"}, root.application.PendingAction.Targets)
	assert.Contains(t, root.activePrompt(), "(y/n)")
	assert.Nil(t, root.resetPrompt)
}

func TestResetPromptCarriesUserDisksFromPreview(t *testing.T) {
	root := openResetPrompt(t, resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader()))
	root.application.Etcd = application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", StatusKnown: true},
		{Hostname: "cp-2", StatusKnown: true},
		{Hostname: "cp-3", StatusKnown: true},
	}}}
	root.resetPrompt.options.Mode = ports.WipeModeUserDisks
	root.resetPrompt.input.SetValue("cp-1")

	updated, _ := root.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	root = updated.(model)

	require.NotNil(t, root.application.PendingAction)
	require.NotNil(t, root.application.PendingAction.Reset)
	assert.Empty(t, root.application.PendingAction.Blocked)
	assert.Equal(t, []string{"sdb"}, root.application.PendingAction.Reset.UserDisks,
		"the overlay preview must populate the user disks Talos will actually wipe")
}

func TestResetConfirmBlockedWhenQuorumDegraded(t *testing.T) {
	root := openResetPrompt(t, resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader()))
	root.application.Etcd = application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", StatusKnown: true},
		{Hostname: "cp-2", StatusKnown: true},
		{Hostname: "cp-3", StatusKnown: true},
	}}}
	root.resetPrompt.input.SetValue("cp-1")
	updated, _ := root.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	root = updated.(model)
	require.NotNil(t, root.application.PendingAction)
	require.Empty(t, root.application.PendingAction.Blocked)

	root.application.Etcd = application.EtcdState{Status: application.Loading}
	updated, cmd := root.Update(keyPress('y'))
	root = updated.(model)

	assert.Nil(t, cmd, "a degraded-quorum reset confirm must produce no effect")
	require.NotNil(t, root.application.PendingAction)
	assert.NotEmpty(t, root.application.PendingAction.Blocked)
}

func TestResetPromptEscCancelsAndNeverPends(t *testing.T) {
	root := openResetPrompt(t, resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader()))

	updated, _ := root.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	root = updated.(model)

	assert.Nil(t, root.resetPrompt)
	assert.Nil(t, root.application.PendingAction)
}

func TestResetPromptOpenedIgnoredWhileAnotherConfirmOpen(t *testing.T) {
	root := resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader())
	root.application.PendingAction = &application.PendingAction{Kind: application.ActionReboot, Targets: []string{"cp-1"}}

	updated, _ := root.Update(applicationMessage{message: application.ResetPromptOpened{
		Generation: root.application.Generation,
		Targets:    []string{"cp-1"},
	}})

	assert.Nil(t, updated.(model).resetPrompt)
}

func TestActionHintsResetOnlyWhenWritesEnabled(t *testing.T) {
	assert.NotContains(t, hintKeys(actionHints(viewNodes, false)), "W")
	assert.Contains(t, hintKeys(actionHints(viewNodes, true)), "W")
}

func TestContextSwitchClearsResetPrompt(t *testing.T) {
	root := openResetPrompt(t, resetTestRoot(t, &testkit.FakeNodeController{}, sampleDiskReader()))

	updated, _ := root.Update(applicationMessage{message: application.SelectContext{Name: "other"}})

	assert.Nil(t, updated.(model).resetPrompt)
}

func TestRenderPendingActionPromptResetVerb(t *testing.T) {
	preview := application.BuildResetPreview("cp-1", domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "sda", SystemDisk: true}}})
	prompt := renderPendingActionPrompt(application.PendingAction{
		Kind:         application.ActionReset,
		Targets:      []string{"cp-1"},
		Reset:        &ports.ResetOptions{Mode: ports.WipeModeAll},
		ResetPreview: &preview,
	})

	assert.Contains(t, prompt, "Reset")
	assert.Contains(t, prompt, "(y/n)")
	assert.LessOrEqual(t, len([]rune(prompt)), 80)
}
