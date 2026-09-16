package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func diskWipeTestModel(t *testing.T, writesEnabled bool) model {
	t.Helper()
	appModel, _ := application.NewModel("prod")
	appModel.WritesEnabled = writesEnabled
	appModel.Disks = application.DisksState{Status: application.Ready, Node: "cp-1", Value: domain.DiskSet{Disks: []domain.DiskSnapshot{
		{DeviceName: "/dev/sda", SystemDisk: true},
		{DeviceName: "/dev/sdb"},
	}}}
	root := newModel(t.Context(), false, appModel, application.NewRunner(application.Dependencies{}))
	root.views = root.views.replaceRoot(viewFrame{Kind: viewDisks, Label: "disks"})
	root.disks = root.disks.setState(root.application.Disks)
	return root
}

func TestDiskWipeKeyWithWritesDisabledIsInert(t *testing.T) {
	root := diskWipeTestModel(t, false)

	updated, cmd := root.Update(keyPress('W'))

	assert.Nil(t, updated.(model).diskWipePrompt)
	assert.Nil(t, cmd)
}

func TestDiskWipeKeyOpensTypedDevicePrompt(t *testing.T) {
	root := diskWipeTestModel(t, true)
	// Select the non-system disk (/dev/sdb).
	root.disks = root.disks.update(keyPress('j'))

	updated, cmd := root.Update(keyPress('W'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.diskWipePrompt)
	assert.Equal(t, "cp-1", rootModel.diskWipePrompt.node)
	assert.Equal(t, "/dev/sdb", rootModel.diskWipePrompt.device)
	assert.NotNil(t, cmd, "opening the prompt must focus the text input")
}

func TestDiskWipeKeyRefusesSystemDisk(t *testing.T) {
	root := diskWipeTestModel(t, true) // cursor starts on the system disk

	updated, cmd := root.Update(keyPress('W'))
	rootModel := updated.(model)

	assert.Nil(t, rootModel.diskWipePrompt)
	assert.Nil(t, cmd)
	assert.Contains(t, rootModel.notice, "system disk")
	assert.Nil(t, rootModel.application.PendingAction)
}

func TestDiskWipeKeyRefusesWhenInventoryNotLoaded(t *testing.T) {
	root := diskWipeTestModel(t, true)
	root.application.Disks.Status = application.Loading // stale rows must not authorize a wipe

	updated, _ := root.Update(keyPress('W'))

	assert.Nil(t, updated.(model).diskWipePrompt)
}

func TestDiskWipePromptEnterOpensPendingAction(t *testing.T) {
	root := diskWipeTestModel(t, true)
	root.disks = root.disks.update(keyPress('j'))
	updated, _ := root.Update(keyPress('W'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.diskWipePrompt)
	rootModel.diskWipePrompt.input.SetValue("/dev/sdb")

	updated, _ = rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rootModel = updated.(model)

	require.Nil(t, rootModel.diskWipePrompt, "Enter must close the prompt")
	require.NotNil(t, rootModel.application.PendingAction)
	assert.Equal(t, application.ActionWipeDevice, rootModel.application.PendingAction.Kind)
	require.NotNil(t, rootModel.application.PendingAction.DeviceWipe)
	assert.Equal(t, "/dev/sdb", rootModel.application.PendingAction.DeviceWipe.Device)
	assert.Equal(t, ports.DeviceWipeFast, rootModel.application.PendingAction.DeviceWipe.Method)
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
}

func TestDiskWipePromptRequiresExactDevice(t *testing.T) {
	root := diskWipeTestModel(t, true)
	root.disks = root.disks.update(keyPress('j'))
	updated, _ := root.Update(keyPress('W'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.diskWipePrompt)
	rootModel.diskWipePrompt.input.SetValue("/dev/sdc")

	updated, _ = rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rootModel = updated.(model)

	assert.Nil(t, rootModel.application.PendingAction)
	require.NotNil(t, rootModel.diskWipePrompt, "a mismatched device token must keep the prompt open")
	assert.NotEmpty(t, rootModel.diskWipePrompt.err)
}

func TestDiskWipePromptEscCancels(t *testing.T) {
	root := diskWipeTestModel(t, true)
	root.disks = root.disks.update(keyPress('j'))
	updated, _ := root.Update(keyPress('W'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.diskWipePrompt)

	updated, _ = rootModel.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	assert.Nil(t, updated.(model).diskWipePrompt)
	assert.Nil(t, updated.(model).application.PendingAction)
}

func TestDiskWipeActionHintsOnlyWhenWritesEnabled(t *testing.T) {
	assert.NotContains(t, hintKeys(actionHints(viewDisks, false)), "W")
	assert.Contains(t, hintKeys(actionHints(viewDisks, true)), "W")
}

func TestContextSwitchClearsDiskWipePrompt(t *testing.T) {
	root := diskWipeTestModel(t, true)
	root.disks = root.disks.update(keyPress('j'))
	updated, _ := root.Update(keyPress('W'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.diskWipePrompt)

	updated, _ = rootModel.Update(applicationMessage{message: application.SelectContext{Name: "other"}})

	assert.Nil(t, updated.(model).diskWipePrompt)
}

func TestRenderPendingActionPromptWipeVerb(t *testing.T) {
	prompt := renderPendingActionPrompt(application.PendingAction{
		Kind:       application.ActionWipeDevice,
		Targets:    []string{"cp-1"},
		DeviceWipe: &ports.DeviceWipeOptions{Node: "cp-1", Device: "/dev/sdb"},
	})

	assert.Contains(t, prompt, "Wipe device")
	assert.Contains(t, prompt, "(y/n)")
	assert.LessOrEqual(t, len([]rune(prompt)), 80)
}
