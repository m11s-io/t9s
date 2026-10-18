package application

import (
	"context"
	"errors"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

// Initial client budgets; validate against supported disposable clusters.
// WithTimeout preserves any shorter deadline supplied by the caller.
const (
	controlOperationTimeout = 30 * time.Second
	snapshotTimeout         = 10 * time.Minute
	defragmentTimeout       = 10 * time.Minute
	destructiveNodeTimeout  = 30 * time.Minute
)

// Preserve the caller's cancellation cause when a transport reports it as a
// status error rather than wrapping context.Canceled/DeadlineExceeded.
func mutationCallError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	return err
}

func snapshotErrorText(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "snapshot deadline exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "snapshot canceled"
	}
	return "snapshot failed"
}

func mutationErrorText(err error, outcome domain.OperationOutcome) string {
	if outcome != domain.OperationUnconfirmed {
		return "action failed"
	}
	prefix := "request failed"
	if errors.Is(err, context.DeadlineExceeded) {
		prefix = "deadline exceeded"
	} else if errors.Is(err, context.Canceled) {
		prefix = "request canceled"
	}
	return prefix + "; remote outcome unconfirmed — check state before retrying"
}
