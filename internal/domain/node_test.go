package domain_test

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

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
