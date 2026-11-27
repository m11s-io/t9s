package tui

import (
	"strings"
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestKubernetesUpgradeProgressCollapsesRepeatsAndKeepsNewest(t *testing.T) {
	apiserver := domain.KubernetesUpgradeEvent{Stage: domain.KubernetesUpgradeControlPlane, NodeUID: "uid-1", Status: "kube-apiserver"}
	scheduler := domain.KubernetesUpgradeEvent{Stage: domain.KubernetesUpgradeControlPlane, NodeUID: "uid-1", Status: "kube-scheduler"}
	var lines []kubernetesUpgradeEventLine
	for range 40 {
		lines = appendKubernetesUpgradeEvent(lines, apiserver, 50)
	}
	lines = appendKubernetesUpgradeEvent(lines, scheduler, 50)
	require.Len(t, lines, 2)
	require.Equal(t, 40, lines[0].count)

	model := kubernetesUpgradeModel{phase: kubernetesUpgradeRunning, events: lines}
	view := model.view(120, 0, true)
	require.Contains(t, view, "control-plane · uid-1 · kube-apiserver ×40")
	require.Contains(t, view, "control-plane · uid-1 · kube-scheduler")

	for index := range 50 {
		lines = appendKubernetesUpgradeEvent(lines, domain.KubernetesUpgradeEvent{Stage: domain.KubernetesUpgradeKubelet, Status: strings.Repeat("x", index+1)}, 50)
	}
	require.Len(t, lines, 50)
	model.events = lines
	short := model.view(120, 6, true)
	require.Contains(t, short, strings.Repeat("x", 50))
	require.NotContains(t, short, "kube-scheduler")
}
