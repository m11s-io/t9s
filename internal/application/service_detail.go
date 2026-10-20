package application

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

func (s *Session) ServiceDetail(ctx context.Context, node, service string) (domain.ServiceDetail, error) {
	ctx, cancel, err := s.operationContext(ctx, unaryReadTimeout)
	if err != nil {
		return domain.ServiceDetail{}, err
	}
	defer cancel()
	if s.sdkSession == nil || s.sdkSession.Services() == nil {
		return domain.ServiceDetail{}, ErrCapabilityUnavailable
	}
	return s.sdkSession.Services().Detail(ctx, node, service)
}
