package talos

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingObservationAPI struct {
	*fakeTalosAPI
	membersCalls  atomic.Int32
	servicesCalls atomic.Int32
}

func (a *countingObservationAPI) Members(ctx context.Context) ([]memberRecord, error) {
	a.membersCalls.Add(1)
	return a.fakeTalosAPI.Members(ctx)
}
func (a *countingObservationAPI) Services(ctx context.Context, target string) ([]serviceRecord, error) {
	a.servicesCalls.Add(1)
	return a.fakeTalosAPI.Services(ctx, target)
}
func TestNodeServicesSharesUncappedObservationOnlyWithinRefresh(t *testing.T) {
	healthy := true
	records := make([]serviceRecord, maxDetailedServices+1)
	for i := range records {
		records[i] = serviceRecord{Name: "service", Healthy: &healthy}
	}
	unhealthy := false
	records[len(records)-1].Healthy = &unhealthy
	api := &countingObservationAPI{fakeTalosAPI: &fakeTalosAPI{
		members:  []memberRecord{{Hostname: "node-1"}},
		services: func(context.Context, string) ([]serviceRecord, error) { return records, nil },
	}}
	reader := newNodeServiceReader(api, time.Now)
	for range 2 {
		nodes, services, err := reader.List(context.Background())
		require.NoError(t, err)
		require.Len(t, nodes.Nodes, 1)
		assert.Len(t, services.Services, maxDetailedServices)
		assert.Equal(t, len(records), nodes.Nodes[0].Services.Total)
		assert.Equal(t, maxDetailedServices, nodes.Nodes[0].Services.Healthy)
	}
	assert.EqualValues(t, 2, api.membersCalls.Load())
	assert.EqualValues(t, 2, api.servicesCalls.Load())
}
