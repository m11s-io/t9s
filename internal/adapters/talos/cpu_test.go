package talos

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeCPUClient struct {
	stats []*machineapi.SystemStat
	errs  []error
	nodes []string
}

func (f *fakeCPUClient) SystemStat(_ context.Context, node string) (*machineapi.SystemStatResponse, error) {
	index := len(f.nodes)
	f.nodes = append(f.nodes, node)
	if index < len(f.errs) && f.errs[index] != nil {
		return nil, f.errs[index]
	}
	if index >= len(f.stats) || f.stats[index] == nil {
		return nil, nil
	}
	return &machineapi.SystemStatResponse{Messages: []*machineapi.SystemStat{f.stats[index]}}, nil
}
func cpuCounterPair() (*machineapi.SystemStat, *machineapi.SystemStat) {
	before := &machineapi.SystemStat{BootTime: 1, CpuTotal: &machineapi.CPUStat{User: 100, Nice: 100, System: 50, Idle: 200, Iowait: 50, Irq: 10, SoftIrq: 5, Steal: 20, Guest: 20, GuestNice: 10}, ProcessRunning: 4, ProcessBlocked: 1}
	after := &machineapi.SystemStat{BootTime: 1, CpuTotal: &machineapi.CPUStat{User: 110, Nice: 104, System: 55, Idle: 260, Iowait: 60, Irq: 13, SoftIrq: 9, Steal: 24, Guest: 25, GuestNice: 12}, ProcessRunning: 7, ProcessBlocked: 2}
	before.Cpu = []*machineapi.CPUStat{before.CpuTotal}
	after.Cpu = []*machineapi.CPUStat{after.CpuTotal}
	return before, after
}
func sampleCPU(t *testing.T, before, after *machineapi.SystemStat) (domain.CPUSnapshot, *fakeCPUClient) {
	t.Helper()
	client := &fakeCPUClient{stats: []*machineapi.SystemStat{before, after}}
	clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	reader := newCPUReader(client, func(ctx context.Context, d time.Duration) error {
		assert.Equal(t, time.Second, d)
		clock = clock.Add(d)
		return ctx.Err()
	}, func() time.Time { return clock })
	got, err := reader.Sample(t.Context(), "cp-1")
	require.NoError(t, err)
	return got, client
}
func TestCPUComputesDeltaWithoutDoubleCountingGuest(t *testing.T) {
	before, after := cpuCounterPair()
	got, client := sampleCPU(t, before, after)
	require.True(t, got.Aggregate.Known)
	assert.InDelta(t, 30, got.Aggregate.BusyPercent, 0.001)
	assert.InDelta(t, 10, got.Aggregate.IOWaitPercent, 0.001)
	require.Len(t, got.Cores, 1)
	assert.True(t, got.Cores[0].Known)
	assert.InDelta(t, 30, got.Cores[0].BusyPercent, 0.001)
	assert.Equal(t, time.Second, got.Interval)
	assert.Equal(t, time.Date(2026, 10, 17, 12, 0, 1, 0, time.UTC), got.SampledAt)
	assert.True(t, got.ProcessCountsKnown)
	assert.Equal(t, uint64(7), got.RunningProcesses)
	assert.Equal(t, uint64(2), got.BlockedProcesses)
	assert.Equal(t, []string{"cp-1", "cp-1"}, client.nodes)
	assert.Empty(t, got.Problems)
}
func TestCPUResetOrHotplugShowsUnknown(t *testing.T) {
	cases := map[string]func(*machineapi.SystemStat, *machineapi.SystemStat){
		"boot changed":      func(_, next *machineapi.SystemStat) { next.BootTime = 2 },
		"counter decreased": func(_, next *machineapi.SystemStat) { next.CpuTotal.User = 99 },
		"zero delta":        func(old, next *machineapi.SystemStat) { next.CpuTotal = old.CpuTotal },
		"negative counter":  func(_, next *machineapi.SystemStat) { next.CpuTotal.System = -1 },
		"nan":               func(_, next *machineapi.SystemStat) { next.CpuTotal.User = math.NaN() },
		"infinity":          func(_, next *machineapi.SystemStat) { next.CpuTotal.User = math.Inf(1) },
		"overflow": func(_, next *machineapi.SystemStat) {
			next.CpuTotal.User = math.MaxFloat64
			next.CpuTotal.System = math.MaxFloat64
		},
		"core added":           func(_, next *machineapi.SystemStat) { next.Cpu = append(next.Cpu, &machineapi.CPUStat{User: 1}) },
		"core removed":         func(_, next *machineapi.SystemStat) { next.Cpu = nil },
		"core replaced by gap": func(_, next *machineapi.SystemStat) { next.Cpu[0] = nil },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			before, after := cpuCounterPair()
			change(before, after)
			got, _ := sampleCPU(t, before, after)
			assert.False(t, got.Aggregate.Known)
			assert.NotEmpty(t, got.Problems)
			assert.True(t, got.ProcessCountsKnown)
		})
	}
}
func TestCPUSparseCoresKeepTheirIDsAndRemainUnknown(t *testing.T) {
	before, after := cpuCounterPair()
	before.Cpu = append(before.Cpu, nil, &machineapi.CPUStat{Idle: 10})
	after.Cpu = append(after.Cpu, nil, &machineapi.CPUStat{Idle: 20})
	got, _ := sampleCPU(t, before, after)
	require.Len(t, got.Cores, 3)
	assert.True(t, got.Aggregate.Known)
	assert.True(t, got.Cores[0].Known)
	assert.False(t, got.Cores[1].Known)
	assert.Equal(t, 1, got.Cores[1].ID)
	assert.True(t, got.Cores[2].Known)
	assert.Equal(t, 2, got.Cores[2].ID)
}
func TestCPUSampleCancellation(t *testing.T) {
	before, _ := cpuCounterPair()
	client := &fakeCPUClient{stats: []*machineapi.SystemStat{before}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiting := make(chan struct{})
	reader := newCPUReader(client, func(ctx context.Context, _ time.Duration) error { close(waiting); <-ctx.Done(); return ctx.Err() }, time.Now)
	done := make(chan error, 1)
	go func() { _, err := reader.Sample(ctx, "cp-1"); done <- err }()
	select {
	case <-waiting:
		cancel()
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		return
	case <-time.After(time.Second):
		t.Fatal("sample did not reach wait")
	}
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("sample wait did not cancel")
	}
	assert.Equal(t, []string{"cp-1"}, client.nodes, "canceled interval must not send the second RPC")
}
func TestCPUPartialSamplePreservesLatestProcessCounts(t *testing.T) {
	for _, failed := range []int{0, 1} {
		t.Run([]string{"first", "second"}[failed], func(t *testing.T) {
			before, after := cpuCounterPair()
			client := &fakeCPUClient{stats: []*machineapi.SystemStat{before, after}, errs: make([]error, 2)}
			client.errs[failed] = errors.New("private endpoint failure")
			clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
			got, err := newCPUReader(client, func(context.Context, time.Duration) error { clock = clock.Add(time.Second); return nil }, func() time.Time { return clock }).Sample(t.Context(), "cp-1")
			require.NoError(t, err)
			assert.False(t, got.Aggregate.Known)
			assert.True(t, got.ProcessCountsKnown)
			assert.NotEmpty(t, got.Problems)
			expected := uint64(7)
			if failed == 1 {
				expected = 4
			}
			assert.Equal(t, expected, got.RunningProcesses)
			assert.NotContains(t, strings.Join(got.Problems, " "), "private endpoint failure")
		})
	}
}
func TestCPUZeroOrNegativeSampleIntervalIsUnknown(t *testing.T) {
	for _, elapsed := range []time.Duration{0, -time.Second} {
		before, after := cpuCounterPair()
		clock := time.Now()
		reader := newCPUReader(&fakeCPUClient{stats: []*machineapi.SystemStat{before, after}}, func(context.Context, time.Duration) error { clock = clock.Add(elapsed); return nil }, func() time.Time { return clock })
		got, err := reader.Sample(t.Context(), "cp-1")
		require.NoError(t, err)
		assert.False(t, got.Aggregate.Known)
		assert.NotEmpty(t, got.Problems)
	}
}

func TestCPUTimerCancellationDoesNotWaitForTheInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan error, 1)
	go func() { done <- waitCPUSample(ctx, time.Hour) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("timer ignored cancellation")
	}
}
func TestCPUUnavailableSamplesFailWithoutInventingProcessCounts(t *testing.T) {
	got, err := newCPUReader(&fakeCPUClient{}, func(context.Context, time.Duration) error { return nil }, time.Now).Sample(t.Context(), "cp-1")
	require.Error(t, err)
	assert.False(t, got.Aggregate.Known)
	assert.False(t, got.ProcessCountsKnown)
	assert.NotEmpty(t, got.Problems)
}

type cpuMachineStub struct {
	machineapi.MachineServiceClient
	captured context.Context
}

func (c *cpuMachineStub) SystemStat(ctx context.Context, _ *emptypb.Empty, _ ...grpc.CallOption) (*machineapi.SystemStatResponse, error) {
	c.captured = ctx
	return &machineapi.SystemStatResponse{}, nil
}
func TestCPUAdapterScopesSystemStatToOneNode(t *testing.T) {
	rpc := &cpuMachineStub{}
	client := machineryCPUClient{client: &talosclient.Client{MachineClient: rpc}}
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("nodes", "other-node", "node", "old-node"))
	_, err := client.SystemStat(ctx, "cp-1")
	require.NoError(t, err)
	outgoing, ok := metadata.FromOutgoingContext(rpc.captured)
	require.True(t, ok)
	assert.Equal(t, []string{"cp-1"}, outgoing.Get("node"))
	assert.Empty(t, outgoing.Get("nodes"))
}
