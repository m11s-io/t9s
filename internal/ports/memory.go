package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

// MemoryReader reads a single node's memory usage. List returns one snapshot
// (not a set) so the method name stays uniform with the other node readers.
type MemoryReader interface {
	List(ctx context.Context, node string) (domain.MemorySnapshot, error)
}
