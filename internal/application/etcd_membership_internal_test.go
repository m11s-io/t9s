package application

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
)

func readyEtcd(members ...domain.EtcdMemberSnapshot) EtcdState {
	return EtcdState{Status: Ready, Value: domain.EtcdSet{Members: members}}
}

func voter(id uint64, hostname string) domain.EtcdMemberSnapshot {
	return domain.EtcdMemberSnapshot{MemberID: id, Hostname: hostname, StatusKnown: true}
}

func TestAssessEtcdMembershipFindsVoterAndLearnerByIDAndHostname(t *testing.T) {
	etcd := readyEtcd(voter(1, "cp-1"), domain.EtcdMemberSnapshot{MemberID: 2, Hostname: "cp-2", IsLearner: true, StatusKnown: true})

	byHostname := assessEtcdMembership(etcd, 0, "cp-1")
	assert.True(t, byHostname.found)
	assert.True(t, byHostname.voter)

	byID := assessEtcdMembership(etcd, 2, "")
	assert.True(t, byID.found)
	assert.False(t, byID.voter, "a learner must not be counted as a voter")
}

func TestEtcdMembershipBlockReasonRefusesWhenEtcdUnavailable(t *testing.T) {
	block := etcdMembershipBlockReason(EtcdState{Status: Loading}, EtcdActionRemoveMember, 1, "cp-1")

	assert.Contains(t, block, "unknown")
	// Proof the existing quorum advisory alone would not refuse: it returns ""
	// for an unknown assessment, which is fine for a reboot but not a removal.
	assert.Empty(t, etcdQuorumBlockReason(EtcdState{Status: Loading}, []string{"cp-1"}))
}

func TestEtcdMembershipBlockReasonRefusesAbsentMember(t *testing.T) {
	etcd := readyEtcd(voter(1, "cp-1"))

	block := etcdMembershipBlockReason(etcd, EtcdActionRemoveMember, 999, "cp-999")

	assert.Contains(t, block, "not in the current etcd snapshot")
}

func TestEtcdMembershipBlockReasonAllowsRemovingLearner(t *testing.T) {
	etcd := readyEtcd(
		voter(1, "cp-1"),
		voter(2, "cp-2"),
		voter(3, "cp-3"),
		domain.EtcdMemberSnapshot{MemberID: 4, Hostname: "cp-4", IsLearner: true},
	)

	block := etcdMembershipBlockReason(etcd, EtcdActionRemoveMember, 4, "cp-4")

	assert.Empty(t, block, "removing a learner is quorum-neutral and must not be blocked")
}

func TestEtcdMembershipBlockReasonBlocksLeaveThatLosesQuorum(t *testing.T) {
	etcd := readyEtcd(voter(1, "cp-1"))

	block := etcdMembershipBlockReason(etcd, EtcdActionLeaveCluster, 1, "cp-1")

	assert.Contains(t, block, "below quorum")
	assert.Contains(t, block, "refusing")
}

func TestEtcdMembershipBlockReasonRefusesForcedRemoveOfHealthyMember(t *testing.T) {
	etcd := readyEtcd(voter(1, "cp-1"), voter(2, "cp-2"), voter(3, "cp-3"))

	block := etcdMembershipBlockReason(etcd, EtcdActionRemoveMember, 2, "cp-2")

	assert.Contains(t, block, "use leave")
}

func TestEtcdMembershipBlockReasonRefusesWhenAnotherVoterHealthUnknown(t *testing.T) {
	// cp-1 (target) and cp-2 are both unreachable; removing cp-1 could leave a
	// single reachable voter. Irreversible membership surgery must treat an
	// unknown voter as at-risk, even though the best case keeps quorum.
	etcd := readyEtcd(
		domain.EtcdMemberSnapshot{MemberID: 1, Hostname: "cp-1", StatusKnown: false},
		domain.EtcdMemberSnapshot{MemberID: 2, Hostname: "cp-2", StatusKnown: false},
		voter(3, "cp-3"),
	)

	block := etcdMembershipBlockReason(etcd, EtcdActionRemoveMember, 1, "cp-1")

	assert.Contains(t, block, "below quorum", "a removal with an unknown peer must be refused, not merely warned")
}

func TestEtcdMembershipBlockReasonMatchesByIDWhenHostnameIsStale(t *testing.T) {
	// The caller's hostname does not match the member, but the ID does; the
	// matched member's hostname must be used so the target is actually counted
	// at-risk and the quorum-losing leave is refused.
	etcd := readyEtcd(voter(1, "cp-1"), voter(2, "cp-2"))

	block := etcdMembershipBlockReason(etcd, EtcdActionLeaveCluster, 1, "stale-name")

	assert.Contains(t, block, "below quorum")
}

func TestEtcdMembershipWarningForZeroFaultToleranceLeave(t *testing.T) {
	etcd := readyEtcd(voter(1, "cp-1"), voter(2, "cp-2"), voter(3, "cp-3"))

	warning := etcdMembershipWarning(etcd, EtcdActionLeaveCluster, 1, "cp-1")

	assert.Contains(t, warning, "would drop etcd to 2/3")
	assert.Empty(t, etcdMembershipBlockReason(etcd, EtcdActionLeaveCluster, 1, "cp-1"), "zero-fault-tolerance leave is a warning, not a block")
}

func TestEtcdMembershipWarningEmptyForLearnerRemoval(t *testing.T) {
	etcd := readyEtcd(voter(1, "cp-1"), domain.EtcdMemberSnapshot{MemberID: 4, Hostname: "cp-4", IsLearner: true})

	assert.Empty(t, etcdMembershipWarning(etcd, EtcdActionRemoveMember, 4, "cp-4"))
}

func TestEtcdQuorumBlockReasonStillAdvisoryForUnknownOnReboot(t *testing.T) {
	// The membership hardening must not change reboot/shutdown semantics: an
	// unknown assessment stays a warning-only ("") hard-gate result.
	assert.Empty(t, etcdQuorumBlockReason(EtcdState{Status: Loading}, []string{"cp-1"}))
	assert.NotEmpty(t, computeEtcdQuorumWarning(EtcdState{Status: Loading}, []string{"cp-1"}))
}
