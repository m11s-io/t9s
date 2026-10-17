package talos

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/protobuf/types/known/emptypb"
)

const cpuSampleInterval = time.Second

type cpuClient interface {
	SystemStat(context.Context, string) (*machineapi.SystemStatResponse, error)
}
type machineryCPUClient struct{ client *talosclient.Client }

func (c machineryCPUClient) SystemStat(ctx context.Context, node string) (*machineapi.SystemStatResponse, error) {
	return c.client.MachineClient.SystemStat(talosclient.WithNode(ctx, node), &emptypb.Empty{})
}

type cpuReader struct {
	client cpuClient
	wait   func(context.Context, time.Duration) error
	now    func() time.Time
}

// wait and now are external timing boundaries, allowing tests to exercise the
// real sampling and delta logic without sleeping for a second.
func newCPUReader(client cpuClient, wait func(context.Context, time.Duration) error, now func() time.Time) ports.CPUReader {
	return &cpuReader{client: client, wait: wait, now: now}
}
func waitCPUSample(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
func (r *cpuReader) Sample(ctx context.Context, node string) (domain.CPUSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return domain.CPUSnapshot{}, err
	}
	var snapshot domain.CPUSnapshot
	before, firstErr := r.read(ctx, node)
	firstAt := r.now()
	if before != nil {
		setCPUProcessCounts(&snapshot, before, firstAt)
	} else {
		snapshot.Problems = append(snapshot.Problems, "first CPU sample unavailable")
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if err := r.wait(ctx, cpuSampleInterval); err != nil {
		return snapshot, err
	}
	// A canceled interval must not issue a new RPC, even if the injected wait
	// happens to return nil when cancellation races timer expiry.
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	after, secondErr := r.read(ctx, node)
	secondAt := r.now()
	if after != nil {
		setCPUProcessCounts(&snapshot, after, secondAt)
	} else {
		snapshot.Problems = append(snapshot.Problems, "second CPU sample unavailable")
	}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	coreCount := 0
	if before != nil {
		coreCount = len(before.GetCpu())
	}
	if after != nil {
		coreCount = max(coreCount, len(after.GetCpu()))
	}
	snapshot.Cores = make([]domain.CPUCoreSnapshot, coreCount)
	for i := range snapshot.Cores {
		snapshot.Cores[i].ID = i
	}
	if before == nil || after == nil {
		if firstErr != nil && secondErr != nil {
			return snapshot, errors.New("CPU samples unavailable")
		}
		return snapshot, nil
	}
	snapshot.Interval = secondAt.Sub(firstAt)
	if snapshot.Interval <= 0 {
		snapshot.Problems = append(snapshot.Problems, "CPU sample interval is not positive")
		return snapshot, nil
	}
	if before.GetBootTime() != after.GetBootTime() {
		snapshot.Problems = append(snapshot.Problems, "node boot time changed during CPU sample")
		return snapshot, nil
	}
	if !sameCPUTopology(before.GetCpu(), after.GetCpu()) {
		snapshot.Problems = append(snapshot.Problems, "CPU topology changed during sample")
		return snapshot, nil
	}
	snapshot.Aggregate = cpuUtilization(before.GetCpuTotal(), after.GetCpuTotal())
	if !snapshot.Aggregate.Known {
		snapshot.Problems = append(snapshot.Problems, "aggregate CPU counters unavailable, reset, or invalid")
	}
	if coreCount == 0 {
		snapshot.Problems = append(snapshot.Problems, "per-core CPU counters unavailable")
	}
	for i := range snapshot.Cores {
		snapshot.Cores[i].CPUUtilization = cpuUtilization(before.GetCpu()[i], after.GetCpu()[i])
		if !snapshot.Cores[i].Known {
			snapshot.Problems = append(snapshot.Problems, fmt.Sprintf("CPU%d counters unavailable, reset, or invalid", i))
		}
	}
	return snapshot, nil
}
func (r *cpuReader) read(ctx context.Context, node string) (*machineapi.SystemStat, error) {
	response, err := r.client.SystemStat(ctx, node)
	if err != nil {
		return nil, errors.New("CPU sample unavailable")
	}
	messages := response.GetMessages()
	if len(messages) != 1 || messages[0] == nil || messages[0].GetMetadata().GetError() != "" {
		return nil, errors.New("CPU sample unavailable")
	}
	return messages[0], nil
}
func setCPUProcessCounts(snapshot *domain.CPUSnapshot, stat *machineapi.SystemStat, sampledAt time.Time) {
	snapshot.ProcessCountsKnown = true
	snapshot.RunningProcesses = stat.GetProcessRunning()
	snapshot.BlockedProcesses = stat.GetProcessBlocked()
	snapshot.SampledAt = sampledAt.UTC()
}
func sameCPUTopology(before, after []*machineapi.CPUStat) bool {
	if len(before) != len(after) {
		return false
	}
	for i := range before {
		if (before[i] == nil) != (after[i] == nil) {
			return false
		}
	}
	return true
}
func cpuUtilization(before, after *machineapi.CPUStat) domain.CPUUtilization {
	unknown := domain.CPUUtilization{}
	if before == nil || after == nil {
		return unknown
	}
	old := cpuCounters(before)
	next := cpuCounters(after)
	var total, oldTotal, nextTotal, busy float64
	for i := range old {
		if !finiteCPUCounter(old[i]) || !finiteCPUCounter(next[i]) || next[i] < old[i] {
			return unknown
		}
		// Guest/GuestNice are already accounted for by User/Nice. Validate them
		// for counter resets, but never include them again in the total or busy.
		if i >= 8 {
			continue
		}
		delta := next[i] - old[i]
		total += delta
		oldTotal += old[i]
		nextTotal += next[i]
		if i != 3 && i != 4 {
			busy += delta
		}
	}
	if !finiteCPUCounter(total) || !finiteCPUCounter(oldTotal) || !finiteCPUCounter(nextTotal) || total <= 0 {
		return unknown
	}
	utilization := domain.CPUUtilization{Known: true, BusyPercent: 100 * (busy / total), IOWaitPercent: 100 * ((next[4] - old[4]) / total)}
	if !finiteCPUCounter(utilization.BusyPercent) || !finiteCPUCounter(utilization.IOWaitPercent) || utilization.BusyPercent > 100 || utilization.IOWaitPercent > 100 {
		return unknown
	}
	return utilization
}
func cpuCounters(c *machineapi.CPUStat) [10]float64 {
	return [10]float64{c.GetUser(), c.GetNice(), c.GetSystem(), c.GetIdle(), c.GetIowait(), c.GetIrq(), c.GetSoftIrq(), c.GetSteal(), c.GetGuest(), c.GetGuestNice()}
}
func finiteCPUCounter(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
