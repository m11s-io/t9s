package tui

import (
	"fmt"

	"github.com/m11s-io/t9s/internal/domain"
)

func serviceSummary(s domain.ServiceSummary) string {
	if !s.Known && s.Total == 0 {
		return "?"
	}

	return fmt.Sprintf("%d healthy · %d unhealthy · %d unknown", s.Healthy, s.Total-s.Healthy-s.Unknown, s.Unknown)
}

func compactServiceSummary(s domain.ServiceSummary) string {
	if !s.Known && s.Total == 0 {
		return "?"
	}

	return fmt.Sprintf("%d✓ %d! %d?", s.Healthy, s.Total-s.Healthy-s.Unknown, s.Unknown)
}
