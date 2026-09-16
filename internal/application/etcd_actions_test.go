package application_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withEtcdOperations seeds Model.etcdOperations via the SessionOpened message,
// since this test file is package application_test and cannot set unexported
// fields directly (the same technique withEtcdReader uses for etcdReader).
func withEtcdOperations(model application.Model, operations ports.EtcdOperations) application.Model {
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, EtcdOperations: operations})
	return model
}

func TestRequestEtcdSnapshotPromptRequiresWritesEnabled(t *testing.T) {
	model := application.Model{Generation: 1}

	model, effect := application.Update(model, application.RequestEtcdSnapshotPrompt{Node: "cp-1", MemberHostname: "cp-1"})

	assert.Nil(t, effect)
	assert.Equal(t, application.EtcdSnapshotState{}, model.EtcdSnapshot)
}

func TestEtcdSnapshotPromptOpenedCarriesDefaultPath(t *testing.T) {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true

	model, effect := application.Update(model, application.RequestEtcdSnapshotPrompt{Node: "cp-1", MemberHostname: "cp-1"})

	require.NotNil(t, effect)
	opened, ok := effect(t.Context(), application.Dependencies{}).(application.EtcdSnapshotPromptOpened)
	require.True(t, ok)
	assert.Equal(t, "cp-1", opened.Node)
	assert.Equal(t, "cp-1", opened.MemberHostname)
	assert.True(t, strings.HasPrefix(opened.DefaultPath, "etcd-prod-cp-1-"), opened.DefaultPath)
	assert.True(t, strings.HasSuffix(opened.DefaultPath, "Z.db"), opened.DefaultPath)
	assert.Equal(t, application.Loading, model.EtcdSnapshot.Status)
}

func TestConfirmEtcdSnapshotPromptRejectsInvalidPathWithoutCallingOps(t *testing.T) {
	called := false
	operations := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			called = true
			return domain.EtcdSnapshotResult{}, nil
		},
	}
	model := application.Model{Generation: 1}
	model = withEtcdOperations(model, operations)

	model, effect := application.Update(model, application.ConfirmEtcdSnapshotPrompt{Node: "cp-1", Path: ".."})

	assert.Nil(t, effect)
	assert.Equal(t, application.Failed, model.EtcdSnapshot.Status)
	assert.NotEmpty(t, model.EtcdSnapshot.Err)
	assert.False(t, called, "an invalid path must never reach the adapter")
}

func TestConfirmEtcdSnapshotPromptRunsSnapshot(t *testing.T) {
	var gotNode, gotPath string
	operations := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			gotNode, gotPath = node, path
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 544, SHA256: "deadbeef"}, nil
		},
	}
	model := application.Model{Generation: 1}
	model = withEtcdOperations(model, operations)

	model, effect := application.Update(model, application.ConfirmEtcdSnapshotPrompt{Node: "cp-1", Path: "/tmp/snap.db"})

	require.NotNil(t, effect)
	msg := effect(t.Context(), application.Dependencies{})
	succeeded, ok := msg.(application.EtcdSnapshotSucceeded)
	require.True(t, ok)
	assert.Equal(t, "cp-1", gotNode)
	assert.Equal(t, "/tmp/snap.db", gotPath)
	assert.Equal(t, int64(544), succeeded.Result.Size)
}

func TestConfirmEtcdSnapshotPromptWithoutOpsFails(t *testing.T) {
	model := application.Model{Generation: 1}

	_, effect := application.Update(model, application.ConfirmEtcdSnapshotPrompt{Node: "cp-1", Path: "/tmp/snap.db"})

	require.NotNil(t, effect)
	msg := effect(t.Context(), application.Dependencies{})
	failed, ok := msg.(application.EtcdSnapshotFailed)
	require.True(t, ok)
	assert.Error(t, failed.Err)
}

func TestEtcdSnapshotSucceededRecordsResult(t *testing.T) {
	result := domain.EtcdSnapshotResult{Node: "cp-1", Path: "/tmp/x.db", Size: 544, SHA256: "ab"}
	model := application.Model{Generation: 1, EtcdSnapshot: application.EtcdSnapshotState{Status: application.Loading, MemberNode: "cp-1"}}

	model, effect := application.Update(model, application.EtcdSnapshotSucceeded{Generation: 1, Result: result})

	assert.Nil(t, effect)
	assert.Equal(t, application.Ready, model.EtcdSnapshot.Status)
	assert.Equal(t, result, model.EtcdSnapshot.Result)
	require.Len(t, model.ActionResults, 1)
}

func TestCancelEtcdSnapshotPromptResetsState(t *testing.T) {
	model := application.Model{Generation: 1, EtcdSnapshot: application.EtcdSnapshotState{Status: application.Loading, MemberNode: "cp-1"}}

	model, effect := application.Update(model, application.CancelEtcdSnapshotPrompt{})

	assert.Nil(t, effect)
	assert.Equal(t, application.EtcdSnapshotState{}, model.EtcdSnapshot, "escaping the path prompt must not leave a phantom in-flight snapshot")
}

func TestRequestEtcdSnapshotPromptClearsStaleActionResults(t *testing.T) {
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", ActionTotal: 3, ActionResults: []application.ActionResult{{Target: "old"}}}

	model, _ = application.Update(model, application.RequestEtcdSnapshotPrompt{Node: "cp-1", MemberHostname: "cp-1"})

	assert.Empty(t, model.ActionResults, "a new snapshot must not inherit a stale results denominator")
	assert.Equal(t, 0, model.ActionTotal)
}

func TestEtcdSnapshotFailedRecordsError(t *testing.T) {
	model := application.Model{Generation: 1, EtcdSnapshot: application.EtcdSnapshotState{Status: application.Loading, MemberNode: "cp-1"}}

	model, _ = application.Update(model, application.EtcdSnapshotFailed{Generation: 1, Err: errors.New("disk full")})

	assert.Equal(t, application.Failed, model.EtcdSnapshot.Status)
	assert.Contains(t, model.EtcdSnapshot.Err, "disk full")
	require.Len(t, model.ActionResults, 1)
}

func TestConfirmEtcdActionRefusesBlockedAction(t *testing.T) {
	model := application.Model{Generation: 1, WritesEnabled: true, PendingEtcdAction: &application.PendingEtcdAction{
		Kind:    application.EtcdActionDefragment,
		Node:    "cp-1",
		Blocked: "refusing: example",
	}}

	model, effect := application.Update(model, application.ConfirmEtcdAction{})

	assert.Nil(t, effect)
	require.NotNil(t, model.PendingEtcdAction, "a blocked etcd action must not be consumed by confirm")
}

func TestRequestEtcdActionClearsStaleSnapshotState(t *testing.T) {
	model := application.Model{Generation: 1, WritesEnabled: true, ContextName: "prod", EtcdSnapshot: application.EtcdSnapshotState{Status: application.Ready, Result: domain.EtcdSnapshotResult{Path: "/old.db"}}}

	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionDefragment, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})

	assert.Equal(t, application.EtcdSnapshotState{}, model.EtcdSnapshot, "a pending membership action must not let a stale snapshot notice mask its outcome")
}

func TestEtcdSnapshotSucceededIgnoresUnrelatedSnapshotDuringMembership(t *testing.T) {
	model := application.Model{
		Generation: 1,
		PendingEtcdAction: &application.PendingEtcdAction{
			Kind: application.EtcdActionRemoveMember, Stage: application.EtcdStageSnapshot,
			MemberID: 1, MemberHostname: "cp-1", SnapshotNode: "cp-2", SnapshotPath: "/tmp/snap.db",
		},
	}

	model, effect := application.Update(model, application.EtcdSnapshotSucceeded{Generation: 1, Result: domain.EtcdSnapshotResult{Node: "cp-9", Path: "/other.db"}})

	assert.Nil(t, effect, "a snapshot from a different source must not satisfy the destructive stage")
	require.NotNil(t, model.PendingEtcdAction)
	assert.Equal(t, application.EtcdStageSnapshot, model.PendingEtcdAction.Stage)
}

func TestRequestEtcdActionRemoveFallsBackToLiveControlPlaneNode(t *testing.T) {
	model := application.Model{
		Generation:    1,
		WritesEnabled: true,
		ContextName:   "prod",
		Nodes: application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{Name: "cp-1", Role: domain.NodeRoleControl},
			{Name: "cp-2", Role: domain.NodeRoleControl},
		}}},
		Etcd: application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{MemberID: 1, Hostname: "cp-1", StatusKnown: false},
		}}},
	}

	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionRemoveMember, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})

	require.NotNil(t, model.PendingEtcdAction)
	assert.Equal(t, "cp-2", model.PendingEtcdAction.Node, "force-remove must be addressed to a live node, not the dead target")
	assert.Contains(t, model.PendingEtcdAction.Warning, "snapshot source is the target")
}

func TestConfirmEtcdActionRefreshesWarning(t *testing.T) {
	model := application.Model{
		Generation:    1,
		WritesEnabled: true,
		Etcd: application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, StatusKnown: true},
			{Hostname: "cp-2", MemberID: 2, StatusKnown: true},
			{Hostname: "cp-3", MemberID: 3, StatusKnown: true},
		}}},
		PendingEtcdAction: &application.PendingEtcdAction{
			Kind: application.EtcdActionLeaveCluster, Stage: application.EtcdStageIdle,
			MemberID: 1, MemberHostname: "cp-1", Node: "cp-1", SnapshotNode: "cp-1", SnapshotPath: "/tmp/snap.db",
		},
	}

	model, _ = application.Update(model, application.ConfirmEtcdAction{})

	require.NotNil(t, model.PendingEtcdAction)
	assert.Contains(t, model.PendingEtcdAction.Warning, "would drop etcd to 2/3")
}

func TestRequestEtcdActionRequiresWritesEnabled(t *testing.T) {
	model := application.Model{Generation: 1}

	model, effect := application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionDefragment, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})

	assert.Nil(t, effect)
	assert.Nil(t, model.PendingEtcdAction)
}

func TestRequestEtcdActionOpensPendingMaintenance(t *testing.T) {
	model := application.Model{Generation: 1, WritesEnabled: true}

	model, effect := application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionDisarmAlarms, MemberID: 7, MemberHostname: "cp-2", Node: "cp-2"})

	assert.Nil(t, effect)
	require.NotNil(t, model.PendingEtcdAction)
	assert.Equal(t, application.EtcdActionDisarmAlarms, model.PendingEtcdAction.Kind)
	assert.Equal(t, "cp-2", model.PendingEtcdAction.Node)
}

func TestConfirmEtcdActionDefragmentRunsMaintenanceWithoutSnapshot(t *testing.T) {
	var defragged []string
	operations := &testkit.FakeEtcdOperations{
		DefragmentFunc: func(_ context.Context, node string) error {
			defragged = append(defragged, node)
			return nil
		},
		SnapshotFunc: func(context.Context, string, string) (domain.EtcdSnapshotResult, error) {
			t.Fatal("defragment must not run a snapshot")
			return domain.EtcdSnapshotResult{}, nil
		},
	}
	model := application.Model{Generation: 1, WritesEnabled: true}
	model = withEtcdOperations(model, operations)
	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionDefragment, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})
	require.NotNil(t, model.PendingEtcdAction)

	model, effect := application.Update(model, application.ConfirmEtcdAction{})

	require.NotNil(t, effect)
	msg := effect(t.Context(), application.Dependencies{})
	_, ok := msg.(application.EtcdActionSucceeded)
	require.True(t, ok)
	assert.Equal(t, []string{"cp-1"}, defragged)
	assert.Nil(t, model.PendingEtcdAction, "confirming must clear the pending action")
}

func TestConfirmEtcdActionDisarmAlarmsRunsMaintenance(t *testing.T) {
	var disarmed []string
	operations := &testkit.FakeEtcdOperations{
		DisarmAlarmsFunc: func(_ context.Context, node string) error {
			disarmed = append(disarmed, node)
			return nil
		},
	}
	model := application.Model{Generation: 1, WritesEnabled: true}
	model = withEtcdOperations(model, operations)
	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionDisarmAlarms, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})

	_, effect := application.Update(model, application.ConfirmEtcdAction{})

	require.NotNil(t, effect)
	effect(t.Context(), application.Dependencies{})
	assert.Equal(t, []string{"cp-1"}, disarmed)
}

func TestEtcdActionFailedRecordsErrorAndClearsPending(t *testing.T) {
	model := application.Model{Generation: 1, PendingEtcdAction: &application.PendingEtcdAction{Kind: application.EtcdActionDefragment, Node: "cp-1"}}

	model, effect := application.Update(model, application.EtcdActionFailed{Generation: 1, MemberHostname: "cp-1", Err: errors.New("boom")})

	assert.Nil(t, effect)
	assert.Nil(t, model.PendingEtcdAction)
	require.Len(t, model.ActionResults, 1)
	assert.Contains(t, model.ActionResults[0].Err, "boom")
}

func TestEtcdActionSucceededRefreshesEtcd(t *testing.T) {
	model := application.Model{Generation: 1, PendingEtcdAction: &application.PendingEtcdAction{Kind: application.EtcdActionDefragment, Node: "cp-1"}}
	model = withNodesAndEtcdReader(model, domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleControl}}}, &testkit.FakeEtcdReader{
		ListFunc: func(context.Context, []string) (domain.EtcdSet, error) { return domain.EtcdSet{}, nil },
	})

	model, effect := application.Update(model, application.EtcdActionSucceeded{Generation: 1, MemberHostname: "cp-1"})

	assert.Nil(t, model.PendingEtcdAction)
	require.Len(t, model.ActionResults, 1)
	require.NotNil(t, effect, "a successful membership/maintenance change must refresh the etcd snapshot")
}

func TestSelectContextClearsPendingEtcdAction(t *testing.T) {
	model := application.Model{Generation: 1, ContextName: "prod", PendingEtcdAction: &application.PendingEtcdAction{Kind: application.EtcdActionDefragment}}

	model, _ = application.Update(model, application.SelectContext{Name: "dev"})

	assert.Nil(t, model.PendingEtcdAction)
}

func TestCancelPendingActionClearsPendingEtcdAction(t *testing.T) {
	model := application.Model{Generation: 1, PendingEtcdAction: &application.PendingEtcdAction{Kind: application.EtcdActionDefragment}}

	model, _ = application.Update(model, application.CancelPendingAction{})

	assert.Nil(t, model.PendingEtcdAction)
}

func TestEtcdSnapshotResultFieldsAreOnlyNonSensitive(t *testing.T) {
	// Positive structural guard: the result type must carry only non-sensitive
	// metadata. A string-contains assertion cannot fail, so pin the exact field
	// set instead; adding a field re-introduces a credential-leak risk and must
	// be an explicit decision.
	typ := reflect.TypeOf(domain.EtcdSnapshotResult{})
	fields := make([]string, 0, typ.NumField())
	for index := 0; index < typ.NumField(); index++ {
		fields = append(fields, typ.Field(index).Name)
	}

	assert.Equal(t, []string{"Node", "Path", "Size", "SHA256"}, fields)
}
