package talos

import (
	"strings"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestConsumeUpgradeOutputNamesComponentAndPlannedNode(t *testing.T) {
	nodeUIDs := upgradeNodeUIDsByAddress([]domain.KubernetesUpgradeNodePlan{{UID: "uid-cp-1", TalosAddress: "10.0.0.1"}})
	output := strings.Join([]string{
		`updating "kube-apiserver" to version "1.37.2"`,
		` > "10.0.0.1": waiting for kube-apiserver pod update`,
		` > "10.0.0.9": kube-scheduler: secret-looking upstream text`,
		` > "10.0.0.1": pre-pulling registry.k8s.io/kube-controller-manager:v1.37.2`,
		`updating manifests`,
		`discovered controlplane nodes ["10.0.0.1"]`,
	}, "\n")
	var events []domain.KubernetesUpgradeEvent
	consumeUpgradeOutput(strings.NewReader(output), nodeUIDs, func(event domain.KubernetesUpgradeEvent) { events = append(events, event) })
	require.Equal(t, []domain.KubernetesUpgradeEvent{
		{Stage: domain.KubernetesUpgradeControlPlane, Status: "kube-apiserver"},
		{Stage: domain.KubernetesUpgradeControlPlane, NodeUID: "uid-cp-1", Status: "kube-apiserver"},
		{Stage: domain.KubernetesUpgradeControlPlane, Status: "kube-scheduler"},
		{Stage: domain.KubernetesUpgradePrePull, NodeUID: "uid-cp-1", Status: "kube-controller-manager"},
		{Stage: domain.KubernetesUpgradeManifests, Status: "in progress"},
	}, events)
}
