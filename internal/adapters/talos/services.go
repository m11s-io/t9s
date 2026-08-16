package talos

import (
	"context"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

const (
	maxDetailedServices  = 1000
	maxServiceProblems   = 100
	maxServiceEventRunes = 512
)

type detailedServiceAPI interface {
	Members(context.Context) ([]memberRecord, error)
	Services(context.Context, string) ([]serviceRecord, error)
}

type detailedServiceReader struct {
	api detailedServiceAPI
	now func() time.Time
}

var _ ports.ServiceReader = (*detailedServiceReader)(nil)

func newServiceReader(client *talosclient.Client, now func() time.Time) ports.ServiceReader {
	return newDetailedServiceReader(&machineryAPI{client: client}, now)
}

func newDetailedServiceReader(api detailedServiceAPI, now func() time.Time) *detailedServiceReader {
	return &detailedServiceReader{api: api, now: now}
}

func (r *detailedServiceReader) List(ctx context.Context) (domain.ServiceSet, error) {
	members, err := r.api.Members(ctx)
	if err != nil {
		return domain.ServiceSet{}, err
	}
	result := make([]domain.ServiceSnapshot, 0, min(len(members), maxDetailedServices))
	problems := make([]domain.ServiceProblem, 0)
	for _, member := range members {
		if err := ctx.Err(); err != nil {
			return domain.ServiceSet{}, err
		}
		node, target := serviceNodeAndTarget(member)
		if target == "" {
			problems = appendServiceProblem(problems, domain.ServiceProblem{Node: node, Message: "node target unavailable"})
			continue
		}
		services, err := r.api.Services(ctx, target)
		if err != nil {
			if ctx.Err() != nil {
				return domain.ServiceSet{}, ctx.Err()
			}
			problems = appendServiceProblem(problems, domain.ServiceProblem{Node: node, Message: "services unavailable"})
			continue
		}
		for _, service := range services {
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
