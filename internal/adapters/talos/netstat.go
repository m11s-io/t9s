package talos

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

type netstatClient interface {
	Netstat(ctx context.Context, node string, req *machineapi.NetstatRequest) (*machineapi.NetstatResponse, error)
}

type machineryNetstatClient struct{ client *talosclient.Client }

func (c machineryNetstatClient) Netstat(ctx context.Context, node string, req *machineapi.NetstatRequest) (*machineapi.NetstatResponse, error) {
	return c.client.Netstat(talosclient.WithNode(ctx, node), req)
}

type netstatReader struct {
	client netstatClient
}

func newNetstatReader(client netstatClient) ports.NetstatReader {
	return &netstatReader{client: client}
}

// hostNetworkRequest lists every TCP/UDP socket in the host network namespace
// with owning-process enrichment, matching the shape `ss -tunap` shows.
func hostNetworkRequest() *machineapi.NetstatRequest {
	return &machineapi.NetstatRequest{
		Filter:  machineapi.NetstatRequest_ALL,
		Feature: &machineapi.NetstatRequest_Feature{Pid: true},
		L4Proto: &machineapi.NetstatRequest_L4Proto{Tcp: true, Tcp6: true, Udp: true, Udp6: true},
		Netns:   &machineapi.NetstatRequest_NetNS{Hostnetwork: true},
	}
}

func (r *netstatReader) List(ctx context.Context, node string) (domain.SocketSet, error) {
	response, err := r.client.Netstat(ctx, node, hostNetworkRequest())
	if err != nil {
		return domain.SocketSet{}, fmt.Errorf("list sockets: %w", err)
	}
	messages := response.GetMessages()
	if len(messages) == 0 {
		return domain.SocketSet{}, fmt.Errorf("socket list from %s returned no messages", node)
	}

	records := messages[0].GetConnectrecord()
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].GetL4Proto() != records[j].GetL4Proto() {
			return records[i].GetL4Proto() < records[j].GetL4Proto()
		}
		if records[i].GetLocalport() != records[j].GetLocalport() {
			return records[i].GetLocalport() < records[j].GetLocalport()
		}
		return socketAddress(records[i].GetRemoteip(), records[i].GetRemoteport()) < socketAddress(records[j].GetRemoteip(), records[j].GetRemoteport())
	})

	sockets := make([]domain.SocketSnapshot, len(records))
	for index, record := range records {
		sockets[index] = domain.SocketSnapshot{
			Protocol:      strings.ToLower(record.GetL4Proto()),
			State:         strings.ToLower(machineapi.ConnectRecord_State_name[int32(record.GetState())]),
			LocalAddress:  socketAddress(record.GetLocalip(), record.GetLocalport()),
			RemoteAddress: socketAddress(record.GetRemoteip(), record.GetRemoteport()),
			UID:           record.GetUid(),
			Inode:         record.GetInode(),
			ProcessName:   record.GetProcess().GetName(),
			PID:           record.GetProcess().GetPid(),
			Netns:         record.GetNetns(),
		}
	}

	return domain.SocketSet{Sockets: sockets}, nil
}

// socketAddress renders an IP and port the way `ss` does. A zero port is
// omitted rather than rendered as ":0", and an empty IP with a zero port is
// rendered as "" (an unconnected socket has no remote endpoint).
func socketAddress(ip string, port uint32) string {
	if ip == "" && port == 0 {
		return ""
	}
	if port == 0 {
		return ip
	}
	return net.JoinHostPort(ip, strconv.FormatUint(uint64(port), 10))
}
