package domain_test

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
			assert.Equal(t, tt.want, tt.in.String())
		})
	}
}

func TestServiceSummaryCompactString(t *testing.T) {
	assert.Equal(t, "8✓ 0! 3?", (domain.ServiceSummary{Healthy: 8, Unknown: 3, Total: 11, Known: true}).CompactString())
}

func TestNodeSnapshotDisplayName(t *testing.T) {
	node := domain.NodeSnapshot{ID: "node-id", Addresses: []string{"10.0.0.2"}}
	assert.Equal(t, "10.0.0.2", node.DisplayName())
}

func TestNodeSnapshotTargetPrefersNameThenFirstAddress(t *testing.T) {
	assert.Equal(t, "cp-1", domain.NodeSnapshot{Name: "cp-1", Addresses: []string{"10.0.0.5"}}.Target())
	assert.Equal(t, "10.0.0.5", domain.NodeSnapshot{Addresses: []string{"10.0.0.5"}}.Target())
	assert.Equal(t, "", domain.NodeSnapshot{}.Target())
}

func TestKubernetesStateValuesAreDistinct(t *testing.T) {
	assert.Equal(t, domain.KubernetesState("Ready"), domain.KubernetesReady)
	assert.Equal(t, domain.KubernetesState("NotReady"), domain.KubernetesNotReady)
	assert.NotEqual(t, domain.KubernetesReady, domain.KubernetesUnknown)
}
