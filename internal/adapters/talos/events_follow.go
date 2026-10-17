package talos

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// Open follows new events only, retaining the existing snapshot in the caller.
// Each node owns a receive goroutine so an idle node cannot delay another.
func (r *eventReader) Open(ctx context.Context) (ports.EventStream, error) {
	members, err := r.members.Members(ctx)
	if err != nil {
		return nil, fmt.Errorf("list event members: %w", err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	s := &eventFollowStream{ctx: streamCtx, cancel: cancel, batches: make(chan domain.EventSet, 64)}
	seen := map[string]bool{}
	for _, member := range members {
		target := member.Hostname
		if target == "" && len(member.Addresses) > 0 {
			target = member.Addresses[0]
		}
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		s.workers.Add(1)
		go func() { defer s.workers.Done(); r.followNode(s, target) }()
	}
	go func() { s.workers.Wait(); close(s.batches) }()
	return s, nil
}

func (r *eventReader) followNode(s *eventFollowStream, target string) {
	stream, err := r.events.Events(s.ctx, target, talosclient.WithTailEvents(0))
	if err != nil {
		s.problem(target)
		return
	}
	for {
		raw, err := stream.Recv()
		if err != nil {
			if s.ctx.Err() == nil {
				s.problem(target)
			}
			return
		}
		decoded, err := talosclient.UnmarshalEvent(raw)
		if err != nil || decoded == nil {
			continue
		}
		kind, message := describeEventPayload(decoded.Payload)
		if kind == "" {
			continue
		}
		if !s.send(domain.EventSet{Events: []domain.EventSnapshot{{Node: target, Kind: kind, Message: message, ObservedAt: r.now()}}}) {
			return
		}
	}
}

type eventFollowStream struct {
	ctx     context.Context
	cancel  context.CancelFunc
	batches chan domain.EventSet
	workers sync.WaitGroup
	once    sync.Once
}

func (s *eventFollowStream) send(batch domain.EventSet) bool {
	select {
	case s.batches <- batch:
		return true
	case <-s.ctx.Done():
		return false
	}
}
func (s *eventFollowStream) problem(node string) {
	if s.ctx.Err() == nil {
		s.send(domain.EventSet{Problems: []domain.EventProblem{{Node: node, Message: "event stream unavailable"}}})
	}
}
func (s *eventFollowStream) Next(ctx context.Context) (domain.EventSet, error) {
	// Canceling a receive also releases the underlying RPCs.
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	if err := ctx.Err(); err != nil {
		s.cancel()
		return domain.EventSet{}, err
	}
	if err := s.ctx.Err(); err != nil {
		return domain.EventSet{}, err
	}
	select {
	case <-ctx.Done():
		s.cancel()
		return domain.EventSet{}, ctx.Err()
	case <-s.ctx.Done():
		return domain.EventSet{}, s.ctx.Err()
	case batch, ok := <-s.batches:
		if !ok {
			return domain.EventSet{}, io.EOF
		}
		return batch, nil
	}
}
func (s *eventFollowStream) Close() error { s.once.Do(s.cancel); return nil }
