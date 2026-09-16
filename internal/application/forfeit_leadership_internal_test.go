package application

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestEtcdLeadershipWarningSilentWithHealthyFollower(t *testing.T) {
	etcd := EtcdState{Status: Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", MemberID: 1, StatusKnown: true, IsLeader: true},
		{Hostname: "cp-2", MemberID: 2, StatusKnown: true},
	}}}

	assert.Empty(t, etcdLeadershipWarning(etcd, "cp-1"), "a healthy follower can take over, so no warning")

	// A non-leader target has nothing to forfeit; never warn about it.
	assert.Empty(t, etcdLeadershipWarning(etcd, "cp-2"))

	// A leader whose follower status is unknown warns.
	degraded := EtcdState{Status: Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", MemberID: 1, StatusKnown: true, IsLeader: true},
		{Hostname: "cp-2", MemberID: 2},
	}}}
	assert.Contains(t, etcdLeadershipWarning(degraded, "cp-1"), "no healthy follower")

	// A learner cannot take leadership, so it never counts as a healthy follower.
	learner := EtcdState{Status: Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", MemberID: 1, StatusKnown: true, IsLeader: true},
		{Hostname: "cp-2", MemberID: 2, StatusKnown: true, IsLearner: true},
	}}}
	assert.Contains(t, etcdLeadershipWarning(learner, "cp-1"), "no healthy follower")

	// Unknown etcd state produces no advisory (the action is still allowed).
	assert.Empty(t, etcdLeadershipWarning(EtcdState{Status: Loading}, "cp-1"))
}
