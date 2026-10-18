package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
)

var (
	ErrWritesDisabled       = errors.New("writes are disabled for this session")
	ErrMutationBusy         = errors.New("another mutation is active in this session")
	ErrInvalidPlan          = errors.New("prepared action is invalid for this session")
	ErrPlanUsed             = errors.New("prepared action has already been used")
	ErrConfirmationRequired = errors.New("confirmation is required")
	ErrAssessmentChanged    = errors.New("action assessment changed")
	ErrActionBlocked        = errors.New("action is blocked")
)

type Confirmation struct {
	Approved bool
	Token    string
}

type TargetResult struct {
	Target  string
	Outcome domain.OperationOutcome
	Err     error
	Warning string
}

type MutationResult struct {
	Targets  []TargetResult
	Snapshot *domain.EtcdSnapshotResult
}

func (s *Session) admit(ctx context.Context, plan *preparedState, confirmation Confirmation, timeout time.Duration) (context.Context, func(), error) {
	if s == nil {
		return nil, nil, ErrSessionClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.lifetimeCtx == nil || s.lifetimeCtx.Err() != nil {
		return nil, nil, ErrSessionClosed
	}
	if !s.writesEnabled {
		return nil, nil, ErrWritesDisabled
	}
	if plan == nil || plan.session != s || (plan.nodeRequest == nil && plan.serviceRequest == nil && plan.etcdRequest == nil) {
		return nil, nil, ErrInvalidPlan
	}
	if plan.consumed {
		return nil, nil, ErrPlanUsed
	}
	if plan.assessment.Blocked != "" {
		return nil, nil, ErrActionBlocked
	}
	tokenMatches := confirmation.Token == plan.assessment.RequiredToken
	if plan.nodeRequest != nil && plan.nodeRequest.Kind == ActionWipeDevice && plan.nodeRequest.DeviceWipe != nil {
		tokenMatches = ValidateDeviceWipeConfirmation(plan.nodeRequest.DeviceWipe.Device, confirmation.Token) == nil
	}
	if !confirmation.Approved || (plan.assessment.RequiredToken != "" && !tokenMatches) {
		return nil, nil, ErrConfirmationRequired
	}
	if s.mutationBusy {
		return nil, nil, ErrMutationBusy
	}

	operationCtx, cancel := s.operationContextLocked(ctx, timeout)
	plan.consumed = true
	s.mutationBusy = true
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			s.mu.Lock()
			s.mutationBusy = false
			s.mu.Unlock()
		})
	}
	return operationCtx, release, nil
}
