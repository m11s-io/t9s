package ports

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpgradeResultCarriesProgressEvent(t *testing.T) {
	result := UpgradeResult{Event: &UpgradeEvent{
		Phase:   UpgradePulling,
		Message: "pulling installer",
		Current: 10,
		Total:   100,
	}}

	assert.Equal(t, UpgradePulling, result.Event.Phase)
	assert.Equal(t, int64(10), result.Event.Current)
	assert.Equal(t, int64(100), result.Event.Total)
	assert.NoError(t, result.Err)
	assert.False(t, result.Done)
}

func TestUpgradeResultCarriesAppliedOutcome(t *testing.T) {
	result := UpgradeResult{Done: true, Outcome: UpgradeOutcomeApplied}

	assert.Equal(t, UpgradeOutcomeApplied, result.Outcome)
	assert.Empty(t, result.Warning)
}

func TestUpgradeResultCarriesRecoveryWarningOutcome(t *testing.T) {
	result := UpgradeResult{
		Done:    true,
		Outcome: UpgradeOutcomeAppliedWithRecoveryWarning,
		Warning: "Talos upgrade applied; node recovery is still pending; node may remain cordoned.",
	}

	assert.Equal(t, UpgradeOutcomeAppliedWithRecoveryWarning, result.Outcome)
	assert.Contains(t, result.Warning, "recovery is still pending")
}

func TestUpgradeResultCarriesTerminalCompletion(t *testing.T) {
	result := UpgradeResult{Done: true}

	assert.True(t, result.Done)
	assert.Nil(t, result.Event)
}
