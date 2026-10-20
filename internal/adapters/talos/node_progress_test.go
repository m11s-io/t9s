package talos

import (
	"context"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeProgressPublishesDiscoveryAndFastNodeBeforeSlowNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := &fakeTalosAPI{members: []memberRecord{{ID: "fast", Hostname: "fast", MachineType: "worker"}, {ID: "slow", Hostname: "slow"}}, machineStatus: func(ctx context.Context, target string) (machineRecord, error) {
		if target == "slow" {
			<-ctx.Done()
			return machineRecord{}, ctx.Err()
		}
		ready := true
		return machineRecord{Stage: "running", Ready: &ready}, nil
	}}
	updates := make(chan domain.NodeSet, 5)
	type result struct {
		nodes domain.NodeSet
		err   error
	}
	done := make(chan result, 1)
	go func() {
		nodes, _, err := newNodeServiceReader(api, time.Now).ListProgress(ctx, func(nodes domain.NodeSet) { updates <- nodes })
		done <- result{nodes, err}
	}()
	select {
	case first := <-updates:
		require.Len(t, first.Nodes, 2)
		assert.Equal(t, "Loading", first.Nodes[0].Stage)
		assert.Equal(t, "worker", string(first.Nodes[0].Role))
	case <-time.After(time.Second):
		t.Fatal("discovery waited for inspections")
	}
	select {
	case next := <-updates:
		assert.Equal(t, "running", next.Nodes[0].Stage)
		assert.Equal(t, "Loading", next.Nodes[1].Stage)
	case <-time.After(time.Second):
		t.Fatal("fast node waited for slow node")
	}
	cancel()
	select {
	case final := <-done:
		require.ErrorIs(t, final.err, context.Canceled)
		require.Len(t, final.nodes.Nodes, 2)
		assert.Equal(t, "running", final.nodes.Nodes[0].Stage)
		assert.NotEmpty(t, final.nodes.Nodes[1].Problem)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop inspections")
	}
}
