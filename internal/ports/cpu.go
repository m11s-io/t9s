package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type CPUReader interface {
	Sample(context.Context, string) (domain.CPUSnapshot, error)
}
