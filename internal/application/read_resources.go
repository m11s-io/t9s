package application

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

func (s *Session) ResourceKinds(ctx context.Context) (domain.ResourceKindSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.ResourceKindSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.ResourceKinds() == nil {
		return domain.ResourceKindSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.ResourceKinds().List(ctx)
}

func (s *Session) Resources(ctx context.Context, node, kind string) (domain.ResourceInstanceSet, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.ResourceInstanceSet{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Resources() == nil {
		return domain.ResourceInstanceSet{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Resources().List(ctx, node, kind)
}

func (s *Session) Resource(ctx context.Context, node, kind, id string) (domain.ResourceInstanceSnapshot, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.ResourceInstanceSnapshot{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Resources() == nil {
		return domain.ResourceInstanceSnapshot{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Resources().Get(ctx, node, kind, id)
}
