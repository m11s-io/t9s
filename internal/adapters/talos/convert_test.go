package talos

import (
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestConvertNode(t *testing.T) {
	now := time.Date(2026, time.August, 12, 10, 30, 0, 0, time.UTC)
	truth, falsity := true, false

	tests := []struct {
		name string
		raw  rawNode
		want domain.NodeSnapshot
	}{
		{
			name: "control plane with healthy machine and mixed services",
			raw: rawNode{
				ID: "machine-1", Hostname: "cp-1", Addresses: []string{"10.0.0.1"},
				MachineType: "controlplane", Stage: "running", Ready: &truth,
				Services: []rawService{{Healthy: &truth}, {Healthy: &falsity}}, ServicesKnown: true,
				Version: "v1.13.3", ObservedAt: now,
			},
			want: domain.NodeSnapshot{
				ID: "machine-1", Name: "cp-1", Addresses: []string{"10.0.0.1"},
				Role: domain.NodeRoleControl, Stage: "running", Health: domain.HealthHealthy,
				Services:   domain.ServiceSummary{Healthy: 1, Total: 2, Known: true},
				Kubernetes: domain.KubernetesUnknown, Version: "v1.13.3", ObservedAt: now,
			},
		},
		{
			name: "worker and not ready",
			raw:  rawNode{ID: "machine-2", MachineType: "worker", Ready: &falsity, ObservedAt: now},
			want: domain.NodeSnapshot{ID: "machine-2", Role: domain.NodeRoleWorker, Health: domain.HealthUnhealthy,
				Services: domain.ServiceSummary{}, Kubernetes: domain.KubernetesUnknown, ObservedAt: now},
		},
		{
			name: "unknown role and missing machine status",
			raw:  rawNode{ID: "machine-3", MachineType: "init", ObservedAt: now},
			want: domain.NodeSnapshot{ID: "machine-3", Role: domain.NodeRoleUnknown, Health: domain.HealthUnknown,
				Services: domain.ServiceSummary{}, Kubernetes: domain.KubernetesUnknown, ObservedAt: now},
		},
		{
			name: "unknown service health makes summary unknown",
			raw: rawNode{ID: "machine-4", Services: []rawService{{Healthy: &truth}, {}}, ServicesKnown: true,
				ObservedAt: now},
			want: domain.NodeSnapshot{ID: "machine-4", Role: domain.NodeRoleUnknown, Health: domain.HealthUnknown,
				Services:   domain.ServiceSummary{Healthy: 1, Unknown: 1, Total: 2, Known: true},
				Kubernetes: domain.KubernetesUnknown, ObservedAt: now},
		},
		{
			name: "missing version and hostname falls back through display name",
			raw: rawNode{ID: "machine-5", Addresses: []string{"10.0.0.5"}, ServicesKnown: true,
				ObservedAt: now},
			want: domain.NodeSnapshot{ID: "machine-5", Addresses: []string{"10.0.0.5"}, Role: domain.NodeRoleUnknown,
				Health: domain.HealthUnknown, Services: domain.ServiceSummary{Known: true},
				Kubernetes: domain.KubernetesUnknown, ObservedAt: now},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := convertNode(test.raw)
			assert.Equal(t, test.want, got)
			if test.raw.Hostname == "" {
				assert.NotEmpty(t, got.DisplayName())
			}
		})
	}
}
