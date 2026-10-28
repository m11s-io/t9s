package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

var ErrUpgradeSnapshotFailed = errors.New("etcd snapshot failed; upgrade not started")

// ExecuteUpgrade consumes a prepared upgrade plan, refreshes its identity and
// policy facts under the session mutation lease, and returns the live adapter
// stream. It preserves caller/session cancellation and leaves long-running
// upgrade deadlines to the adapter's stage budgets. The lease remains held
// through terminal completion, including recovery cleanup, without a progress
// consumer.
func (s *Session) ExecuteUpgrade(ctx context.Context, plan *PreparedNodeAction, confirmation Confirmation) (ports.UpgradeStream, error) {
	if plan == nil || plan.state == nil || plan.state.nodeRequest == nil || plan.state.nodeFacts == nil || plan.state.nodeRequest.Kind != ActionUpgrade {
		return nil, ErrInvalidPlan
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	request := cloneNodeActionRequest(*plan.state.nodeRequest)
	operationCtx, release, err := s.admit(ctx, plan.state, confirmation, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (ports.UpgradeStream, error) {
		release()
		return nil, err
	}
	if s.sdkSession == nil || s.sdkSession.NodeActions() == nil {
		return fail(ErrCapabilityUnavailable)
	}

	checkFacts := func() error {
		readCtx, cancelRead := context.WithTimeout(operationCtx, unaryReadTimeout)
		defer cancelRead()
		nodes, err := readNodes(readCtx, s.sdkSession)
		if err != nil {
			if readCtx.Err() != nil {
				return errors.Join(readCtx.Err(), err)
			}
			return errors.Join(ErrAssessmentChanged, err)
		}
		resolved := make([]domain.NodeSnapshot, 0, len(request.Targets))
		for _, target := range request.Targets {
			node, ok := resolveNodeTarget(nodes.Nodes, target)
			if !ok || !knownNodeActionRole(node.Role) {
				return ErrAssessmentChanged
			}
			resolved = append(resolved, node)
		}
		currentFacts, err := s.readNodeActionFacts(readCtx, nodes.Nodes, resolved, request)
		if err != nil {
			if readCtx.Err() != nil {
				return errors.Join(readCtx.Err(), err)
			}
			return errors.Join(ErrAssessmentChanged, err)
		}
		if err := readCtx.Err(); err != nil {
			return err
		}
		if currentFacts.Blocked != "" {
			return ErrActionBlocked
		}
		if !sameNodeActionFacts(*plan.state.nodeFacts, currentFacts) {
			return ErrAssessmentChanged
		}
		return nil
	}
	if err := checkFacts(); err != nil {
		return fail(err)
	}
	var snapshot *domain.EtcdSnapshotResult
	if request.AllowControlPlaneDowntime {
		if s.sdkSession.EtcdOperations() == nil {
			return fail(ErrCapabilityUnavailable)
		}
		var err error
		snapshot, _, err = s.executeEtcdSnapshot(operationCtx, request.Targets[0], request.SnapshotPath)
		if err != nil {
			return fail(fmt.Errorf("%w: %w", ErrUpgradeSnapshotFailed, err))
		}
		if err := checkFacts(); err != nil {
			return fail(err)
		}
	}
	if err := operationCtx.Err(); err != nil {
		return fail(err)
	}

	upstream := s.sdkSession.NodeActions().UpgradeStream(operationCtx, request.Targets[0], request.Image)
	if upstream == nil {
		return fail(ErrCapabilityUnavailable)
	}
	wrapped := &retainedUpgradeStream{upstream: upstream, release: release, done: make(chan struct{}), snapshot: snapshot}
	go wrapped.awaitTerminal()
	return wrapped, nil
}

type retainedUpgradeStream struct {
	snapshot *domain.EtcdSnapshotResult
	upstream ports.UpgradeStream
	release  func()
	done     chan struct{}

	terminal ports.UpgradeResult
	waitErr  error
}

func (s *retainedUpgradeStream) Results() <-chan ports.UpgradeResult { return s.upstream.Results() }
func (s *retainedUpgradeStream) Cancel()                             { s.upstream.Cancel() }

// awaitTerminal uses an uncanceled local context only to observe completion;
// all adapter RPCs receive the operation context created by Session.admit.
func (s *retainedUpgradeStream) awaitTerminal() {
	result, err := s.upstream.Wait(context.Background())
	result.Snapshot = s.snapshot
	s.terminal, s.waitErr = result, err
	s.release()
	close(s.done)
}

// Wait is safe for repeated and concurrent callers. A caller's context only
// controls that caller's wait and cannot abandon terminal retention or release
// the session mutation lease early.
func (s *retainedUpgradeStream) Wait(ctx context.Context) (ports.UpgradeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.done:
		result := s.terminal
		if result.Snapshot != nil {
			receipt := *result.Snapshot
			result.Snapshot = &receipt
		}
		return result, s.waitErr
	case <-ctx.Done():
		return ports.UpgradeResult{}, ctx.Err()
	}
}
