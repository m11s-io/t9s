package tui

import (
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/application"
)

// renderUpgradeNotice creates the compact global upgrade status shown while an
// upgrade is running and after it fails. The adapter only reports byte totals
// when Talos supplies them, so a percentage is intentionally absent otherwise.
func renderUpgradeNotice(upgrade application.UpgradeState) string {
	if upgrade.Active {
		phase := string(upgrade.Event.Phase)
		if phase == "" {
			phase = "starting"
		}
		progress := phase
		if percent, known := upgradePercent(upgrade.Event.Current, upgrade.Event.Total); known {
			progress = fmt.Sprintf("%s %d%%", phase, percent)
		}
		notice := fmt.Sprintf("Upgrading %s: %s", fallback(upgrade.Target), progress)
		if upgrade.Event.Message != "" {
			notice += " — " + upgrade.Event.Message
		}
		return notice
	}
	if upgrade.Err == "" {
		return ""
	}
	phase := string(upgrade.Event.Phase)
	if phase == "" {
		phase = "upgrade"
	}
	return fmt.Sprintf("Upgrade %s failed during %s: %s", fallback(upgrade.Target), phase, upgrade.Err)
}

// upgradePercent returns a percentage only when Talos supplied a positive byte
// total. Its result is clamped so malformed or out-of-order progress cannot
// claim more than 100%% or less than 0%%.
func upgradePercent(current, total int64) (int64, bool) {
	if total <= 0 {
		return 0, false
	}
	if current <= 0 {
		return 0, true
	}
	if current >= total {
		return 100, true
	}
	return (current/total)*100 + (current%total)*100/total, true
}

func renderK9sFooter(width int, breadcrumb, prompt, flash string, skin k9sSkin) string {
	status := flash
	if prompt != "" {
		status = prompt
	}
	statusLine := fitK9sCell(skin.prompt.Render(status), width)
	parts := strings.Split(breadcrumb, " > ")
	active := ""
	if len(parts) > 0 {
		active = strings.TrimSpace(parts[len(parts)-1])
	}
	crumbLine := skin.crumbActive.Render(" <" + active + "> ")
	return statusLine + "\n" + fitK9sCell(crumbLine, width)
}
