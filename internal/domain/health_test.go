package domain_test

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestSeverityStringCoversEveryValue(t *testing.T) {
	assert.Equal(t, "unknown", domain.SeverityUnknown.String())
	assert.Equal(t, "healthy", domain.SeverityHealthy.String())
	assert.Equal(t, "warning", domain.SeverityWarning.String())
	assert.Equal(t, "critical", domain.SeverityCritical.String())
}

func TestSeverityOrdersMoreSevereAsGreater(t *testing.T) {
	assert.Greater(t, domain.SeverityCritical, domain.SeverityWarning)
	assert.Greater(t, domain.SeverityWarning, domain.SeverityHealthy)
	assert.Greater(t, domain.SeverityHealthy, domain.SeverityUnknown)
}
