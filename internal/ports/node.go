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

type NodeController interface {
	Reboot(ctx context.Context, target string, mode RebootMode) error
	Shutdown(ctx context.Context, target string, force bool) error
	Rollback(ctx context.Context, target string) error
	Upgrade(ctx context.Context, target, image string) error
	UpgradeStream(ctx context.Context, target, image string) UpgradeStream
	CurrentInstallImage(ctx context.Context, target string) (string, error)
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
