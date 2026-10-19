package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func directEtcdMutationSession(t *testing.T, writes bool, ops *testkit.FakeEtcdOperations) *application.Session {
	return directEtcdMutationSessionWithTargetHealth(t, writes, ops, false)
}

func directEtcdMutationSessionWithTargetHealth(t *testing.T, writes bool, ops *testkit.FakeEtcdOperations, targetHealthy bool) *application.Session {
	t.Helper()
	members := domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1", StatusKnown: targetHealthy, DBSize: 100},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true, DBSize: 100},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true, DBSize: 100},
	}}
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{
					{ID: "cp-1-id", Name: "cp-1", Role: domain.NodeRoleControl},
					{ID: "cp-2-id", Name: "cp-2", Role: domain.NodeRoleControl},
					{ID: "cp-3-id", Name: "cp-3", Role: domain.NodeRoleControl},
				}}, nil
			}},
			EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) { return members, nil }},
			EtcdOps:    ops,
		}, nil
	}}})
	session, err := client.Open(context.Background(), "prod", application.Options{WritesEnabled: writes})
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func directEtcdModel(session *application.Session, writes bool) directModel {
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: writes})
	model.splash, model.session, model.sessionCtx = false, session, context.Background()
	model.cluster.contextName = "prod"
	model.cluster.etcd = directEtcdState{Status: directReady, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1", StatusKnown: false},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: true},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
	}}}
	model.etcd = model.etcd.normalizeSelection(model.cluster.etcd.Value.Members, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	return model
}

func TestDirectEtcdActionKeysPrepareTypedPlansAndExecuteBySelectedMember(t *testing.T) {
	for _, test := range []struct {
		name string
		key  rune
		kind application.EtcdActionKind
	}{
		{"snapshot", 's', application.EtcdActionSnapshot},
		{"defragment", 'd', application.EtcdActionDefragment},
		{"disarm", 'A', application.EtcdActionDisarmAlarms},
		{"forfeit", 'F', application.EtcdActionForfeitLeadership},
		{"remove", 'R', application.EtcdActionRemoveMember},
		{"leave", 'L', application.EtcdActionLeaveCluster},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			ops := &testkit.FakeEtcdOperations{
				SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
					calls = append(calls, "snapshot:"+node)
					return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 1024, SHA256: strings.Repeat("a", 64)}, nil
				},
				DefragmentFunc:        func(_ context.Context, node string) error { calls = append(calls, "defrag:"+node); return nil },
				DisarmAlarmsFunc:      func(_ context.Context, node string) error { calls = append(calls, "disarm:"+node); return nil },
				ForfeitLeadershipFunc: func(_ context.Context, node string) error { calls = append(calls, "forfeit:"+node); return nil },
				RemoveMemberByIDFunc: func(_ context.Context, node string, id uint64) error {
					calls = append(calls, "remove:"+node+":"+string(rune('0'+id)))
					return nil
				},
				LeaveClusterFunc: func(_ context.Context, node string) error { calls = append(calls, "leave:"+node); return nil },
			}
			targetHealthy := test.kind != application.EtcdActionRemoveMember
			model := directEtcdModel(directEtcdMutationSessionWithTargetHealth(t, true, ops, targetHealthy), true)
			model, command := updateDirect(model, keyPress(test.key))
			if test.kind == application.EtcdActionSnapshot || test.kind == application.EtcdActionRemoveMember || test.kind == application.EtcdActionLeaveCluster {
				require.Nil(t, command)
				require.NotNil(t, model.etcdPathPrompt)
				assert.Equal(t, test.kind, model.etcdPathPrompt.kind)
				model.etcdPathPrompt.input.SetValue("/tmp/direct-etcd.db")
				model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
			}
			require.NotNil(t, command)
			model, _ = applyDirectCommand(t, model, command)
			require.NotNil(t, model.mutation.prompt)
			assert.Equal(t, test.kind, model.mutation.prompt.etcdKind)
			if test.kind == application.EtcdActionSnapshot || test.kind == application.EtcdActionRemoveMember || test.kind == application.EtcdActionLeaveCluster {
				assert.Contains(t, renderDirectMutationPrompt(model.mutation.prompt), "/tmp/direct-etcd.db")
			}
			if test.kind == application.EtcdActionRemoveMember || test.kind == application.EtcdActionLeaveCluster || test.kind == application.EtcdActionForfeitLeadership {
				assert.Equal(t, []string{"cp-1"}, model.mutation.prompt.assessment.Targets)
			}
			if model.mutation.prompt.assessment.Blocked != "" {
				model, command = updateDirect(model, keyPress('y'))
				assert.Nil(t, command)
				assert.NotNil(t, model.mutation.prompt)
				model, _ = updateDirect(model, keyPress('n'))
				assert.Nil(t, model.mutation.prompt)
				assert.Empty(t, calls)
				return
			}
			model, command = updateDirect(model, keyPress('y'))
			require.NotNil(t, command)
			model, _ = applyDirectCommand(t, model, command)
			assert.Nil(t, model.mutation.prompt)
			if test.kind == application.EtcdActionSnapshot {
				assert.Contains(t, model.mutation.notice, "completed")
				assert.Contains(t, model.mutation.notice, "snapshot completed: /tmp/direct-etcd.db")
			} else {
				assert.Contains(t, model.mutation.notice, "accepted")
				if test.kind == application.EtcdActionRemoveMember || test.kind == application.EtcdActionLeaveCluster {
					assert.Contains(t, model.mutation.notice, "snapshot completed: /tmp/direct-etcd.db")
				}
			}
			switch test.kind {
			case application.EtcdActionSnapshot:
				assert.Equal(t, []string{"snapshot:cp-1"}, calls)
			case application.EtcdActionDefragment:
				assert.Equal(t, []string{"defrag:cp-1"}, calls)
			case application.EtcdActionDisarmAlarms:
				assert.Equal(t, []string{"disarm:cp-1"}, calls)
			case application.EtcdActionForfeitLeadership:
				assert.Equal(t, []string{"forfeit:cp-1"}, calls)
			case application.EtcdActionRemoveMember:
				assert.Equal(t, []string{"snapshot:cp-2", "remove:cp-2:1"}, calls)
			case application.EtcdActionLeaveCluster:
				assert.Equal(t, []string{"snapshot:cp-2", "leave:cp-1"}, calls)
			}
		})
	}
}

func TestDirectEtcdSnapshotPathRejectsInvalidInputWithoutCallingRPC(t *testing.T) {
	var calls int
	ops := &testkit.FakeEtcdOperations{SnapshotFunc: func(context.Context, string, string) (domain.EtcdSnapshotResult, error) {
		calls++
		return domain.EtcdSnapshotResult{}, nil
	}}
	model := directEtcdModel(directEtcdMutationSession(t, true, ops), true)
	model, _ = updateDirect(model, keyPress('s'))
	require.NotNil(t, model.etcdPathPrompt)
	model.etcdPathPrompt.input.SetValue("..")
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, command)
	assert.NotNil(t, model.etcdPathPrompt)
	assert.Contains(t, model.etcdPathPrompt.view(), "invalid snapshot path")
	assert.Zero(t, calls)
}

func TestDirectEtcdPathPromptKeepsSelectedMemberIdentityAndPreparedResultIsStamped(t *testing.T) {
	model := directEtcdModel(directEtcdMutationSession(t, true, &testkit.FakeEtcdOperations{}), true)
	model, _ = updateDirect(model, keyPress('s'))
	require.NotNil(t, model.etcdPathPrompt)
	assert.Equal(t, uint64(1), model.etcdPathPrompt.memberID)
	model.etcd.selectedKey = etcdMemberKey(model.cluster.etcd.Value.Members[1])
	model.etcd = model.etcd.normalizeSelection(model.cluster.etcd.Value.Members, 1)
	model.etcdPathPrompt.input.SetValue("/tmp/stamped.db")
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	prepared := command().(directEtcdPrepared)
	assert.Equal(t, application.EtcdActionSnapshot, prepared.request.Kind)
	assert.Equal(t, "cp-1", prepared.request.Node)
	assert.Equal(t, uint64(1), prepared.request.MemberID)
	assert.Equal(t, "/tmp/stamped.db", prepared.request.SnapshotPath)
	assert.True(t, model.mutation.busy)

	model.mutation.prompt = nil
	model.mutation.operation = prepared.operation + 1
	updated, _ := model.Update(prepared)
	assert.Nil(t, updated.(directModel).mutation.prompt)
	assert.True(t, updated.(directModel).mutation.busy, "late prep for an older operation must not clear the active busy state")
}

func TestDirectEtcdActionPreparationHonorsSessionReadOnlyAndBlockedPlan(t *testing.T) {
	model := directEtcdModel(directEtcdMutationSession(t, false, &testkit.FakeEtcdOperations{}), true)
	model, command := updateDirect(model, keyPress('F'))
	require.NotNil(t, command)
	model, _ = applyDirectCommand(t, model, command)
	assert.Nil(t, model.mutation.prompt)
	assert.Equal(t, "writes are disabled for this session", model.view.notice)

	blockedOps := &testkit.FakeEtcdOperations{}
	blockedSession := directEtcdMutationSessionWithTargetHealth(t, true, blockedOps, true)
	blocked := directEtcdModel(blockedSession, true)
	blocked, command = updateDirect(blocked, keyPress('R'))
	require.Nil(t, command)
	require.NotNil(t, blocked.etcdPathPrompt)
	blocked.etcdPathPrompt.input.SetValue("/tmp/blocked.db")
	blocked, command = updateDirect(blocked, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, command)
	blocked, _ = applyDirectCommand(t, blocked, command)
	require.NotNil(t, blocked.mutation.prompt)
	require.NotEmpty(t, blocked.mutation.prompt.assessment.Blocked)
	blocked, command = updateDirect(blocked, keyPress('y'))
	assert.Nil(t, command)
	assert.Contains(t, blocked.view.notice, "refused")
	blocked, _ = updateDirect(blocked, keyPress('n'))
	assert.Nil(t, blocked.mutation.prompt)
}

func TestDirectMembershipFailureRetainsSuccessfulSnapshotMetadata(t *testing.T) {
	ops := &testkit.FakeEtcdOperations{
		SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
			return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 1024, SHA256: strings.Repeat("b", 64)}, nil
		},
		RemoveMemberByIDFunc: func(context.Context, string, uint64) error { return errors.New("secret transport detail") },
	}
	model := directEtcdModel(directEtcdMutationSession(t, true, ops), true)
	model, _ = updateDirect(model, keyPress('R'))
	model.etcdPathPrompt.input.SetValue("/tmp/retained.db")
	model, command := updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model, _ = applyDirectCommand(t, model, command)
	model, command = updateDirect(model, keyPress('y'))
	model, _ = applyDirectCommand(t, model, command)
	assert.NotNil(t, model.mutation.snapshot)
	assert.Equal(t, "/tmp/retained.db", model.mutation.snapshot.Path)
	assert.Contains(t, model.mutation.notice, "snapshot completed: /tmp/retained.db")
	assert.Contains(t, model.mutation.notice, "unconfirmed")
	assert.NotContains(t, model.mutation.notice, "secret transport detail")
}
