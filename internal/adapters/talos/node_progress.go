package talos

import (
	"context"
	"sort"
	"sync"

	"github.com/m11s-io/t9s/internal/domain"
	"golang.org/x/sync/errgroup"
)

func (r *nodeReader) listProgress(ctx context.Context, publish func(domain.NodeSet)) (domain.NodeSet, error) {
	members, err := r.api.Members(ctx)
	if err != nil {
		return domain.NodeSet{}, err
	}
	rows := make([]rawNode, len(members))
	for i, member := range members {
		rows[i] = rawNode{ID: member.ID, Hostname: member.Hostname, Addresses: append([]string(nil), member.Addresses...), MachineType: member.MachineType, OperatingSystem: member.OperatingSystem, Stage: "Loading", ObservedAt: r.now()}
	}
	snapshot := func() domain.NodeSet {
		nodes := make([]domain.NodeSnapshot, len(rows))
		for i := range rows {
			nodes[i] = convertNode(rows[i])
		}
		sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].DisplayName() < nodes[j].DisplayName() })
		return domain.NodeSet{Nodes: nodes, ObservedAt: r.now()}
	}
	publish(snapshot())
	var mu sync.Mutex
	var group errgroup.Group
	group.SetLimit(maxConcurrentNodeInspections)
	for i, member := range members {
		if ctx.Err() != nil {
			break
		}
		group.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			row := r.inspectNode(ctx, member)
			row.ObservedAt = r.now()
			if ctx.Err() != nil {
				row.Problem = "node inspection unavailable"
				if row.Stage == "" {
					row.Stage = "Unavailable"
				}
			}
			mu.Lock()
			defer mu.Unlock()
			rows[i] = row
			if ctx.Err() == nil {
				publish(snapshot())
			}
			return nil
		})
	}
	_ = group.Wait()
	for i := range rows {
		if rows[i].Stage == "Loading" {
			rows[i].Stage = "Unavailable"
			rows[i].Problem = "node inspection unavailable"
		}
	}
	return snapshot(), ctx.Err()
}
