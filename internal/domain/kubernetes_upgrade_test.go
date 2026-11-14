package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssessKubernetesUpgradeRequiresSupportedTalosCLI(t *testing.T) {
	inventory := KubernetesUpgradeInventory{
		ClusterUID: "cluster", TalosClusterID: "talos", Gateway: "10.0.0.1",
		CLI:     TalosCLIIdentity{Path: "/usr/bin/talosctl", Version: "v1.14.6", SHA256: "fingerprint"},
		Healthy: true, ProxyConfigKnown: true, TargetProxyImage: "registry.k8s.io/kube-proxy:v1.36.1",
		Nodes: []KubernetesUpgradeNode{{
			Name: "cp-1", UID: "node", TalosID: "cp-1", TalosAddress: "10.0.0.1", Role: NodeRoleControl,
			TalosVersion: "v1.14.6", Ready: true, ProxyConfigKnown: true,
			APIServerVersion: "1.36.0", ControllerManagerVersion: "1.36.0", SchedulerVersion: "1.36.0",
			KubeletVersion: "1.36.0", KubeletImage: "ghcr.io/siderolabs/kubelet:v1.36.0",
			TargetKubeletImage: "ghcr.io/siderolabs/kubelet:v1.36.1", ConfigFingerprint: "config",
		}},
	}
	for _, version := range []string{"v1.14.6", "v1.14.5", "v1.14.7", "v1.14.6-rc.1"} {
		t.Run(version, func(t *testing.T) {
			inventory.CLI.Version = version
			plan, err := AssessKubernetesUpgrade(inventory, "1.36.1")
			if version != "v1.14.6" {
				require.ErrorIs(t, err, ErrKubernetesUpgradeAssessment)
				require.Empty(t, plan)
				return
			}
			require.NoError(t, err)
			require.Equal(t, inventory.CLI, plan.CLI)
			require.Equal(t, "1.36.1", plan.TargetVersion)
		})
	}
}
