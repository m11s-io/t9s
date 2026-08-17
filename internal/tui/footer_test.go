package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestK9sFooterPlacesStatusAboveBottomCrumbTab(t *testing.T) {
	footer := renderK9sFooter(80, "test > services", "", "services unavailable", defaultK9sSkin())
	lines := strings.Split(footer, "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "services unavailable")
	assert.Contains(t, lines[1], "<services>")
	assert.Equal(t, 80, ansi.StringWidth(lines[0]))
	assert.Equal(t, 80, ansi.StringWidth(lines[1]))
}

func TestK9sFooterPromptTakesTheStatusRow(t *testing.T) {
	footer := renderK9sFooter(60, "prod > nodes", "COMMAND :svc", "ignored", defaultK9sSkin())
	assert.Contains(t, strings.Split(footer, "\n")[0], "COMMAND :svc")
	assert.NotContains(t, footer, "ignored")
}

func TestRenderUpgradeNoticeShowsBoundedKnownProgress(t *testing.T) {
	notice := renderUpgradeNotice(application.UpgradeState{
		Active: true,
		Target: "worker-2",
		Event:  ports.UpgradeEvent{Phase: ports.UpgradePulling, Message: "pulling installer", Current: 125, Total: 100},
	})

	assert.Equal(t, "Upgrading worker-2: pulling 100% — pulling installer", notice)
}

func TestRenderUpgradeNoticeOmitsPercentageWithoutATotal(t *testing.T) {
	notice := renderUpgradeNotice(application.UpgradeState{
		Active: true,
		Target: "worker-2",
		Event:  ports.UpgradeEvent{Phase: ports.UpgradeInstalling, Message: "installing"},
	})

	assert.Equal(t, "Upgrading worker-2: installing — installing", notice)
}

func TestRenderUpgradeNoticeKeepsFailedPhaseAndCancellation(t *testing.T) {
	notice := renderUpgradeNotice(application.UpgradeState{
		Target: "worker-2",
		Event:  ports.UpgradeEvent{Phase: ports.UpgradeRebooting},
		Err:    "context canceled",
	})

	assert.Equal(t, "Upgrade worker-2 failed during rebooting: context canceled", notice)
}
