package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threeVoterEtcd is a Ready 3-voter cluster whose target cp-1 is dead
// (StatusKnown false) so a forced removal is permitted by the healthy-member
// guard and the removal leaves 2/3 voters, exactly at the quorum floor.
func threeVoterEtcd() application.EtcdState {
	return application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1"},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
	}}}
}

func TestRequestEtcdActionRemoveMemberBlocksWhenQuorumUnknown(t *testing.T) {
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", Etcd: application.EtcdState{Status: application.Loading}}
	model = withEtcdOperations(model, &testkit.FakeEtcdOperations{})

	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})

	require.NotNil(t, model.PendingEtcdAction)
	assert.Contains(t, model.PendingEtcdAction.Blocked, "unknown")

	model, effect := application.Update(model, application.ConfirmEtcdAction{})
	assert.Nil(t, effect, "a blocked removal must not build a mutation effect")
	require.NotNil(t, model.PendingEtcdAction, "a blocked removal stays pending until cancelled")
}

func TestRequestEtcdActionRemoveMemberRequiresWritesEnabled(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", Etcd: threeVoterEtcd()}

	model, effect := application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})

	assert.Nil(t, effect)
	assert.Nil(t, model.PendingEtcdAction)
}

func TestRequestEtcdActionRemoveMemberPicksSnapshotNodeAndPath(t *testing.T) {
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", Etcd: threeVoterEtcd()}
	model = withEtcdOperations(model, &testkit.FakeEtcdOperations{})

	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})

	require.NotNil(t, model.PendingEtcdAction)
	assert.Empty(t, model.PendingEtcdAction.Blocked)
	assert.Equal(t, application.EtcdStageIdle, model.PendingEtcdAction.Stage)
	assert.Contains(t, []string{"cp-2", "cp-3"}, model.PendingEtcdAction.SnapshotNode, "snapshot must be taken from a healthy voter, not the dead target")
	assert.Equal(t, model.PendingEtcdAction.SnapshotNode, model.PendingEtcdAction.Node, "the removal RPC is addressed to the live snapshot node")
	assert.True(t, strings.HasPrefix(model.PendingEtcdAction.SnapshotPath, "etcd-prod-cp-1-"), model.PendingEtcdAction.SnapshotPath)
}

func TestConfirmEtcdActionRemoveRunsSnapshotFirst(t *testing.T) {
	var removeNode string
	var removeID uint64
	removeCalled := false
	operations := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 544}, nil
		},
		RemoveMemberByIDFunc: func(_ context.Context, node string, memberID uint64) error {
			removeCalled = true
			removeNode, removeID = node, memberID
			return nil
		},
	}
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", Etcd: threeVoterEtcd()}
	model = withEtcdOperations(model, operations)

	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})
	require.NotNil(t, model.PendingEtcdAction)

	model, effect := application.Update(model, application.ConfirmEtcdAction{})
	require.NotNil(t, effect)
	require.NotNil(t, model.PendingEtcdAction, "the destructive action stays pending across the snapshot stage")
	assert.Equal(t, application.EtcdStageSnapshot, model.PendingEtcdAction.Stage)

	msg := effect(t.Context(), application.Dependencies{})
	succeeded, ok := msg.(application.EtcdSnapshotSucceeded)
	require.True(t, ok)
	assert.False(t, removeCalled, "a forced removal must not fire before its snapshot completes")

	model, effect = application.Update(model, succeeded)
	require.NotNil(t, effect)
	assert.Equal(t, application.EtcdStageOperation, model.PendingEtcdAction.Stage)

	effect(t.Context(), application.Dependencies{})
	assert.True(t, removeCalled, "the removal fires only after a successful snapshot")
	assert.Equal(t, uint64(1), removeID)
	assert.Equal(t, model.PendingEtcdAction.SnapshotNode, removeNode)
}

func TestEtcdSnapshotFailedAbortsMembershipAction(t *testing.T) {
	removeCalled := false
	operations := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error {
			removeCalled = true
			return nil
		},
	}
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", Etcd: threeVoterEtcd()}
	model = withEtcdOperations(model, operations)
	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})
	model, _ = application.Update(model, application.ConfirmEtcdAction{})
	require.Equal(t, application.EtcdStageSnapshot, model.PendingEtcdAction.Stage)

	model, effect := application.Update(model, application.EtcdSnapshotFailed{Generation: 1, Err: errors.New("disk full")})

	assert.Nil(t, effect, "a failed snapshot must not schedule the membership change")
	assert.Nil(t, model.PendingEtcdAction, "the aborted action must be cleared")
	require.Len(t, model.ActionResults, 1)
	assert.Contains(t, model.ActionResults[0].Err, "disk full")
	assert.False(t, removeCalled)
}

func TestConfirmEtcdActionLeaveUsesMemberNode(t *testing.T) {
	var leaveNode string
	leaveCalled := false
	operations := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path}, nil
		},
		LeaveClusterFunc: func(_ context.Context, node string) error {
			leaveCalled = true
			leaveNode = node
			return nil
		},
	}
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", Etcd: application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1", StatusKnown: true},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
	}}}}
	model = withEtcdOperations(model, operations)

	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionLeaveCluster, MemberID: 1, MemberHostname: "cp-1", Node: "cp-2"})
	require.NotNil(t, model.PendingEtcdAction)
	assert.Equal(t, "cp-1", model.PendingEtcdAction.Node, "leave must be addressed to the member's own node, not the RPC gateway")

	model, effect := application.Update(model, application.ConfirmEtcdAction{})
	require.NotNil(t, effect)
	succeeded := effect(t.Context(), application.Dependencies{}).(application.EtcdSnapshotSucceeded)

	model, effect = application.Update(model, succeeded)
	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})

	assert.True(t, leaveCalled)
	assert.Equal(t, "cp-1", leaveNode)
}

func TestConfirmEtcdActionRecomputesMembershipBlock(t *testing.T) {
	operations := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			t.Fatal("a re-blocked removal must not snapshot")
			return domain.EtcdSnapshotResult{}, nil
		},
	}
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", Etcd: threeVoterEtcd()}
	model = withEtcdOperations(model, operations)
	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})
	require.Empty(t, model.PendingEtcdAction.Blocked)

	// etcd degrades after the prompt opened; confirm must re-evaluate.
	model.Etcd = application.EtcdState{Status: application.Loading}

	model, effect := application.Update(model, application.ConfirmEtcdAction{})

	assert.Nil(t, effect)
	require.NotNil(t, model.PendingEtcdAction)
	assert.NotEmpty(t, model.PendingEtcdAction.Blocked)
}

func TestRequestEtcdActionRemoveLearnerStillRequiresSnapshot(t *testing.T) {
	operations := &testkit.FakeEtcdOperations{}
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", Etcd: application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1", StatusKnown: true},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
		{MemberID: 4, Hostname: "cp-4", IsLearner: true},
	}}}}
	model = withEtcdOperations(model, operations)

	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 4, MemberHostname: "cp-4", Node: "cp-4"})

	require.NotNil(t, model.PendingEtcdAction)
	assert.Empty(t, model.PendingEtcdAction.Blocked, "a learner does not vote, so its removal is not quorum-blocked")
	assert.NotEmpty(t, model.PendingEtcdAction.SnapshotPath, "an irreversible membership change still requires a snapshot")

	model, effect := application.Update(model, application.ConfirmEtcdAction{})
	require.NotNil(t, effect)
	assert.Equal(t, application.EtcdStageSnapshot, model.PendingEtcdAction.Stage)
}
