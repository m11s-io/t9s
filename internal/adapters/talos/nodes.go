package talos

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	runtimeresource "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"golang.org/x/sync/errgroup"
)

const maxConcurrentNodeInspections = 8

type memberRecord struct {
	ID              string
	Hostname        string
	Addresses       []string
	MachineType     string
	OperatingSystem string
}

type machineRecord struct {
	Stage string
	Ready *bool
}

type serviceRecord struct {
	Name        string
	State       string
	Healthy     *bool
	LastMessage string
	LastChange  time.Time
}

type talosAPI interface {
	Members(context.Context) ([]memberRecord, error)
	MachineStatus(context.Context, string) (machineRecord, error)
	Services(context.Context, string) ([]serviceRecord, error)
	Version(context.Context, string) (string, error)
}

type nodeReader struct {
	api talosAPI
	now func() time.Time
}

var _ ports.NodeReader = (*nodeReader)(nil)

func newNodeReader(api talosAPI, now func() time.Time) *nodeReader {
	return &nodeReader{api: api, now: now}
}

func (r *nodeReader) List(ctx context.Context) (domain.NodeSet, error) {
	members, err := r.api.Members(ctx)
	if err != nil {
		return domain.NodeSet{}, fmt.Errorf("list members: %w", err)
	}

	results := make([]rawNode, len(members))
	semaphore := make(chan struct{}, maxConcurrentNodeInspections)
	group, groupCtx := errgroup.WithContext(ctx)

	for i := range members {
		if err := ctx.Err(); err != nil {
			_ = group.Wait()

			return domain.NodeSet{}, err
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			_ = group.Wait()
			return domain.NodeSet{}, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			<-semaphore
			_ = group.Wait()

			return domain.NodeSet{}, err
		}

		i := i
		group.Go(func() error {
			defer func() { <-semaphore }()
			results[i] = r.inspectNode(groupCtx, members[i])

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return domain.NodeSet{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.NodeSet{}, err
	}

	observedAt := r.now()
	nodes := make([]domain.NodeSnapshot, len(results))
	for i := range results {
		results[i].ObservedAt = observedAt
		nodes[i] = convertNode(results[i])
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].DisplayName() < nodes[j].DisplayName()
	})

	return domain.NodeSet{Nodes: nodes, ObservedAt: observedAt}, nil
}

func (r *nodeReader) inspectNode(ctx context.Context, member memberRecord) rawNode {
	raw := rawNode{
		ID:              member.ID,
		Hostname:        member.Hostname,
		Addresses:       append([]string(nil), member.Addresses...),
		MachineType:     member.MachineType,
		OperatingSystem: member.OperatingSystem,
	}
	target := member.Hostname
	if target == "" && len(member.Addresses) > 0 {
		target = member.Addresses[0]
	}
	if target == "" {
		raw.Problem = "node target unavailable"

		return raw
	}

	problems := make([]string, 0, 3)
	status, err := r.api.MachineStatus(ctx, target)
	if err != nil {
		problems = append(problems, "machine status unavailable")
	} else {
		raw.Stage = status.Stage
		raw.Ready = status.Ready
	}
	if ctx.Err() != nil {
		return raw
	}

	services, err := r.api.Services(ctx, target)
	if err != nil {
		problems = append(problems, "services unavailable")
	} else {
		raw.ServicesKnown = true
		raw.Services = make([]rawService, len(services))
		for i := range services {
			raw.Services[i] = rawService{Healthy: services[i].Healthy}
		}
	}
	if ctx.Err() != nil {
		return raw
	}

	version, err := r.api.Version(ctx, target)
	if err != nil {
		problems = append(problems, "version unavailable")
	} else {
		raw.Version = version
	}
	raw.Problem = strings.Join(problems, "; ")

	return raw
}

type machineryAPI struct {
	client *talosclient.Client
}

func (a *machineryAPI) Members(ctx context.Context) ([]memberRecord, error) {
	members, err := safe.StateListAll[*cluster.Member](ctx, a.client.COSI)
	if err != nil {
		return nil, err
	}

	result := make([]memberRecord, 0, members.Len())
	for member := range members.All() {
		spec := member.TypedSpec()
		addresses := make([]string, len(spec.Addresses))
		for i := range spec.Addresses {
			addresses[i] = spec.Addresses[i].String()
		}
		id := spec.NodeID
		if id == "" {
			id = string(member.Metadata().ID())
		}
		result = append(result, memberRecord{
			ID:              id,
			Hostname:        spec.Hostname,
			Addresses:       addresses,
			MachineType:     spec.MachineType.String(),
			OperatingSystem: spec.OperatingSystem,
		})
	}

	return result, nil
}

func (a *machineryAPI) MachineStatus(ctx context.Context, node string) (machineRecord, error) {
	status, err := safe.StateGet[*runtimeresource.MachineStatus](
		talosclient.WithNode(ctx, node),
		a.client.COSI,
		runtimeresource.NewMachineStatus().Metadata(),
	)
	if err != nil {
		return machineRecord{}, err
	}
	spec := status.TypedSpec()
	ready := spec.Status.Ready

	return machineRecord{Stage: spec.Stage.String(), Ready: &ready}, nil
}

func (a *machineryAPI) Services(ctx context.Context, node string) ([]serviceRecord, error) {
	response, err := a.client.ServiceList(talosclient.WithNode(ctx, node))
	if err != nil {
		return nil, err
	}

	var result []serviceRecord
	for _, message := range response.GetMessages() {
		for _, service := range message.GetServices() {
			record := serviceRecord{Name: service.GetId(), State: service.GetState()}
			health := service.GetHealth()
			if health != nil {
				record.LastMessage = health.GetLastMessage()
				if change := health.GetLastChange(); change != nil {
					record.LastChange = change.AsTime()
				}
				if !health.GetUnknown() {
					healthy := health.GetHealthy()
					record.Healthy = &healthy
				}
			}
			if events := service.GetEvents().GetEvents(); len(events) > 0 {
				last := events[len(events)-1]
				if record.LastMessage == "" {
					record.LastMessage = last.GetMsg()
				}
				if record.LastChange.IsZero() && last.GetTs() != nil {
					record.LastChange = last.GetTs().AsTime()
				}
			}
			result = append(result, record)
		}
	}

	return result, nil
}

func (a *machineryAPI) Version(ctx context.Context, node string) (string, error) {
	response, err := a.client.Version(talosclient.WithNode(ctx, node))
	if err != nil {
		return "", err
	}
	for _, message := range response.GetMessages() {
		if version := message.GetVersion(); version != nil {
			return version.GetTag(), nil
		}
	}

	return "", fmt.Errorf("version response is empty")
}
