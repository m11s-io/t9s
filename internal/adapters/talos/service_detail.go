package talos

import (
	"context"

	"github.com/m11s-io/t9s/internal/domain"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type serviceDetailRecord struct {
	ID              string
	State           string
	Healthy         *bool
	Events          []domain.ServiceEventSnapshot
	MetadataProblem string
}

func (a *machineryAPI) ServiceInfo(ctx context.Context, node, service string) (serviceDetailRecord, error) {
	record := serviceDetailRecord{Events: make([]domain.ServiceEventSnapshot, 0)}
	results, err := a.client.ServiceInfo(talosclient.WithNode(ctx, node), service)
	for _, result := range results {
		if result.Service == nil || result.Service.GetId() != service {
			continue
		}
		if record.ID == "" {
			record.ID = result.Service.GetId()
			record.State = result.Service.GetState()
			if health := result.Service.GetHealth(); health != nil && !health.GetUnknown() {
				healthy := health.GetHealthy()
				record.Healthy = &healthy
			}
			if result.Metadata != nil {
				record.MetadataProblem = result.Metadata.GetError()
			}
			for _, event := range result.Service.GetEvents().GetEvents() {
				if event == nil {
					continue
				}
				normalized := domain.ServiceEventSnapshot{State: event.GetState(), Message: event.GetMsg()}
				if timestamp := event.GetTs(); timestamp != nil && timestamp.CheckValid() == nil {
					normalized.Time, normalized.TimeKnown = timestamp.AsTime(), true
				}
				record.Events = append(record.Events, normalized)
			}
		}
	}
	return record, err
}

var _ serviceDetailAPI = (*machineryAPI)(nil)
