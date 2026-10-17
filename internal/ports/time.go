package ports

import (
	"context"
	"errors"

	"github.com/m11s-io/t9s/internal/domain"
)

var ErrTimePermissionDenied = errors.New("permission denied")

type TimeReader interface {
	Read(context.Context, string) (domain.TimeSnapshot, error)
	Check(context.Context, string, string) (domain.TimeCheckResult, error)
}
