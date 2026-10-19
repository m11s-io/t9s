package tui

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func renderDirectUpgradeNotice(upgrade directUpgradeDisplayState) string {
	return renderUpgradeStatus(upgrade.Active, upgrade.Target, upgrade.Event.Phase, upgrade.Event.Message, upgrade.Event.Current, upgrade.Event.Total, upgrade.Err, upgrade.Warning, upgrade.Outcome)
}

func renderUpgradeStatus(active bool, target string, eventPhase ports.UpgradePhase, eventMessage string, current, total int64, errMessage, warning string, outcome domain.OperationOutcome) string {
	if active {
		phase := sanitizeUntrustedText(string(eventPhase))
		if phase == "" {
			phase = "starting"
		}
		progress := phase
		if percent, known := upgradePercent(current, total); known {
			progress = fmt.Sprintf("%s %d%%", phase, percent)
		}
		notice := fmt.Sprintf("Upgrading %s: %s", fallback(sanitizeUntrustedText(target)), progress)
		if eventMessage != "" {
			notice += " — " + sanitizeUntrustedText(eventMessage)
		}
		return notice
	}
	if warning != "" {
		return fmt.Sprintf("Upgrade %s Applied — recovery warning: %s", fallback(sanitizeUntrustedText(target)), sanitizeUntrustedText(warning))
	}
	if outcome == domain.OperationAccepted {
		return fmt.Sprintf("Upgrade %s accepted — completion not verified", fallback(sanitizeUntrustedText(target)))
	}
	if outcome == domain.OperationCompleted {
		return fmt.Sprintf("Upgrade %s applied", fallback(sanitizeUntrustedText(target)))
	}
	if errMessage == "" {
		return ""
	}
	phase := sanitizeUntrustedText(string(eventPhase))
	if phase == "" {
		phase = "upgrade"
	}
	if outcome == domain.OperationUnconfirmed {
		return fmt.Sprintf("Upgrade %s unconfirmed during %s: %s", fallback(sanitizeUntrustedText(target)), phase, sanitizeUntrustedText(errMessage))
	}
	return fmt.Sprintf("Upgrade %s failed during %s: %s", fallback(sanitizeUntrustedText(target)), phase, sanitizeUntrustedText(errMessage))
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
	hi, lo := bits.Mul64(uint64(current), 100)
	percent, _ := bits.Div64(hi, lo, uint64(total))
	return int64(percent), true
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
