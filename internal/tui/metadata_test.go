package tui

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestDeriveShellMetadataUsesActiveContextAndNodeSnapshot(t *testing.T) {
	model := application.Model{
		ContextName: "prod",
		Contexts: []domain.ClusterContext{
			{Name: "dev", Cluster: "development"},
			{Name: "prod", Cluster: "production", Endpoints: []string{"10.0.0.1", "10.0.0.2"}, Nodes: []string{"cp-1", "cp-2", "worker-1"}},
		},
		Nodes: application.NodeState{Status: application.Partial, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{Name: "cp-1", Health: domain.HealthHealthy, Version: "v1.13.2"},
			{Name: "cp-2", Health: domain.HealthHealthy, Version: "v1.13.3"},
			{Name: "worker-1", Health: domain.HealthUnhealthy, Version: "v1.13.3"},
		}}},
	}

	assert.Equal(t, shellMetadata{
		Context: "prod", Cluster: "production", EndpointSummary: "2", NodeSummary: "2/3",
		TalosVersion: "mixed", Health: "Degraded", Mode: "[RO]", AppVersion: "dev",
	}, deriveShellMetadata(model))
}

func TestDeriveShellMetadataDoesNotInventUnavailableValues(t *testing.T) {
	model := application.Model{ContextName: "prod", Nodes: application.NodeState{Status: application.Loading}}

	assert.Equal(t, shellMetadata{Context: "prod", Health: "Loading", Mode: "[RO]", AppVersion: "dev"}, deriveShellMetadata(model))
}

func TestDeriveShellMetadataReportsUniformTalosVersion(t *testing.T) {
	model := application.Model{Nodes: application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
		{Health: domain.HealthHealthy, Version: "v1.13.6"}, {Health: domain.HealthHealthy, Version: "v1.13.6"},
	}}}}

	metadata := deriveShellMetadata(model)
	assert.Equal(t, "v1.13.6", metadata.TalosVersion)
	assert.Equal(t, "Healthy", metadata.Health)
	assert.Equal(t, "2/2", metadata.NodeSummary)
}

func TestDeriveShellMetadataShowsReadWriteWhenWritesEnabled(t *testing.T) {
	model := application.Model{ContextName: "prod", WritesEnabled: true, Nodes: application.NodeState{Status: application.Loading}}

	metadata := deriveShellMetadata(model)

	assert.Equal(t, "[RW]", metadata.Mode)
}

func TestDeriveShellMetadataShowsReadOnlyByDefault(t *testing.T) {
	model := application.Model{ContextName: "prod", Nodes: application.NodeState{Status: application.Loading}}

	metadata := deriveShellMetadata(model)

	assert.Equal(t, "[RO]", metadata.Mode)
}
