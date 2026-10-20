package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

// KubernetesUpgradeBackend owns the Talos CLI and node-level kubelet stages.
// Implementations must emit normalized events and must not expose raw output.
type KubernetesUpgradeBackend interface {
	Inspect(ctx context.Context, target string) (domain.KubernetesUpgradeInventory, error)
	RunControlPlane(ctx context.Context, plan domain.KubernetesUpgradePlan, progress func(domain.KubernetesUpgradeEvent)) error
	UpgradeKubelet(ctx context.Context, node domain.KubernetesUpgradeNodePlan, progress func(domain.KubernetesUpgradeEvent)) (domain.KubernetesUpgradeNodeResult, error)
}

type KubernetesUpgradeStream interface {
	Events() <-chan domain.KubernetesUpgradeEvent
	Cancel()
	Wait(context.Context) (domain.KubernetesUpgradeResult, error)
}
