package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/ports"
)

var (
	ErrSessionClosed         = errors.New("session is closed")
	ErrCapabilityUnavailable = errors.New("session capability is unavailable")
)

// Session owns one adapter session and the resources associated with its lifetime.
type Session struct {
	sdkSession                 ports.Session
	kubernetesNodes            ports.KubernetesNodeReader
	lifetimeCtx                context.Context
	lifetimeCancel             context.CancelFunc
	writesEnabled              bool
	clusterUID                 string
	kubernetesUpgradeManifests map[string]bool

	mu           sync.Mutex
	closed       bool
	mutationBusy bool
	closeOnce    sync.Once
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.lifetimeCancel()
		if s.sdkSession != nil {
			closeAdapter(s.sdkSession)
		}
	})
	return nil
}

func (s *Session) operationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, error) {
	if s == nil {
		return nil, nil, ErrSessionClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, ErrSessionClosed
	}
	if err := s.lifetimeCtx.Err(); err != nil {
		s.mu.Unlock()
		return nil, nil, ErrSessionClosed
	}
	operationCtx, cancel := s.operationContextLocked(ctx, timeout)
	s.mu.Unlock()
	return operationCtx, cancel, nil
}

// operationContextLocked creates a caller-scoped context linked to the
// session lifetime. The caller must hold s.mu.
func (s *Session) operationContextLocked(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	var operationCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		operationCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		operationCtx, cancel = context.WithCancel(ctx)
	}
	stop := context.AfterFunc(s.lifetimeCtx, cancel)
	return operationCtx, func() {
		stop()
		cancel()
	}
}
