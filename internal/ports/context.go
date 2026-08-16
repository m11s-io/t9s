package ports

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
)

type ContextCatalog interface {
	List(context.Context) ([]domain.ClusterContext, error)
}

type SessionFactory interface {
	Open(context.Context, string) (Session, error)
}

type Session interface {
	Nodes() NodeReader
	Services() ServiceReader
	ServiceLogs() ServiceLogReader
	Events() EventReader
	Etcd() EtcdReader
	Processes() ProcessReader
	Disks() DiskReader
	Network() NetworkReader
	ResourceKinds() ResourceKindReader
	Resources() ResourceInstanceReader
	Close() error
}
