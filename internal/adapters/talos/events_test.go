package talos

import (
	"context"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
)

const testFetchTimeout = 50 * time.Millisecond

type fakeMembersAPI struct {
	members []memberRecord
	err     error
}

func (f *fakeMembersAPI) Members(context.Context) ([]memberRecord, error) {
	return f.members, f.err
}

func mustPackEvent(t *testing.T, payload proto.Message) *machineapi.Event {
	t.Helper()
	data, err := proto.Marshal(payload)
	require.NoError(t, err)
	return &machineapi.Event{
		Data: &anypb.Any{
			TypeUrl: "talos/runtime/" + string(payload.ProtoReflect().Descriptor().FullName()),
			Value:   data,
		},
	}
}

type fakeEventsDataStream struct {
	events []*machineapi.Event
	index  int
	ctx    context.Context
}

func (s *fakeEventsDataStream) Recv() (*machineapi.Event, error) {
	if s.index >= len(s.events) {
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

type fakeEventsClient struct {
	events []*machineapi.Event
}

func (c *fakeEventsClient) Events(ctx context.Context, _ string, _ ...talosclient.EventsOptionFunc) (eventsDataStream, error) {
	return &fakeEventsDataStream{events: c.events, ctx: ctx}, nil
}

func TestDescribeEventPayloadSequence(t *testing.T) {
	kind, message := describeEventPayload(&machineapi.SequenceEvent{
		Sequence: "boot",
		Action:   machineapi.SequenceEvent_START,
	})
	assert.Equal(t, "Sequence", kind)
	assert.Equal(t, "boot START", message)
}

func TestDescribeEventPayloadSequenceIncludesError(t *testing.T) {
	kind, message := describeEventPayload(&machineapi.SequenceEvent{
		Sequence: "boot",
		Action:   machineapi.SequenceEvent_STOP,
		Error:    &common.Error{Message: "disk not found"},
	})
	assert.Equal(t, "Sequence", kind)
	assert.Equal(t, "boot STOP: disk not found", message)
}

func TestDescribeEventPayloadServiceState(t *testing.T) {
	kind, message := describeEventPayload(&machineapi.ServiceStateEvent{
		Service: "etcd",
		Action:  machineapi.ServiceStateEvent_RUNNING,
		Message: "member is healthy",
	})
	assert.Equal(t, "ServiceState", kind)
	assert.Equal(t, "etcd: RUNNING — member is healthy", message)
}

func TestDescribeEventPayloadUnknownTypeIsSkipped(t *testing.T) {
	kind, message := describeEventPayload(&emptypb.Empty{})
	assert.Equal(t, "", kind)
	assert.Equal(t, "", message)
}

func TestEventReaderListStopsAfterTailCapWithoutWaitingForFetchTimeout(t *testing.T) {
	events := make([]*machineapi.Event, eventTailPerNode)
	for i := range events {
		events[i] = mustPackEvent(t, &machineapi.TaskEvent{Task: "install", Action: machineapi.TaskEvent_START})
	}
	reader := newEventReader(
		&fakeMembersAPI{members: []memberRecord{{Hostname: "node-1"}}},
		&fakeEventsClient{events: events},
		func() time.Time { return time.Unix(0, 0).UTC() },
		testFetchTimeout,
	)

	started := time.Now()
	set, err := reader.List(t.Context())
	elapsed := time.Since(started)

	require.NoError(t, err)
	assert.Len(t, set.Events, int(eventTailPerNode))
	assert.Less(t, elapsed, testFetchTimeout, "must stop reading once the tail cap is reached, not wait for the fetch timeout")
}

func TestEventReaderListReturnsPartialResultsWhenOneNodeFails(t *testing.T) {
	reader := newEventReader(
		&fakeMembersAPI{members: []memberRecord{{Hostname: "node-1"}, {Hostname: ""}}},
		&fakeEventsClient{events: []*machineapi.Event{
			mustPackEvent(t, &machineapi.TaskEvent{Task: "install", Action: machineapi.TaskEvent_START}),
		}},
		func() time.Time { return time.Unix(0, 0).UTC() },
		testFetchTimeout,
	)

	set, err := reader.List(t.Context())

	require.NoError(t, err)
	require.Len(t, set.Events, 1)
	assert.Equal(t, "node-1", set.Events[0].Node)
}

func TestEventReaderListSkipsUnsupportedEventTypes(t *testing.T) {
	reader := newEventReader(
		&fakeMembersAPI{members: []memberRecord{{Hostname: "node-1"}}},
		&fakeEventsClient{events: []*machineapi.Event{
			{Data: &anypb.Any{TypeUrl: "talos/runtime/does.not.Exist"}},
			mustPackEvent(t, &machineapi.TaskEvent{Task: "install", Action: machineapi.TaskEvent_START}),
		}},
		func() time.Time { return time.Unix(0, 0).UTC() },
		testFetchTimeout,
	)

	set, err := reader.List(t.Context())

	require.NoError(t, err)
	require.Len(t, set.Events, 1)
	assert.Equal(t, "Task", set.Events[0].Kind)
}

type fakeMultiNodeEventsClient struct {
	eventsByNode map[string][]*machineapi.Event
}

func (c *fakeMultiNodeEventsClient) Events(ctx context.Context, node string, _ ...talosclient.EventsOptionFunc) (eventsDataStream, error) {
	return &fakeEventsDataStream{events: c.eventsByNode[node], ctx: ctx}, nil
}

func TestEventReaderListGroupsEventsByNodeRegardlessOfMemberOrder(t *testing.T) {
	client := &fakeMultiNodeEventsClient{eventsByNode: map[string][]*machineapi.Event{
		"node-b": {mustPackEvent(t, &machineapi.TaskEvent{Task: "b-task", Action: machineapi.TaskEvent_START})},
		"node-a": {mustPackEvent(t, &machineapi.TaskEvent{Task: "a-task", Action: machineapi.TaskEvent_START})},
	}}
	reader := newEventReader(
		&fakeMembersAPI{members: []memberRecord{{Hostname: "node-b"}, {Hostname: "node-a"}}},
		client,
		func() time.Time { return time.Unix(0, 0).UTC() },
		testFetchTimeout,
	)

	set, err := reader.List(t.Context())

	require.NoError(t, err)
	require.Len(t, set.Events, 2)
	assert.Equal(t, "node-a", set.Events[0].Node, "results must be grouped by node name, not member-list order")
	assert.Equal(t, "node-b", set.Events[1].Node)
}
