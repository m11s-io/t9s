package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func disksTestState() application.DisksState {
	return application.DisksState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.DiskSet{Disks: []domain.DiskSnapshot{
			{DeviceName: "sda", Type: "ssd", SizeBytes: 536870912000, Model: "SAMSUNG MZVL", SystemDisk: true},
			{DeviceName: "sdb", Type: "hdd", SizeBytes: 1073741824000, Model: "WDC WD10"},
		}},
	}
}

func renderDisks(width int, state application.DisksState) string {
	return newDisksModel(state).view(width)
}

func TestDisksRenderSemanticColumns(t *testing.T) {
	rendered := renderDisks(120, disksTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"DEVICE", "TYPE", "SIZE", "MODEL", "SYSTEM"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "sda")
	assert.Contains(t, ansi.Strip(lines[1]), "ssd")
	assert.Contains(t, ansi.Strip(lines[1]), "500.0 GiB")
	assert.Contains(t, ansi.Strip(lines[1]), "SAMSUNG MZVL")
}

func TestDisksRenderEmptyState(t *testing.T) {
	model := newDisksModel(application.DisksState{Status: application.Ready})
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No disks", rendered)
}

func TestDisksFilterMatchesDeviceName(t *testing.T) {
	disks := newDisksModel(disksTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('s'), keyPress('d'), keyPress('b'),
		{Code: tea.KeyEnter},
	} {
		disks = disks.update(message)
	}

	rendered := disks.view(120)

	assert.Contains(t, rendered, "sdb")
	assert.NotContains(t, rendered, "sda")
}
