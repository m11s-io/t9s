package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type EtcdReader interface {
	List(ctx context.Context, controlPlaneNodes []string) (domain.EtcdSet, error)
}

// EtcdOperations is the write/admin surface for etcd. It is deliberately
// separate from EtcdReader so read-only callers keep the narrow interface.
type EtcdOperations interface {
	// Snapshot streams a point-in-time etcd snapshot from node to a local
	// path. Single node only. The implementation writes atomically
	// (<path>.part -> <path>) and verifies the sha256 trailer.
	Snapshot(ctx context.Context, node, path string) (domain.EtcdSnapshotResult, error)
	// RemoveMemberByID forcibly removes an etcd member by numeric member ID.
	// Used for dead/unreachable members; prefer LeaveCluster for live ones.
	RemoveMemberByID(ctx context.Context, node string, memberID uint64) error
	// LeaveCluster makes the member reached at node leave the cluster
	// gracefully. Must be addressed to the member's own node.
	LeaveCluster(ctx context.Context, node string) error
	// Defragment releases unused space in the etcd data dir on node.
	Defragment(ctx context.Context, node string) error
	// DisarmAlarms disarms active etcd alarms (NOSPACE, CORRUPT, ...) on node.
	// It does not reclaim disk and does not repair corruption.
	DisarmAlarms(ctx context.Context, node string) error
}
