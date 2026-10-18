package talos

import (
	"context"
	"sync"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type nodeServiceReader struct {
	api talosAPI
	now func() time.Time
}

var _ ports.NodeServiceReader = (*nodeServiceReader)(nil)

func newNodeServiceReader(api talosAPI, now func() time.Time) *nodeServiceReader {
	return &nodeServiceReader{api: api, now: now}
}

func (r *nodeServiceReader) List(ctx context.Context) (domain.NodeSet, domain.ServiceSet, error) {
	// The observation lives for this call only. Services remain uncapped here so
	// the detailed table limit cannot alter the node health summary.
	observation := &serviceObservation{talosAPI: r.api, services: make(map[string]*observedServices)}
	observedAt := r.now()
	now := func() time.Time { return observedAt }
	nodes, err := newNodeReader(observation, now).List(ctx)
	if err != nil {
		return nodes, domain.ServiceSet{}, err
	}
	services, err := newDetailedServiceReader(observation, now).List(ctx)
	return nodes, services, err
}

type observedServices struct {
	once    sync.Once
	records []serviceRecord
	err     error
}

type serviceObservation struct {
	talosAPI
	membersOnce sync.Once
	members     []memberRecord
	membersErr  error
	mu          sync.Mutex
	services    map[string]*observedServices
}

func (o *serviceObservation) Members(ctx context.Context) ([]memberRecord, error) {
	o.membersOnce.Do(func() { o.members, o.membersErr = o.talosAPI.Members(ctx) })
	return o.members, o.membersErr
}

func (o *serviceObservation) Services(ctx context.Context, target string) ([]serviceRecord, error) {
	o.mu.Lock()
	entry := o.services[target]
	if entry == nil {
		entry = &observedServices{}
		o.services[target] = entry
	}
	o.mu.Unlock()
	entry.once.Do(func() { entry.records, entry.err = o.talosAPI.Services(ctx, target) })
	return entry.records, entry.err
}
