package talos

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetstatReaderListConvertsAndSorts(t *testing.T) {
	client := &fakeNetstatClient{response: &machineapi.NetstatResponse{Messages: []*machineapi.Netstat{
		{Connectrecord: []*machineapi.ConnectRecord{
			{L4Proto: "udp", Localip: "10.0.0.2", Localport: 53, State: machineapi.ConnectRecord_LISTEN},
			{
				L4Proto: "tcp", Localip: "10.0.0.1", Localport: 6443,
				Remoteip: "10.0.0.9", Remoteport: 5000, State: machineapi.ConnectRecord_ESTABLISHED,
				Process: &machineapi.ConnectRecord_Process{Pid: 42, Name: "kubelet"},
			},
		}},
	}}}
	reader := newNetstatReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	require.NoError(t, err)
	require.Len(t, set.Sockets, 2)

	assert.Equal(t, "tcp", set.Sockets[0].Protocol)
	assert.Equal(t, "established", set.Sockets[0].State)
	assert.Equal(t, "10.0.0.1:6443", set.Sockets[0].LocalAddress)
	assert.Equal(t, "10.0.0.9:5000", set.Sockets[0].RemoteAddress)
	assert.Equal(t, "kubelet", set.Sockets[0].ProcessName)
	assert.Equal(t, uint32(42), set.Sockets[0].PID)

	assert.Equal(t, "udp", set.Sockets[1].Protocol)
	assert.Equal(t, "listen", set.Sockets[1].State)
	assert.Equal(t, "10.0.0.2:53", set.Sockets[1].LocalAddress)
	assert.Equal(t, "", set.Sockets[1].RemoteAddress)
}

func TestNetstatReaderListBuildsHostNetworkRequest(t *testing.T) {
	client := &fakeNetstatClient{response: &machineapi.NetstatResponse{Messages: []*machineapi.Netstat{{}}}}
	reader := newNetstatReader(client)

	_, err := reader.List(t.Context(), "cp-1")
	require.NoError(t, err)

	require.NotNil(t, client.request)
	assert.Equal(t, "cp-1", client.node)
	assert.Equal(t, machineapi.NetstatRequest_ALL, client.request.GetFilter())
	assert.True(t, client.request.GetFeature().GetPid())
	l4 := client.request.GetL4Proto()
	assert.True(t, l4.GetTcp())
	assert.True(t, l4.GetTcp6())
	assert.True(t, l4.GetUdp())
	assert.True(t, l4.GetUdp6())
	assert.True(t, client.request.GetNetns().GetHostnetwork())
}

func TestNetstatReaderListErrorsOnClientFailure(t *testing.T) {
	client := &fakeNetstatClient{err: errors.New("unreachable")}
	reader := newNetstatReader(client)

	_, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
}

func TestNetstatReaderListErrorsOnEmptyMessages(t *testing.T) {
	client := &fakeNetstatClient{response: &machineapi.NetstatResponse{}}
	reader := newNetstatReader(client)

	set, err := reader.List(t.Context(), "cp-1")

	assert.Error(t, err)
	assert.Empty(t, set.Sockets)
}

type fakeNetstatClient struct {
	response *machineapi.NetstatResponse
	err      error

	node    string
	request *machineapi.NetstatRequest
}

func (f *fakeNetstatClient) Netstat(_ context.Context, node string, req *machineapi.NetstatRequest) (*machineapi.NetstatResponse, error) {
	f.node = node
	f.request = req
	return f.response, f.err
}
