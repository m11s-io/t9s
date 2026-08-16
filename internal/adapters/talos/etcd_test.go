package talos

import (
	"context"
	"errors"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEtcdClient struct {
	members    map[string]*machineapi.EtcdMemberListResponse
	membersErr map[string]error
	statuses   map[string]*machineapi.EtcdStatusResponse
	statusErr  map[string]error
}

func (c *fakeEtcdClient) EtcdMemberList(_ context.Context, node string, _ *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
	if err, ok := c.membersErr[node]; ok {
		return nil, err
	}
	return c.members[node], nil
}

func (c *fakeEtcdClient) EtcdStatus(_ context.Context, node string) (*machineapi.EtcdStatusResponse, error) {
	if err, ok := c.statusErr[node]; ok {
		return nil, err
	}
	return c.statuses[node], nil
}

func membersResponse(members ...*machineapi.EtcdMember) *machineapi.EtcdMemberListResponse {
	return &machineapi.EtcdMemberListResponse{
		Messages: []*machineapi.EtcdMembers{{Members: members}},
	}
}

func statusResponse(status *machineapi.EtcdMemberStatus) *machineapi.EtcdStatusResponse {
	return &machineapi.EtcdStatusResponse{
		Messages: []*machineapi.EtcdStatus{{MemberStatus: status}},
	}
}

func TestEtcdReaderMergesMembershipAndStatusByMemberID(t *testing.T) {
	roster := membersResponse(
		&machineapi.EtcdMember{Id: 1, Hostname: "cp-1", ClientUrls: []string{"https://cp-1:2379"}},
		&machineapi.EtcdMember{Id: 2, Hostname: "cp-2", ClientUrls: []string{"https://cp-2:2379"}},
	)
	client := &fakeEtcdClient{
		members: map[string]*machineapi.EtcdMemberListResponse{"cp-1": roster},
		statuses: map[string]*machineapi.EtcdStatusResponse{
			"cp-1": statusResponse(&machineapi.EtcdMemberStatus{MemberId: 1, Leader: 1, DbSize: 100}),
			"cp-2": statusResponse(&machineapi.EtcdMemberStatus{MemberId: 2, Leader: 1, DbSize: 90}),
		},
	}
	reader := newEtcdReader(client)

	set, err := reader.List(t.Context(), []string{"cp-1", "cp-2"})

	require.NoError(t, err)
	require.Len(t, set.Members, 2)
	byHostname := map[string]int{}
	for index, member := range set.Members {
		byHostname[member.Hostname] = index
	}
	leader := set.Members[byHostname["cp-1"]]
	assert.True(t, leader.StatusKnown)
	assert.True(t, leader.IsLeader)
	assert.Equal(t, int64(100), leader.DBSize)
	follower := set.Members[byHostname["cp-2"]]
	assert.True(t, follower.StatusKnown)
	assert.False(t, follower.IsLeader)
	assert.Equal(t, int64(90), follower.DBSize)
}

func TestEtcdReaderTriesMemberListAgainstNextNodeOnFailure(t *testing.T) {
	roster := membersResponse(&machineapi.EtcdMember{Id: 1, Hostname: "cp-2"})
	client := &fakeEtcdClient{
		membersErr: map[string]error{"cp-1": errors.New("unreachable")},
		members:    map[string]*machineapi.EtcdMemberListResponse{"cp-2": roster},
		statuses:   map[string]*machineapi.EtcdStatusResponse{},
	}
	reader := newEtcdReader(client)

	set, err := reader.List(t.Context(), []string{"cp-1", "cp-2"})

	require.NoError(t, err)
	require.Len(t, set.Members, 1)
	assert.Equal(t, "cp-2", set.Members[0].Hostname)
	assert.False(t, set.Members[0].StatusKnown, "no status response was configured for cp-2, so it must degrade rather than fabricate status")
}

func TestEtcdReaderFailsOnlyWhenEveryControlPlaneNodeFailsMemberList(t *testing.T) {
	client := &fakeEtcdClient{
		membersErr: map[string]error{
			"cp-1": errors.New("unreachable"),
			"cp-2": errors.New("unreachable"),
		},
	}
	reader := newEtcdReader(client)

	_, err := reader.List(t.Context(), []string{"cp-1", "cp-2"})

	assert.Error(t, err)
}

func TestEtcdReaderDegradesOneNodeWhenItsStatusCallFails(t *testing.T) {
	roster := membersResponse(
		&machineapi.EtcdMember{Id: 1, Hostname: "cp-1"},
		&machineapi.EtcdMember{Id: 2, Hostname: "cp-2"},
	)
	client := &fakeEtcdClient{
		members: map[string]*machineapi.EtcdMemberListResponse{"cp-1": roster},
		statuses: map[string]*machineapi.EtcdStatusResponse{
			"cp-1": statusResponse(&machineapi.EtcdMemberStatus{MemberId: 1, Leader: 1}),
		},
		statusErr: map[string]error{"cp-2": errors.New("timeout")},
	}
	reader := newEtcdReader(client)

	set, err := reader.List(t.Context(), []string{"cp-1", "cp-2"})

	require.NoError(t, err)
	require.Len(t, set.Members, 2)
	byHostname := map[string]int{}
	for index, member := range set.Members {
		byHostname[member.Hostname] = index
	}
	assert.True(t, set.Members[byHostname["cp-1"]].StatusKnown)
	assert.False(t, set.Members[byHostname["cp-2"]].StatusKnown)
}
