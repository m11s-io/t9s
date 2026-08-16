package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type EventReader interface {
	List(context.Context) (domain.EventSet, error)
}
