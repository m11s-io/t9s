package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type EventReader interface {
	List(context.Context) (domain.EventSet, error)
	// Open follows new events from all current members until canceled or closed.
	Open(context.Context) (EventStream, error)
}

// EventStream merges node events in receive order. Next and Close may run concurrently.
type EventStream interface {
	Next(context.Context) (domain.EventSet, error)
	Close() error
}
