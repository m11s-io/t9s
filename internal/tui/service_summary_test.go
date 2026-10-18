package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestServiceSummaryString(t *testing.T) {
	tests := []struct {
		name string
		in   domain.ServiceSummary
		want string
	}{
		{"known", domain.ServiceSummary{Healthy: 6, Total: 7, Known: true}, "6 healthy · 1 unhealthy · 0 unknown"},
		{"mixed", domain.ServiceSummary{Healthy: 8, Unknown: 3, Total: 11, Known: true}, "8 healthy · 0 unhealthy · 3 unknown"},
		{"unknown", domain.ServiceSummary{}, "?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, serviceSummary(tt.in))
		})
	}
}

func TestServiceSummaryCompactString(t *testing.T) {
	assert.Equal(t, "8✓ 0! 3?", compactServiceSummary(domain.ServiceSummary{Healthy: 8, Unknown: 3, Total: 11, Known: true}))
}
