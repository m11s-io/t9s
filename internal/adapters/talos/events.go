package talos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

const (
	eventTailPerNode  int32         = 50
	eventFetchTimeout time.Duration = 5 * time.Second
)

func describeEventPayload(payload proto.Message) (kind, message string) {
	switch value := payload.(type) {
	case *machineapi.SequenceEvent:
		message = strings.TrimSpace(value.GetSequence() + " " + value.GetAction().String())
		if errMsg := value.GetError().GetMessage(); errMsg != "" {
			message += ": " + errMsg
		}
		return "Sequence", message
	case *machineapi.PhaseEvent:
		return "Phase", strings.TrimSpace(value.GetPhase() + " " + value.GetAction().String())
	case *machineapi.TaskEvent:
		return "Task", strings.TrimSpace(value.GetTask() + " " + value.GetAction().String())
	case *machineapi.ServiceStateEvent:
		message = value.GetService() + ": " + value.GetAction().String()
		if msg := value.GetMessage(); msg != "" {
			message += " — " + msg
		}
		return "ServiceState", message
	case *machineapi.MachineStatusEvent:
		message = value.GetStage().String()
		if status := value.GetStatus(); status != nil {
			if status.GetReady() {
				message += " (ready)"
			} else if len(status.GetUnmetConditions()) > 0 {
				message += " (not ready)"
			}
		}
		return "MachineStatus", message
	case *machineapi.RestartEvent:
		return "Restart", fmt.Sprintf("restart requested (cmd=%d)", value.GetCmd())
	case *machineapi.ConfigLoadErrorEvent:
		return "ConfigLoadError", value.GetError()
	case *machineapi.ConfigValidationErrorEvent:
		return "ConfigValidationError", value.GetError()
	case *machineapi.AddressEvent:
		return "Address", strings.TrimSpace(value.GetHostname() + ": " + strings.Join(value.GetAddresses(), ", "))
	default:
		return "", ""
	}
}

type eventsDataStream interface {
	Recv() (*machineapi.Event, error)
}

type eventsClient interface {
	Events(ctx context.Context, node string, opts ...talosclient.EventsOptionFunc) (eventsDataStream, error)
}

type machineryEventsClient struct{ client *talosclient.Client }

func (c machineryEventsClient) Events(ctx context.Context, node string, opts ...talosclient.EventsOptionFunc) (eventsDataStream, error) {
	return c.client.Events(talosclient.WithNode(ctx, node), opts...)
}

type eventMembersAPI interface {
	Members(context.Context) ([]memberRecord, error)
}

type eventReader struct {
	members      eventMembersAPI
	events       eventsClient
	now          func() time.Time
	fetchTimeout time.Duration
}

func newEventReader(members eventMembersAPI, events eventsClient, now func() time.Time, fetchTimeout time.Duration) ports.EventReader {
	return &eventReader{members: members, events: events, now: now, fetchTimeout: fetchTimeout}
}

func (r *eventReader) List(ctx context.Context) (domain.EventSet, error) {
	members, err := r.members.Members(ctx)
	if err != nil {
		return domain.EventSet{}, fmt.Errorf("list members: %w", err)
	}

	results := make([][]domain.EventSnapshot, len(members))
	semaphore := make(chan struct{}, maxConcurrentNodeInspections)
	group, groupCtx := errgroup.WithContext(ctx)

	for i := range members {
		if err := ctx.Err(); err != nil {
			_ = group.Wait()
			return domain.EventSet{}, err
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			_ = group.Wait()
			return domain.EventSet{}, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			<-semaphore
			_ = group.Wait()
			return domain.EventSet{}, err
		}

		i := i
		group.Go(func() error {
			defer func() { <-semaphore }()
			results[i] = r.nodeEvents(groupCtx, members[i])
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return domain.EventSet{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.EventSet{}, err
	}

	var events []domain.EventSnapshot
	for i := range results {
		events = append(events, results[i]...)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Node < events[j].Node
	})

	return domain.EventSet{Events: events}, nil
}

func (r *eventReader) nodeEvents(ctx context.Context, member memberRecord) []domain.EventSnapshot {
	target := member.Hostname
	if target == "" && len(member.Addresses) > 0 {
		target = member.Addresses[0]
	}
	if target == "" {
		return nil
	}

	nodeCtx, cancel := context.WithTimeout(ctx, r.fetchTimeout)
	defer cancel()

	stream, err := r.events.Events(nodeCtx, target, talosclient.WithTailEvents(eventTailPerNode))
	if err != nil {
		return nil
	}

	var snapshots []domain.EventSnapshot
	for int32(len(snapshots)) < eventTailPerNode {
		raw, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || nodeCtx.Err() != nil {
				return snapshots
			}
			return snapshots
		}

		decoded, err := talosclient.UnmarshalEvent(raw)
		if err != nil {
			var unsupported talosclient.EventNotSupportedError
			if errors.As(err, &unsupported) {
				continue
			}
			continue
		}
		if decoded == nil {
			continue
		}

		kind, message := describeEventPayload(decoded.Payload)
		if kind == "" {
			continue
		}

		snapshots = append(snapshots, domain.EventSnapshot{
			Node:       target,
			Kind:       kind,
			Message:    message,
			ObservedAt: r.now(),
		})
	}

	return snapshots
}
