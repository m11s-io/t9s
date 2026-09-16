package application_test

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func forfeitTestModel(etcd application.EtcdState) application.Model {
	model, _ := application.NewModel("prod")
	model.WritesEnabled = true
	model.Etcd = etcd
	return model
}

func TestRequestEtcdActionForfeitLeadershipWarnsWhenNoHealthyFollower(t *testing.T) {
	model := forfeitTestModel(application.EtcdState{Status: application.Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", MemberID: 1, StatusKnown: true, IsLeader: true},
		{Hostname: "cp-2", MemberID: 2, StatusKnown: true, Errors: []string{"unreachable"}},
		{Hostname: "cp-3", MemberID: 3, StatusKnown: false},
	}}})

	model, effect := application.Update(model, application.RequestEtcdAction{
		Kind:           application.EtcdActionForfeitLeadership,
		MemberID:       1,
		MemberHostname: "cp-1",
		Node:           "cp-1",
	})

	assert.Nil(t, effect)
	require.NotNil(t, model.PendingEtcdAction)
	assert.Equal(t, application.EtcdActionForfeitLeadership, model.PendingEtcdAction.Kind)
	assert.Contains(t, model.PendingEtcdAction.Warning, "no healthy follower")
	assert.Empty(t, model.PendingEtcdAction.Blocked, "leadership transfer is quorum-neutral and never hard-blocked")
	assert.Empty(t, model.PendingEtcdAction.SnapshotPath, "forfeit must not snapshot")
}

func TestConfirmEtcdActionForfeitLeadershipCallsForfeit(t *testing.T) {
	var forfeited []string
	operations := &testkit.FakeEtcdOperations{
		ForfeitLeadershipFunc: func(_ context.Context, node string) error {
			forfeited = append(forfeited, node)
			return nil
		},
		SnapshotFunc: func(context.Context, string, string) (domain.EtcdSnapshotResult, error) {
			t.Fatal("forfeit leadership must not run a snapshot")
			return domain.EtcdSnapshotResult{}, nil
		},
	}
	model := application.Model{Generation: 1, WritesEnabled: true}
	model, _ = application.Update(model, application.SessionOpened{Generation: model.Generation, EtcdOperations: operations})
	model, _ = application.Update(model, application.RequestEtcdAction{Kind: application.EtcdActionForfeitLeadership, MemberID: 1, MemberHostname: "cp-1", Node: "cp-1"})
	require.NotNil(t, model.PendingEtcdAction)

	model, effect := application.Update(model, application.ConfirmEtcdAction{})

	require.NotNil(t, effect)
	msg := effect(t.Context(), application.Dependencies{})
	_, ok := msg.(application.EtcdActionSucceeded)
	require.True(t, ok)
	assert.Equal(t, []string{"cp-1"}, forfeited)
	assert.Nil(t, model.PendingEtcdAction, "confirming must clear the pending action")
}
