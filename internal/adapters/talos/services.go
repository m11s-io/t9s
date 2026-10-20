package talos

import (
	"context"
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"sync"
)

const (
	maxDetailedServices  = 1000
	maxServiceProblems   = 100
	maxServiceEventRunes = 512
	maxServiceHistory    = 50
)

type detailedServiceAPI interface {
	Members(context.Context) ([]memberRecord, error)
	Services(context.Context, string) ([]serviceRecord, error)
}

type serviceDetailAPI interface {
	ServiceInfo(context.Context, string, string) (serviceDetailRecord, error)
}

type detailedServiceReader struct {
	api       detailedServiceAPI
	detailAPI serviceDetailAPI
	now       func() time.Time
}

var _ ports.ServiceReader = (*detailedServiceReader)(nil)

func newDetailedServiceReader(api detailedServiceAPI, now func() time.Time) *detailedServiceReader {
	detailAPI, _ := api.(serviceDetailAPI)
	return &detailedServiceReader{api: api, detailAPI: detailAPI, now: now}
}

func (r *detailedServiceReader) Detail(ctx context.Context, node, name string) (domain.ServiceDetail, error) {
	detail := domain.ServiceDetail{Node: node, Service: name, Events: make([]domain.ServiceEventSnapshot, 0)}
	if node == "" || name == "" {
		return detail, fmt.Errorf("service detail requires a node and service")
	}
	if r.detailAPI == nil {
		return detail, fmt.Errorf("service detail is unavailable")
	}
	record, err := r.detailAPI.ServiceInfo(ctx, node, name)
	if record.ID == "" {
		if err != nil {
			return detail, err
		}
		return detail, fmt.Errorf("service %q is unavailable on node %q", name, node)
	}
	detail.State, detail.Healthy = record.State, record.Healthy
	if record.MetadataProblem != "" {
		detail.Problems = append(detail.Problems, record.MetadataProblem)
	}
	detail.Events = append(detail.Events, record.Events...)
	sort.SliceStable(detail.Events, func(i, j int) bool {
		if detail.Events[i].TimeKnown != detail.Events[j].TimeKnown {
			return detail.Events[i].TimeKnown
		}
		if !detail.Events[i].TimeKnown {
			return false
		}
		return detail.Events[i].Time.Before(detail.Events[j].Time)
	})
	if len(detail.Events) > maxServiceHistory {
		detail.Events = append([]domain.ServiceEventSnapshot(nil), detail.Events[len(detail.Events)-maxServiceHistory:]...)
	}
	for index := range detail.Events {
		detail.Events[index].Message = boundRunes(detail.Events[index].Message, maxServiceEventRunes)
	}
	if err != nil {
		detail.Problems = append(detail.Problems, "service detail is partially unavailable")
		return detail, err
	}
	return detail, nil
}

func (r *detailedServiceReader) List(ctx context.Context) (domain.ServiceSet, error) {
	members, err := r.api.Members(ctx)
	if err != nil {
		return domain.ServiceSet{}, err
	}
	type nodeServices struct {
		records []serviceRecord
		err     error
	}
	observations := make([]nodeServices, len(members))
	semaphore := make(chan struct{}, maxConcurrentNodeInspections)
	var group sync.WaitGroup
	for i, member := range members {
		if ctx.Err() != nil {
			break
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			group.Wait()
			return domain.ServiceSet{}, ctx.Err()
		}
		group.Add(1)
		go func(i int, member memberRecord) {
			defer group.Done()
			defer func() { <-semaphore }()
			_, target := serviceNodeAndTarget(member)
			if target != "" && ctx.Err() == nil {
				observations[i].records, observations[i].err = r.api.Services(ctx, target)
			}
		}(i, member)
	}
	group.Wait()
	if err := ctx.Err(); err != nil {
		return domain.ServiceSet{}, err
	}
	result := make([]domain.ServiceSnapshot, 0, min(len(members), maxDetailedServices))
	problems := make([]domain.ServiceProblem, 0)
	for i, member := range members {
		node, target := serviceNodeAndTarget(member)
		if target == "" {
			problems = appendServiceProblem(problems, domain.ServiceProblem{Node: node, Message: "node target unavailable"})
			continue
		}
		observation := observations[i]
		if observation.err != nil {
			problems = appendServiceProblem(problems, domain.ServiceProblem{Node: node, Message: "services unavailable"})
			continue
		}
		for _, service := range observation.records {
			if len(result) == maxDetailedServices {
				break
			}
			result = append(result, domain.ServiceSnapshot{
				Node: node, Name: service.Name, State: service.State, Healthy: service.Healthy,
				LastMessage: boundRunes(service.LastMessage, maxServiceEventRunes), LastChange: service.LastChange,
			})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Node == result[j].Node {
			return result[i].Name < result[j].Name
		}
		return result[i].Node < result[j].Node
	})
	return domain.ServiceSet{Services: result, Problems: problems, ObservedAt: r.now()}, nil
}

func serviceNodeAndTarget(member memberRecord) (string, string) {
	node := member.Hostname
	if node == "" && len(member.Addresses) > 0 {
		node = member.Addresses[0]
	}
	if node == "" {
		node = member.ID
	}
	return node, node
}

func appendServiceProblem(problems []domain.ServiceProblem, problem domain.ServiceProblem) []domain.ServiceProblem {
	if len(problems) == maxServiceProblems {
		return problems
	}
	return append(problems, problem)
}

func boundRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}
