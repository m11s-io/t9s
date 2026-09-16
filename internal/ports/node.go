package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type NodeReader interface {
	List(context.Context) (domain.NodeSet, error)
}

type RebootMode int

const (
	RebootDefault RebootMode = iota
	RebootPowercycle
)

// WipeMode selects which class of storage a reset erases.
type WipeMode int

const (
	// WipeModeAll erases the system disk and every user disk.
	WipeModeAll WipeMode = iota
	// WipeModeSystemDisk erases only the system disk.
	WipeModeSystemDisk
	// WipeModeUserDisks erases only user disks.
	WipeModeUserDisks
)

// ResetOptions describes a node reset/wipe. SystemPartitions is left empty in
// normal use: the disks reader exposes whole disks only, so an empty list
// means "all system partitions", which is the machinery default.
type ResetOptions struct {
	Mode             WipeMode
	Graceful         bool     // leave etcd before reset
	Reboot           bool     // true = reboot, false = halt
	SystemPartitions []string // partition labels; empty = all system partitions
	// UserDisks are device paths exactly as the disk inventory reports them
	// (for example /dev/sdb). Talos wipes only listed user disks and never
	// auto-enumerates, so an empty list means no user disk is erased.
	UserDisks []string
}

type NodeController interface {
	Reboot(ctx context.Context, target string, mode RebootMode) error
	Shutdown(ctx context.Context, target string, force bool) error
	Rollback(ctx context.Context, target string) error
	Reset(ctx context.Context, target string, options ResetOptions) error
	Upgrade(ctx context.Context, target, image string) error
	UpgradeStream(ctx context.Context, target, image string) UpgradeStream
	CurrentInstallImage(ctx context.Context, target string) (string, error)
	// Uncordon is idempotent: it is a no-op when the node is already
	// schedulable, so callers may retry it freely.
	Uncordon(ctx context.Context, target string) error
}

// UpgradePhase identifies the lifecycle stage represented by an upgrade event.
type UpgradePhase string

const (
	UpgradeChecking   UpgradePhase = "checking"
	UpgradePulling    UpgradePhase = "pulling"
	UpgradeInstalling UpgradePhase = "installing"
	UpgradeDraining   UpgradePhase = "draining"
	UpgradeRebooting  UpgradePhase = "rebooting"
	UpgradeWaiting    UpgradePhase = "waiting"
	UpgradeUncordon   UpgradePhase = "uncordoning"
	UpgradeComplete   UpgradePhase = "complete"
)

type UpgradeEvent struct {
	Phase   UpgradePhase
	Message string
	Current int64
	Total   int64
}

type UpgradeOutcome string

const (
	UpgradeOutcomeApplied                    UpgradeOutcome = "applied"
	UpgradeOutcomeAppliedWithRecoveryWarning UpgradeOutcome = "applied-with-recovery-warning"
)

type UpgradeResult struct {
	Event   *UpgradeEvent
	Err     error
	Outcome UpgradeOutcome
	Warning string
	Done    bool
}

type UpgradeStream interface {
	Results() <-chan UpgradeResult
	Cancel()
}
