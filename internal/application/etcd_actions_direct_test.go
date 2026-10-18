package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func directEtcdMembers() domain.EtcdSet {
	return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1"},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
	}}
}

func directEtcdAdapter(members domain.EtcdSet, ops *testkit.FakeEtcdOperations) *testkit.FakeSession {
	return &testkit.FakeSession{
		NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
			return domain.NodeSet{Nodes: []domain.NodeSnapshot{
				{ID: "id-1", Name: "cp-1", Role: domain.NodeRoleControl},
				{ID: "id-2", Name: "cp-2", Role: domain.NodeRoleControl},
				{ID: "id-3", Name: "cp-3", Role: domain.NodeRoleControl},
			}}, nil
		}},
		EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) { return members, nil }},
		EtcdOps:    ops,
	}
}

func nodesOnly(names ...string) domain.NodeSet {
	set := domain.NodeSet{}
	for i, name := range names {
		set.Nodes = append(set.Nodes, domain.NodeSnapshot{ID: fmt.Sprintf("id-%d", i+1), Name: name, Role: domain.NodeRoleControl})
	}
	return set
}

func openDirectEtcdSession(t *testing.T, adapter *testkit.FakeSession) *application.Session {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{
		OpenFunc: func(context.Context, string) (ports.Session, error) { return adapter, nil },
	}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestSnapshotPlanCannotReplay(t *testing.T) {
	var calls int
	session := openDirectEtcdSession(t, directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			calls++
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("a", 64)}, nil
		},
	}))
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionSnapshot, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1", SnapshotPath: "/tmp/one.db"})
	require.NoError(t, err)
	first, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	require.NoError(t, err)
	assert.Equal(t, domain.OperationCompleted, first.Targets[0].Outcome)
	_, err = session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	assert.ErrorIs(t, err, application.ErrPlanUsed)
	assert.Equal(t, 1, calls)
}

func TestMembershipWaitsForVerifiedSnapshot(t *testing.T) {
	var removed bool
	session := openDirectEtcdSession(t, directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("b", 64)}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error { removed = true; return nil },
	}))
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionRemoveMember, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1", SnapshotPath: "/tmp/verified.db"})
	require.NoError(t, err)
	result, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NotNil(t, result.Snapshot)
	assert.Equal(t, domain.OperationAccepted, result.Targets[0].Outcome)
}

func TestMembershipRechecksAfterSnapshot(t *testing.T) {
	read := 0
	var removed bool
	adapter := directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("c", 64)}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error { removed = true; return nil },
	})
	adapter.EtcdReader = &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
		read++
		members := directEtcdMembers()
		if read >= 3 { // preparation, pre-snapshot, then post-snapshot read
			members.Members[1].Errors = []string{"unhealthy"}
		}
		return members, nil
	}}
	session := openDirectEtcdSession(t, adapter)
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionRemoveMember, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1", SnapshotPath: "/tmp/recheck.db"})
	require.NoError(t, err)
	result, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	assert.Error(t, err)
	assert.False(t, removed)
	assert.NotNil(t, result.Snapshot, "completed snapshot metadata survives a failed second stage")
}

func TestMembershipNeverFollowsChangedMemberIdentity(t *testing.T) {
	read := 0
	var removed bool
	adapter := directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("d", 64)}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error { removed = true; return nil },
	})
	adapter.EtcdReader = &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
		read++
		members := directEtcdMembers()
		if read >= 3 {
			members.Members[0].Hostname = "renamed"
		}
		return members, nil
	}}
	session := openDirectEtcdSession(t, adapter)
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionRemoveMember, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1", SnapshotPath: "/tmp/identity.db"})
	require.NoError(t, err)
	result, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	assert.Error(t, err)
	assert.NotNil(t, result.Snapshot)
	assert.False(t, removed)
}

func TestMembershipRetainsLeaseAcrossStages(t *testing.T) {
	entered := make(chan struct{})
	resume := make(chan struct{})
	session := openDirectEtcdSession(t, directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("e", 64)}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error { close(entered); <-resume; return nil },
	}))
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionRemoveMember, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1", SnapshotPath: "/tmp/lease.db"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("membership RPC did not start")
	}
	other, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionDefragment, Node: "cp-2"})
	require.NoError(t, err)
	_, err = session.ExecuteEtcdAction(context.Background(), other, application.Confirmation{Approved: true})
	assert.ErrorIs(t, err, application.ErrMutationBusy)
	close(resume)
	require.NoError(t, <-done)
}

func TestForfeitUsesMemberNodeAndNeedsNoSnapshot(t *testing.T) {
	var target string
	var snapshots int
	session := openDirectEtcdSession(t, directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(context.Context, string, string) (domain.EtcdSnapshotResult, error) {
			snapshots++
			return domain.EtcdSnapshotResult{}, nil
		},
		ForfeitLeadershipFunc: func(_ context.Context, node string) error { target = node; return nil },
	}))
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionForfeitLeadership, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1"})
	require.NoError(t, err)
	_, err = session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	require.NoError(t, err)
	assert.Equal(t, "cp-1", target)
	assert.Zero(t, snapshots)
}

func TestLeaveAcceptsMemberIDOnlyAndUsesMemberNode(t *testing.T) {
	var leaveNode string
	var snapshotNode string
	session := openDirectEtcdSession(t, directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			snapshotNode = node
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("f", 64)}, nil
		},
		LeaveClusterFunc: func(_ context.Context, node string) error { leaveNode = node; return nil },
	}))
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionLeaveCluster, Node: "cp-1", MemberID: 1, SnapshotPath: "/tmp/leave.db"})
	require.NoError(t, err)
	result, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	require.NoError(t, err)
	assert.Equal(t, "cp-2", snapshotNode, "snapshot prefers a healthy voter other than the target")
	assert.Equal(t, "cp-1", leaveNode, "leave must target its member's own node")
	assert.Equal(t, domain.OperationAccepted, result.Targets[0].Outcome)
}

func TestEtcdMutationPreservesRawRPCErrorAndUncertainty(t *testing.T) {
	rpcErr := errors.New("transport reset")
	session := openDirectEtcdSession(t, directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		ForfeitLeadershipFunc: func(context.Context, string) error { return rpcErr },
	}))
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionForfeitLeadership, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1"})
	require.NoError(t, err)
	result, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	assert.ErrorIs(t, err, rpcErr)
	require.Len(t, result.Targets, 1)
	assert.Equal(t, domain.OperationUnconfirmed, result.Targets[0].Outcome)
	assert.ErrorIs(t, result.Targets[0].Err, rpcErr)
}

func TestEtcdLocalPathRemainsProcessLocal(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 13, 14, 0, time.UTC)
	path := application.DefaultEtcdSnapshotPath("prod/eu", "cp-1", now)
	assert.Equal(t, "etcd-prod-eu-cp-1-20261018T121314Z.db", path)
}

func TestRemoveOrphanMemberUsesDiscoveredHealthyGateway(t *testing.T) {
	for _, selectedNode := range []string{"cp-1", "cp-2"} {
		t.Run(selectedNode, func(t *testing.T) {
			var snapshotNode, removeNode string
			var removeID uint64
			adapter := directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
				SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
					snapshotNode = node
					return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("a", 64)}, nil
				},
				RemoveMemberByIDFunc: func(_ context.Context, node string, memberID uint64) error {
					removeNode, removeID = node, memberID
					return nil
				},
			})
			adapter.NodeReader = &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) { return nodesOnly("cp-2", "cp-3"), nil }}
			session := openDirectEtcdSession(t, adapter)
			plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionRemoveMember, Node: selectedNode, MemberID: 1, MemberHostname: "cp-1", SnapshotPath: "/tmp/orphan.db"})
			require.NoError(t, err)
			result, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
			require.NoError(t, err)
			assert.Equal(t, "cp-2", snapshotNode)
			assert.Equal(t, "cp-2", removeNode)
			assert.Equal(t, uint64(1), removeID)
			assert.Equal(t, "cp-1", result.Targets[0].Target, "the outcome names the etcd member, not its RPC gateway")
		})
	}
}

func TestRemoveDoesNotUseGatewayThatChangedAfterSnapshot(t *testing.T) {
	read := 0
	var removeCalls int
	var snapshotNode string
	adapter := directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			snapshotNode = node
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("b", 64)}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error { removeCalls++; return nil },
	})
	adapter.NodeReader = &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
		read++
		if read >= 3 { // post-snapshot read: the selected gateway disappeared
			return nodesOnly("cp-3"), nil
		}
		return nodesOnly("cp-2", "cp-3"), nil
	}}
	session := openDirectEtcdSession(t, adapter)
	plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionRemoveMember, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1", SnapshotPath: "/tmp/changed-gateway.db"})
	require.NoError(t, err)
	result, err := session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
	assert.Error(t, err)
	assert.Equal(t, "cp-2", snapshotNode)
	assert.NotNil(t, result.Snapshot)
	assert.Zero(t, removeCalls)
}

func TestEtcdSnapshotAndMaintenanceDeadlinesIncludeFreshReadBudget(t *testing.T) {
	t.Run("snapshot", func(t *testing.T) {
		var remaining time.Duration
		adapter := directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{SnapshotFunc: func(ctx context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			remaining = time.Until(deadline)
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 32, SHA256: strings.Repeat("c", 64)}, nil
		}})
		session := openDirectEtcdSession(t, adapter)
		plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionSnapshot, Node: "cp-1", SnapshotPath: "/tmp/deadline.db"})
		require.NoError(t, err)
		_, err = session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
		require.NoError(t, err)
		assert.Greater(t, remaining, 9*time.Minute+50*time.Second)
	})
	t.Run("defragment", func(t *testing.T) {
		var remaining time.Duration
		adapter := directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{DefragmentFunc: func(ctx context.Context, _ string) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			remaining = time.Until(deadline)
			return nil
		}})
		session := openDirectEtcdSession(t, adapter)
		plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionDefragment, Node: "cp-1"})
		require.NoError(t, err)
		_, err = session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
		require.NoError(t, err)
		assert.Greater(t, remaining, 9*time.Minute+50*time.Second)
	})
	t.Run("forfeit", func(t *testing.T) {
		var remaining time.Duration
		adapter := directEtcdAdapter(directEtcdMembers(), &testkit.FakeEtcdOperations{ForfeitLeadershipFunc: func(ctx context.Context, _ string) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			remaining = time.Until(deadline)
			return nil
		}})
		session := openDirectEtcdSession(t, adapter)
		plan, err := session.PrepareEtcdAction(context.Background(), application.EtcdActionRequest{Kind: application.EtcdActionForfeitLeadership, Node: "cp-1", MemberID: 1, MemberHostname: "cp-1"})
		require.NoError(t, err)
		_, err = session.ExecuteEtcdAction(context.Background(), plan, application.Confirmation{Approved: true})
		require.NoError(t, err)
		assert.Greater(t, remaining, 25*time.Second)
	})
}
