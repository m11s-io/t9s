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

func TestUpgradeResultCarriesTerminalCompletion(t *testing.T) {
	result := UpgradeResult{Done: true}

	assert.True(t, result.Done)
	assert.Nil(t, result.Event)
}
