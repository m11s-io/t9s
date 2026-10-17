package talos

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type followEventsClient struct {
	events   map[string][]*machineapi.Event
	failures map[string]error
	contexts chan context.Context
}

func (c *followEventsClient) Events(ctx context.Context, node string, opts ...talosclient.EventsOptionFunc) (eventsDataStream, error) {
	req := &machineapi.EventsRequest{}
	for _, opt := range opts {
		opt(req)
	}
	// New-only follow must not replay the snapshot tail.
	if req.TailEvents != 0 {
		return nil, errors.New("follow requested historical events")
	}
	if c.contexts != nil {
		c.contexts <- ctx
	}
	if err := c.failures[node]; err != nil {
		return nil, err
	}
	return &fakeEventsDataStream{events: c.events[node], ctx: ctx}, nil
}

func TestEventFollowMergesNodesReportsPartialFailureAndCancels(t *testing.T) {
	client := &followEventsClient{events: map[string][]*machineapi.Event{
		"cp-1": {mustPackEvent(t, &machineapi.TaskEvent{Task: "one", Action: machineapi.TaskEvent_START})},
		"cp-2": {mustPackEvent(t, &machineapi.TaskEvent{Task: "two", Action: machineapi.TaskEvent_START})},
	}, failures: map[string]error{"cp-3": errors.New("sensitive backend detail")}, contexts: make(chan context.Context, 3)}
	reader := newEventReader(&fakeMembersAPI{members: []memberRecord{{Hostname: "cp-1"}, {Hostname: "cp-2"}, {Hostname: "cp-3"}}}, client, time.Now, testFetchTimeout)
	stream, err := reader.Open(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	nodes := map[string]bool{}
	problem := false
	for len(nodes) < 2 || !problem {
		batch, err := stream.Next(ctx)
		require.NoError(t, err)
		for _, event := range batch.Events {
			nodes[event.Node] = true
		}
		for _, p := range batch.Problems {
			assert.Equal(t, "cp-3", p.Node)
			assert.NotContains(t, p.Message, "sensitive")
			problem = true
		}
	}
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	for range 3 {
		nodeCtx := <-client.contexts
		select {
		case <-nodeCtx.Done():
		case <-ctx.Done():
			t.Fatal("node receive was not canceled")
		}
	}
	_, err = stream.Next(ctx)
	require.Error(t, err)
}

func TestEventFollowEmptyMembershipEnds(t *testing.T) {
	reader := newEventReader(&fakeMembersAPI{}, &fakeEventsClient{}, time.Now, testFetchTimeout)
	stream, err := reader.Open(t.Context())
	require.NoError(t, err)
	defer stream.Close()
	_, err = stream.Next(t.Context())
	assert.ErrorIs(t, err, io.EOF)
}

func TestEventFollowNextCancellationStopsUnderlyingReceives(t *testing.T) {
	client := &followEventsClient{contexts: make(chan context.Context, 1)}
	reader := newEventReader(&fakeMembersAPI{members: []memberRecord{{Hostname: "cp-1"}}}, client, time.Now, testFetchTimeout)
	stream, err := reader.Open(t.Context())
	require.NoError(t, err)
	defer stream.Close()
	nodeCtx := <-client.contexts
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = stream.Next(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	select {
	case <-nodeCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("receive survived cancellation")
	}
}
