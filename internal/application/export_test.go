package application

import (
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

func ControlPlaneHostnamesForTest(nodes []domain.NodeSnapshot) []string {
	return controlPlaneHostnames(nodes)
}

func DefaultEtcdSnapshotPathForTest(contextName, memberHostname string, now time.Time) string {
	return defaultEtcdSnapshotPath(contextName, memberHostname, now)
}

// Legacy effect construction is exposed only for focused controller tests.
func BuildActionEffects(model Model, pending PendingAction) []Effect {
	return buildActionEffects(model, pending)
}
func BuildServiceActionEffect(model Model, pending PendingServiceAction) Effect {
	return buildServiceActionEffect(model, pending)
}
