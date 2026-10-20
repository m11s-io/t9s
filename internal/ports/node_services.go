package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

// NodeServiceReader observes node health and detailed services in one refresh.
type NodeServiceReader interface {
	List(context.Context) (domain.NodeSet, domain.ServiceSet, error)
}

// NodeServiceSession optionally provides a shared node and service observation.
type NodeServiceSession interface {
	NodeServices() NodeServiceReader
}

// ProgressiveNodeServiceReader optionally publishes discovery and completed
// inspections before the final shared node/service observation is ready.
// Snapshots must be independent; publish is called serially and must honor ctx.
type ProgressiveNodeServiceReader interface {
	ListProgress(context.Context, func(domain.NodeSet)) (domain.NodeSet, domain.ServiceSet, error)
}
