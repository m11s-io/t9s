package talos

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeReaderReturnsNodesSortedByDisplayNameWithOneObservationTime(t *testing.T) {
	now := time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC)
	ready := true
	api := &fakeTalosAPI{members: []memberRecord{
		{ID: "3", Hostname: "worker-z", Addresses: []string{"10.0.0.3"}, MachineType: "worker"},
		{ID: "1", Hostname: "control-a", Addresses: []string{"10.0.0.1"}, MachineType: "controlplane"},
		{ID: "2", Addresses: []string{"10.0.0.2"}, MachineType: "worker"},
	}, machineStatus: func(context.Context, string) (machineRecord, error) {
		return machineRecord{Stage: "running", Ready: &ready}, nil
	}}

	got, err := newNodeReader(api, func() time.Time { return now }).List(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Nodes, 3)
	assert.Equal(t, []string{"10.0.0.2", "control-a", "worker-z"}, []string{
		got.Nodes[0].DisplayName(), got.Nodes[1].DisplayName(), got.Nodes[2].DisplayName(),
	})
	assert.Equal(t, now, got.ObservedAt)
	for _, node := range got.Nodes {
		assert.Equal(t, now, node.ObservedAt)
	}
}

func TestNodeReaderBoundsConcurrentNodeInspectionsAtEight(t *testing.T) {
	members := make([]memberRecord, 40)
	for i := range members {
		members[i] = memberRecord{ID: fmt.Sprintf("machine-%02d", i), Hostname: fmt.Sprintf("node-%02d", i)}
	}
	api := &fakeTalosAPI{members: members, delay: 2 * time.Millisecond}

	got, err := newNodeReader(api, time.Now).List(context.Background())
	require.NoError(t, err)
	assert.Len(t, got.Nodes, 40)
	assert.LessOrEqual(t, api.maxConcurrent(), 8)
	assert.Greater(t, api.maxConcurrent(), 1)
}

func TestNodeReaderKeepsObjectiveIdentityWhenInspectionFails(t *testing.T) {
	api := &fakeTalosAPI{
		members: []memberRecord{{
			ID: "machine-1", Hostname: "cp-1", Addresses: []string{"10.0.0.1"},
			MachineType: "controlplane", OperatingSystem: "Talos",
		}},
		machineStatus: func(context.Context, string) (machineRecord, error) {
			return machineRecord{}, errors.New("token=top-secret status endpoint")
		},
		services: func(context.Context, string) ([]serviceRecord, error) {
			return nil, errors.New("certificate=top-secret services endpoint")
		},
		version: func(context.Context, string) (string, error) {
			return "", errors.New("key=top-secret version endpoint")
		},
	}

	got, err := newNodeReader(api, time.Now).List(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Nodes, 1)
	node := got.Nodes[0]
	assert.Equal(t, "machine-1", node.ID)
	assert.Equal(t, "cp-1", node.Name)
	assert.Equal(t, []string{"10.0.0.1"}, node.Addresses)
	assert.Equal(t, domain.NodeRoleControl, node.Role)
	assert.Equal(t, domain.HealthUnknown, node.Health)
	assert.Equal(t, domain.ServiceSummary{}, node.Services)
	assert.Empty(t, node.Version)
	assert.NotEmpty(t, node.Problem)
	assert.NotContains(t, node.Problem, "top-secret")
	assert.NotContains(t, node.Problem, "endpoint")
}

func TestNodeReaderReturnsMemberDiscoveryFailureWithoutFabricatingNodes(t *testing.T) {
	api := &fakeTalosAPI{membersErr: errors.New("discovery unavailable")}

	got, err := newNodeReader(api, time.Now).List(context.Background())

	require.ErrorContains(t, err, "list members")
	assert.Empty(t, got.Nodes)
}

func TestNodeReaderCancellationStopsSchedulingNewNodeWork(t *testing.T) {
	members := make([]memberRecord, 40)
	for i := range members {
		members[i] = memberRecord{ID: fmt.Sprintf("machine-%02d", i), Hostname: fmt.Sprintf("node-%02d", i)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	api := &fakeTalosAPI{
		members: members,
		machineStatus: func(ctx context.Context, _ string) (machineRecord, error) {
			once.Do(cancel)
			<-ctx.Done()
			return machineRecord{}, ctx.Err()
		},
	}

	got, err := newNodeReader(api, time.Now).List(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got.Nodes)
	assert.LessOrEqual(t, api.machineStatusCalls(), 8)
}

func TestNodeReaderAlreadyCanceledDoesNotScheduleNodeWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := &fakeTalosAPI{members: []memberRecord{{ID: "machine-1", Hostname: "node-1"}}}

	for range 100 {
		got, err := newNodeReader(api, time.Now).List(ctx)
		require.ErrorIs(t, err, context.Canceled)
		assert.Empty(t, got.Nodes)
	}

	assert.Zero(t, api.machineStatusCalls())
}

type fakeTalosAPI struct {
	members       []memberRecord
	membersErr    error
	machineStatus func(context.Context, string) (machineRecord, error)
	services      func(context.Context, string) ([]serviceRecord, error)
	version       func(context.Context, string) (string, error)
	delay         time.Duration

	mu          sync.Mutex
	current     int
	maximum     int
	statusCalls int
}

func (f *fakeTalosAPI) Members(context.Context) ([]memberRecord, error) {
	return append([]memberRecord(nil), f.members...), f.membersErr
}

func (f *fakeTalosAPI) MachineStatus(ctx context.Context, node string) (machineRecord, error) {
	f.enter()
	defer f.leave()
	f.mu.Lock()
	f.statusCalls++
	f.mu.Unlock()
	if f.machineStatus != nil {
		return f.machineStatus(ctx, node)
	}
	wait(ctx, f.delay)
	return machineRecord{}, nil
}

func (f *fakeTalosAPI) Services(ctx context.Context, node string) ([]serviceRecord, error) {
	f.enter()
	defer f.leave()
	if f.services != nil {
		return f.services(ctx, node)
	}
	wait(ctx, f.delay)
	return []serviceRecord{}, nil
}

func (f *fakeTalosAPI) Version(ctx context.Context, node string) (string, error) {
	f.enter()
	defer f.leave()
	if f.version != nil {
		return f.version(ctx, node)
	}
	wait(ctx, f.delay)
	return "v1.13.3", nil
}

func (f *fakeTalosAPI) enter() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current++
	if f.current > f.maximum {
		f.maximum = f.current
	}
}

func (f *fakeTalosAPI) leave() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current--
}

func (f *fakeTalosAPI) maxConcurrent() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maximum
}

func (f *fakeTalosAPI) machineStatusCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusCalls
}

func wait(ctx context.Context, delay time.Duration) {
	if delay == 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
