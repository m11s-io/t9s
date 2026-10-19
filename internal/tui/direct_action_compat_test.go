package tui

import (
	"context"
	"strings"
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

func TestDirectSpaceKeyKeepsDisabledMarksInertAndFiltersSpace(t *testing.T) {
	nodes := []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}, {ID: "worker-id", Name: "worker 1", Role: domain.NodeRoleWorker}}
	model := newDirectNodesModel(t, LaunchOptions{}, nodes)
	model, _ = updateDirect(model, keyPress(' '))
	assert.Empty(t, model.nodes.marked)
	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress(' '))
	assert.Equal(t, " ", model.nodes.filter, "disabled space remains ordinary filter text")
	assert.Empty(t, model.nodes.marked)

	writable := newDirectNodesModel(t, LaunchOptions{WritesEnabled: true}, nodes)
	writable, _ = updateDirect(writable, keyPress(' '))
	assert.True(t, writable.nodes.isMarked("cp-id"))
}

func TestDirectNodeActionConfirmationCallsControllerAndKeepsTargetInResult(t *testing.T) {
	var gotTarget string
	session := directMutationSession(t, true, domain.ServiceSet{}, func(_ context.Context, target string, _ ports.RebootMode) error {
		gotTarget = target
		return nil
	})
	model := newDirectNodesModel(t, LaunchOptions{WritesEnabled: true}, []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl}})
	model.session, model.sessionCtx = session, context.Background()
	model, command := updateDirect(model, keyPress('R'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.Contains(t, ansi.Strip(model.View().Content), "Reboot cp-1? (y/n)")
	model, command = updateDirect(model, keyPress('y'))
	require.NotNil(t, command)
	for model.mutation.busy {
		model, command = applyDirectCommand(t, model, command)
	}
	assert.Equal(t, "cp-1", gotTarget)
	assert.Contains(t, model.mutation.notice, "cp-1")
}

func TestDirectDiskWipeStaleVisibleSelectionIsBlockedByCurrentInventory(t *testing.T) {
	listed := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sdb"}}}
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) {
		return domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sda", SystemDisk: true}}}, nil
	}, &testkit.FakeNodeController{}, true)
	model.sessionCtx = context.Background()
	model.disks = newDirectDisksModel().setResult("cp-1", listed, nil)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewDisks, Label: "cp-1 > disks", NodeTarget: "cp-1", NodeID: "cp-id"})
	model, command := updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	assert.NotEmpty(t, model.mutation.prompt.assessment.Blocked)
	assert.NotContains(t, ansi.Strip(model.View().Content), "Type \"/dev/sdb\"")
	model, command = updateDirect(model, keyPress('y'))
	assert.Nil(t, command)
	assert.NotNil(t, model.mutation.prompt)
}

func TestDirectDiskWipeDoesNotStartFromLoadingInventoryAndKeepsMismatchVisible(t *testing.T) {
	disks := domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "/dev/sdb"}}}
	model := newModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.session = directDestructiveSession(t, func(context.Context, string) (domain.DiskSet, error) { return disks, nil }, &testkit.FakeNodeController{}, true)
	model.sessionCtx = context.Background()
	model.disks = newDirectDisksModel()
	model.disks.node, model.disks.values = "cp-1", disks
	model.views = model.views.replaceRoot(viewFrame{Kind: viewDisks, Label: "cp-1 > disks", NodeTarget: "cp-1", NodeID: "cp-id"})
	model, command := updateDirect(model, keyPress('W'))
	assert.Nil(t, command)
	assert.Nil(t, model.mutation.prompt)

	model.disks.status = directReady
	model, command = updateDirect(model, keyPress('W'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	require.NotNil(t, model.mutation.prompt)
	model, _ = updateDirect(model, keyPress('x'))
	model.mutation.prompt.intent.SetValue("/dev/sdc")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.NotNil(t, model.mutation.prompt)
	assert.Contains(t, model.mutation.prompt.err, "does not match")
}

func TestDirectWriteActionHintsAreRenderedOnlyWhenEnabled(t *testing.T) {
	for _, test := range []struct {
		name string
		kind viewKind
		key  string
	}{{"wipe", viewDisks, "Wipe"}, {"etcd snapshot", viewEtcd, "Snapshot"}, {"etcd remove", viewEtcd, "Remove"}, {"etcd leave", viewEtcd, "Leave"}, {"etcd defragment", viewEtcd, "Defragment"}, {"etcd disarm", viewEtcd, "Disarm"}, {"etcd forfeit", viewEtcd, "Forfeit"}, {"service start", viewServices, "Start"}, {"service stop", viewServices, "Stop"}, {"service restart", viewServices, "Restart"}} {
		t.Run(test.name, func(t *testing.T) {
			hasHint := func(enabled bool) bool {
				for _, hint := range directActionHints(test.kind, enabled) {
					if hint.Label == test.key {
						return true
					}
				}
				return false
			}
			assert.False(t, hasHint(false))
			assert.True(t, hasHint(true))
			if test.kind == viewEtcd {
				for _, writes := range []bool{false, true} {
					model := directEtcdModel(directEtcdMutationSession(t, writes, &testkit.FakeEtcdOperations{}), writes)
					content := ansi.Strip(model.View().Content)
					if writes {
						assert.Contains(t, content, test.key)
					} else {
						assert.NotContains(t, content, test.key)
					}
				}
			}
		})
	}
}

func TestDirectMutationPromptKeepsWarningAndConfirmationCueVisible(t *testing.T) {
	warning := strings.Repeat("node may lose quorum and become unavailable ", 2)
	prompt := &directMutationPrompt{node: &application.PreparedNodeAction{}, nodeKind: application.ActionReboot,
		assessment: application.Assessment{Targets: []string{"cp-1", "cp-2"}, Warning: warning}}
	rendered := renderDirectMutationPrompt(prompt)
	assert.Contains(t, rendered, "!! ")
	assert.Contains(t, rendered, "(y/n)")
	assert.LessOrEqual(t, len([]rune(rendered)), 80)
}

func TestDirectUpgradePromptKeepsVersionRiskAndConfirmationVisible(t *testing.T) {
	t.Run("long installer reference and quorum warning", func(t *testing.T) {
		image := "factory.talos.dev/metal-installer/very-long-board-name:v1.24.3"
		warning := strings.Repeat("upgrade may cause temporary node unavailability; ", 3) + "below quorum (need 2)"
		prompt := &directMutationPrompt{
			node: &application.PreparedNodeAction{}, nodeKind: application.ActionUpgrade,
			label:      image,
			assessment: application.Assessment{Targets: []string{"cp-1", "cp-2", "cp-3"}, Warning: warning},
		}
		rendered := renderDirectMutationPrompt(prompt)
		assert.LessOrEqual(t, len([]rune(rendered)), 80)
		assert.Contains(t, rendered, "v1.24.3")
		assert.Contains(t, rendered, "below quorum (need 2)")
		assert.Contains(t, rendered, "3 nodes")
		assert.Contains(t, rendered, "(y/n)")
	})

	t.Run("short image and single target remain readable", func(t *testing.T) {
		prompt := &directMutationPrompt{
			node: &application.PreparedNodeAction{}, nodeKind: application.ActionUpgrade,
			label:      "installer:v1.15.0",
			assessment: application.Assessment{Targets: []string{"cp-1"}, Warning: "skips intermediate minor releases"},
		}
		rendered := renderDirectMutationPrompt(prompt)
		assert.LessOrEqual(t, len([]rune(rendered)), 80)
		assert.Contains(t, rendered, "v1.15.0")
		assert.Contains(t, rendered, "cp-1")
		assert.Contains(t, rendered, "skips intermediate minor releases")
		assert.Contains(t, rendered, "(y/n)")
	})
}
