package talos

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProcessClient struct {
	response *machineapi.ProcessesResponse
	err      error
}

func (c *fakeProcessClient) Processes(context.Context, string) (*machineapi.ProcessesResponse, error) {
	return c.response, c.err
}

func TestProcessReaderListConvertsAndSortsByPID(t *testing.T) {
	client := &fakeProcessClient{response: &machineapi.ProcessesResponse{
		Messages: []*machineapi.Process{{Processes: []*machineapi.ProcessInfo{
			{Pid: 42, Command: "second"},
			{Pid: 7, Command: "first"},
		}}},
	}}
	reader := newProcessReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	require.Len(t, set.Processes, 2)
	assert.Equal(t, int32(7), set.Processes[0].PID)
	assert.Equal(t, "first", set.Processes[0].Command)
	assert.Equal(t, int32(42), set.Processes[1].PID)
}

func TestProcessReaderListReturnsErrorWhenClientFails(t *testing.T) {
	client := &fakeProcessClient{err: errors.New("unreachable")}
	reader := newProcessReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}

func TestProcessReaderListReturnsErrorWhenResponseHasNoMessages(t *testing.T) {
	client := &fakeProcessClient{response: &machineapi.ProcessesResponse{}}
	reader := newProcessReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}
