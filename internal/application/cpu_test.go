package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cpuTestReader func(context.Context, string) (domain.CPUSnapshot, error)

func (f cpuTestReader) Sample(ctx context.Context, node string) (domain.CPUSnapshot, error) {
	return f(ctx, node)
}
func withCPUReader(m application.Model, r ports.CPUReader) application.Model {
	m, _ = application.Update(m, application.SessionOpened{Generation: m.Generation, CPU: r})
	return m
}
func TestCPURefreshStaysOnOpenedNode(t *testing.T) {
	nodes := []string{}
	m := withCPUReader(application.Model{Generation: 1}, cpuTestReader(func(_ context.Context, node string) (domain.CPUSnapshot, error) {
		nodes = append(nodes, node)
		return domain.CPUSnapshot{Aggregate: domain.CPUUtilization{Known: true, BusyPercent: 30}}, nil
	}))
	m, e := application.Update(m, application.OpenCPU{Node: "cp-1"})
	require.NotNil(t, e)
	m, _ = application.Update(m, e(t.Context(), application.Dependencies{}))
	assert.Equal(t, application.Ready, m.CPU.Status)
	m.NodeFocus = "different"
	m, e = application.Update(m, application.RefreshCPU{})
	require.NotNil(t, e)
	m, next := application.Update(m, e(t.Context(), application.Dependencies{}))
	assert.Nil(t, next, "no background polling")
	assert.Equal(t, []string{"cp-1", "cp-1"}, nodes)
	assert.Equal(t, 30.0, m.CPU.Value.Aggregate.BusyPercent)
}
func TestCPULateSameNodeAndContextResultsAreIgnored(t *testing.T) {
	m := withCPUReader(application.Model{Generation: 3}, cpuTestReader(func(context.Context, string) (domain.CPUSnapshot, error) {
		return domain.CPUSnapshot{RunningProcesses: 7}, nil
	}))
	m, e := application.Update(m, application.OpenCPU{Node: "cp-1"})
	require.NotNil(t, e)
	old := e(t.Context(), application.Dependencies{})
	m, e = application.Update(m, application.RefreshCPU{})
	require.NotNil(t, e)
	fresh := e(t.Context(), application.Dependencies{}).(application.CPULoaded)
	m, _ = application.Update(m, old)
	assert.Equal(t, application.Loading, m.CPU.Status)
	stale := fresh
	stale.Generation = 2
	m, _ = application.Update(m, stale)
	assert.Equal(t, application.Loading, m.CPU.Status)
	stale = fresh
	stale.Node = "other"
	m, _ = application.Update(m, stale)
	assert.Equal(t, application.Loading, m.CPU.Status)
	m, _ = application.Update(m, fresh)
	assert.Equal(t, uint64(7), m.CPU.Value.RunningProcesses)
}
func TestCPUCloseAndContextSwitchCancelSampling(t *testing.T) {
	for _, message := range []application.Message{application.CloseCPU{}, application.SelectContext{Name: "other"}} {
		started := make(chan struct{})
		m := withCPUReader(application.Model{Generation: 1}, cpuTestReader(func(ctx context.Context, _ string) (domain.CPUSnapshot, error) {
			close(started)
			<-ctx.Done()
			return domain.CPUSnapshot{}, ctx.Err()
		}))
		m, e := application.Update(m, application.OpenCPU{Node: "cp-1"})
		require.NotNil(t, e)
		done := make(chan application.Message, 1)
		go func() { done <- e(t.Context(), application.Dependencies{}) }()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("sample did not start")
		}
		m, _ = application.Update(m, message)
		select {
		case result := <-done:
			m, _ = application.Update(m, result)
		case <-time.After(time.Second):
			t.Fatal("sample did not cancel")
		}
		assert.Equal(t, application.CPUState{}, m.CPU)
	}
}

func TestCPUFailedSampleKeepsProcessCountsWithoutReportingValidUtilization(t *testing.T) {
	m := withCPUReader(application.Model{Generation: 1}, cpuTestReader(func(context.Context, string) (domain.CPUSnapshot, error) {
		return domain.CPUSnapshot{Aggregate: domain.CPUUtilization{Known: true, BusyPercent: 30}, Cores: []domain.CPUCoreSnapshot{{ID: 0, CPUUtilization: domain.CPUUtilization{Known: true}}}, ProcessCountsKnown: true, RunningProcesses: 7}, context.Canceled
	}))
	m, e := application.Update(m, application.OpenCPU{Node: "cp-1"})
	m, _ = application.Update(m, e(t.Context(), application.Dependencies{}))
	assert.Equal(t, application.Partial, m.CPU.Status)
	assert.True(t, m.CPU.Value.ProcessCountsKnown)
	assert.Equal(t, uint64(7), m.CPU.Value.RunningProcesses)
	assert.False(t, m.CPU.Value.Aggregate.Known)
	require.Len(t, m.CPU.Value.Cores, 1)
	assert.False(t, m.CPU.Value.Cores[0].Known)
	assert.Equal(t, "CPU sample canceled", m.CPU.Err)
}
