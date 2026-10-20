package talos

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceDetailUsesRequestedNodeAndServiceAndKeepsNewest50Events(t *testing.T) {
	api := &fakeServiceDetailAPI{record: serviceDetailRecord{ID: "containerd", State: "Running"}}
	for index := 0; index < 60; index++ {
		api.record.Events = append(api.record.Events, domain.ServiceEventSnapshot{
			Time: time.Date(2026, 10, 1, 0, 0, index, 0, time.UTC), TimeKnown: true,
			State: "Running", Message: fmt.Sprintf("event-%02d", index),
		})
	}
	reader := newDetailedServiceReader(api, time.Now)
	detail, err := reader.Detail(t.Context(), "worker-2", "containerd")
	require.NoError(t, err)
	assert.Equal(t, "worker-2", api.node)
	assert.Equal(t, "containerd", api.service)
	require.Len(t, detail.Events, 50)
	assert.Equal(t, "event-10", detail.Events[0].Message)
	assert.Equal(t, "event-59", detail.Events[49].Message)
}

func TestServiceDetailInvalidEventTimestampIsUnknownAndMissingServiceIsUnavailable(t *testing.T) {
	api := &fakeServiceDetailAPI{record: serviceDetailRecord{ID: "containerd", State: "Running", Events: []domain.ServiceEventSnapshot{{Message: "bad timestamp"}}}}
	detail, err := newDetailedServiceReader(api, time.Now).Detail(context.Background(), "node-a", "containerd")
	require.NoError(t, err)
	require.Len(t, detail.Events, 1)
	assert.False(t, detail.Events[0].TimeKnown)

	api.record = serviceDetailRecord{}
	_, err = newDetailedServiceReader(api, time.Now).Detail(context.Background(), "node-a", "missing")
	assert.ErrorContains(t, err, "unavailable")
}

type fakeServiceDetailAPI struct {
	node, service string
	record        serviceDetailRecord
	err           error
}

func (f *fakeServiceDetailAPI) Members(context.Context) ([]memberRecord, error) { return nil, nil }
func (f *fakeServiceDetailAPI) Services(context.Context, string) ([]serviceRecord, error) {
	return nil, nil
}
func (f *fakeServiceDetailAPI) ServiceInfo(_ context.Context, node, service string) (serviceDetailRecord, error) {
	f.node, f.service = node, service
	return f.record, f.err
}
