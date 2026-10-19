package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mountsTestState() domain.MountSet {
	return domain.MountSet{Mounts: []domain.MountSnapshot{
		{Filesystem: "/dev/sda1", MountedOn: "/", SizeBytes: 1000, UsedBytes: 600, AvailableBytes: 400, UsedPercent: 60},
		{Filesystem: "tmpfs", MountedOn: "/var", SizeBytes: 2048, UsedBytes: 1024, AvailableBytes: 1024, UsedPercent: 50},
	}}
}

func renderMounts(width int, state domain.MountSet) string {
	return newDirectMountsModel().setResult("cp-1", state, nil).view(width)
}

func TestMountsRenderSemanticColumns(t *testing.T) {
	rendered := renderMounts(120, mountsTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"FILESYSTEM", "SIZE", "USED", "AVAIL", "USE%", "MOUNTED"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "/dev/sda1")
	assert.Contains(t, ansi.Strip(lines[1]), "60%")
}

func TestMountsRenderEmptyState(t *testing.T) {
	model := newDirectMountsModel().setResult("cp-1", domain.MountSet{}, nil)
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No mounts", rendered)
}

func TestMountsFilterMatchesMountedOn(t *testing.T) {
	mounts := newDirectMountsModel().setResult("cp-1", mountsTestState(), nil)
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('/'), keyPress('v'), keyPress('a'), keyPress('r'),
		{Code: tea.KeyEnter},
	} {
		mounts = mounts.update(message)
	}

	rendered := mounts.view(120)

	assert.Contains(t, rendered, "/var")
	assert.NotContains(t, rendered, "/dev/sda1")
}
