package application

import (
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

func ControlPlaneHostnamesForTest(nodes []domain.NodeSnapshot) []string {
	return controlPlaneHostnames(nodes)
}
func DefaultEtcdSnapshotPathForTest(contextName, memberHostname string, now time.Time) string {
	return DefaultEtcdSnapshotPath(contextName, memberHostname, now)
}
