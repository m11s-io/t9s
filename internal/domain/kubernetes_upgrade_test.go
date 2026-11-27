package domain

import (
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/stretchr/testify/require"
)

func TestAssessKubernetesUpgradeRequiresSupportedTalosCLI(t *testing.T) {
	inventory := KubernetesUpgradeInventory{
		ClusterUID: "cluster", TalosClusterID: "talos", Gateway: "10.0.0.1",
		CLI:     TalosCLIIdentity{Path: "/usr/bin/talosctl", Version: MinimumTalosCLIVersion, SHA256: "fingerprint"},
		Healthy: true, ProxyConfigKnown: true, TargetProxyImage: "registry.k8s.io/kube-proxy:v1.36.1",
		Nodes: []KubernetesUpgradeNode{{
			Name: "cp-1", UID: "node", TalosID: "cp-1", TalosAddress: "10.0.0.1", Role: NodeRoleControl,
			TalosVersion: "v1.14.6", Ready: true, ProxyConfigKnown: true,
			APIServerVersion: "1.36.0", ControllerManagerVersion: "1.36.0", SchedulerVersion: "1.36.0",
			KubeletVersion: "1.36.0", KubeletImage: "ghcr.io/siderolabs/kubelet:v1.36.0",
			TargetKubeletImage: "ghcr.io/siderolabs/kubelet:v1.36.1", ConfigFingerprint: "config",
		}},
	}
	for _, tc := range talosCLIVersionCases(t, MinimumTalosCLIVersion) {
		t.Run(tc.version, func(t *testing.T) {
			require.Equal(t, tc.supported, SupportedTalosCLIVersion(tc.version))
			inventory.CLI.Version = tc.version
			plan, err := AssessKubernetesUpgrade(inventory, "1.36.1")
			if !tc.supported {
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

type talosCLIVersionCase struct {
	version   string
	supported bool
}

// talosCLIVersionCases derives accepted and rejected talosctl versions from
// the minimum so bumping it never requires editing tests.
func talosCLIVersionCases(t *testing.T, minimum string) []talosCLIVersionCase {
	t.Helper()
	v, err := semver.ParseTolerant(minimum)
	require.NoError(t, err)
	require.NotZero(t, v.Patch, "minimum needs a previous patch to test against")
	previous, next, nextMinor := v, v, v
	previous.Patch--
	next.Patch++
	nextMinor.Minor++
	nextMinor.Patch = 0
	return []talosCLIVersionCase{
		{minimum, true},
		{"v" + next.String(), true},
		{"v" + previous.String(), false},
		{"v" + nextMinor.String(), false},
		{"v" + next.String() + "-rc.1", false},
		{strings.TrimPrefix(minimum, "v"), false},
	}
}
