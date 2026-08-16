package talos

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetailedServicesPreservesSuccessfulRowsAndSanitizedNodeFailure(t *testing.T) {
	healthy := true
	changed := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	reader := newDetailedServiceReader(&fakeDetailedServiceAPI{
		members: []memberRecord{{Hostname: "cp-1"}, {Hostname: "worker-1"}},
		services: map[string][]serviceRecord{
			"cp-1": {{Name: "etcd", State: "Running", Healthy: &healthy, LastMessage: "member healthy", LastChange: changed}},
		},
		errors: map[string]error{"worker-1": errors.New("token=top-secret service endpoint failed")},
	}, func() time.Time { return changed })

	set, err := reader.List(context.Background())

	require.NoError(t, err)
	require.Len(t, set.Services, 1)
	assert.Equal(t, "cp-1", set.Services[0].Node)
	assert.Equal(t, "etcd", set.Services[0].Name)
	assert.Equal(t, "Running", set.Services[0].State)
	assert.Equal(t, "member healthy", set.Services[0].LastMessage)
	assert.Equal(t, changed, set.Services[0].LastChange)
	require.Len(t, set.Problems, 1)
	assert.Equal(t, "worker-1", set.Problems[0].Node)
	assert.Equal(t, "services unavailable", set.Problems[0].Message)
	assert.NotContains(t, fmt.Sprintf("%+v", set), "top-secret")
}

func TestDetailedServicesBoundsRowsAndProblems(t *testing.T) {
	members := make([]memberRecord, 1105)
	errorsByNode := make(map[string]error, len(members))
	servicesByNode := make(map[string][]serviceRecord, len(members))
	for index := range members {
		node := fmt.Sprintf("node-%04d", index)
		members[index] = memberRecord{Hostname: node}
		if index < 105 {
			errorsByNode[node] = errors.New("unavailable")
		} else {
			servicesByNode[node] = []serviceRecord{{Name: "service"}}
		}
	}
	reader := newDetailedServiceReader(&fakeDetailedServiceAPI{members: members, services: servicesByNode, errors: errorsByNode}, time.Now)

	set, err := reader.List(context.Background())

	require.NoError(t, err)
	assert.Len(t, set.Problems, 100)
	assert.Len(t, set.Services, 1000)
}

type fakeDetailedServiceAPI struct {
	members  []memberRecord
	services map[string][]serviceRecord
	errors   map[string]error
}

func (f *fakeDetailedServiceAPI) Members(context.Context) ([]memberRecord, error) {
	return append([]memberRecord(nil), f.members...), nil
}

func (f *fakeDetailedServiceAPI) Services(_ context.Context, node string) ([]serviceRecord, error) {
	if err := f.errors[node]; err != nil {
		return nil, err
	}
	return append([]serviceRecord(nil), f.services[node]...), nil
}
