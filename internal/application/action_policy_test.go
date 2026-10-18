package application

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

func TestUnknownNodeCannotBecomeWorker(t *testing.T) {
	_, ok := resolveNodeTarget([]domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleControl}}, "missing")
	if ok {
		t.Fatal("unresolved target was accepted as a node")
	}
	if _, ok := resolveNodeTarget([]domain.NodeSnapshot{{ID: "n-1", Name: "worker-1", Role: domain.NodeRoleWorker}}, "worker-1"); !ok {
		t.Fatal("resolved worker identity should be accepted")
	}
}

func TestNodeTargetRejectsNonUniqueCanonicalTarget(t *testing.T) {
	nodes := []domain.NodeSnapshot{
		{ID: "control-id", Name: "shared", Role: domain.NodeRoleControl},
		{ID: "worker-id", Name: "shared", Role: domain.NodeRoleWorker},
	}
	if _, ok := resolveNodeTarget(nodes, "worker-id"); ok {
		t.Fatal("a unique alias must not resolve to a target shared with another node")
	}
}

func TestConflictingMemberIdentityIsRejected(t *testing.T) {
	members := []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1"},
		{MemberID: 2, Hostname: "cp-2"},
	}
	if _, ok := resolveEtcdMember(members, 1, "cp-2"); ok {
		t.Fatal("ID and hostname identifying different members must be rejected")
	}
	if _, ok := resolveEtcdMember(members, 1, "cp-1"); !ok {
		t.Fatal("matching ID and hostname should resolve")
	}
}

func TestActionPolicyEtcdQuorumPreservesUnknownAndLearnerRules(t *testing.T) {
	facts := EtcdPolicyFacts{Available: true, Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1", StatusKnown: true},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: false},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
		{MemberID: 4, Hostname: "learner", IsLearner: true, StatusKnown: false},
	}}
	assessment := assessEtcdQuorumFacts(facts, []string{"cp-1"})
	if !assessment.known || assessment.voters != 3 || assessment.remaining != 1 || assessment.confirmedRemaining != 2 || assessment.floor != 2 {
		t.Fatalf("unexpected quorum assessment: %+v", assessment)
	}
}

func TestActionPolicyDiskIdentityAndAvailability(t *testing.T) {
	disk := domain.DiskSnapshot{DeviceName: "/dev/sdb", Model: "fast-disk", Serial: "serial-a", SizeBytes: 100, Type: "ssd", BusPath: "pci-1"}
	facts := DiskPolicyFacts{Available: true, Node: "worker-1", Disks: []domain.DiskSnapshot{disk}}
	identity := diskIdentityOf(disk)
	if reason := assessDeviceWipeFacts(facts, "worker-1", identity); reason != "" {
		t.Fatalf("matching identity should be allowed, got %q", reason)
	}
	identity.Serial = "serial-b"
	if reason := assessDeviceWipeFacts(facts, "worker-1", identity); reason == "" {
		t.Fatal("changed disk identity must refuse the wipe")
	}
	facts.Available = false
	if reason := assessDeviceWipeFacts(facts, "worker-1", diskIdentityOf(disk)); reason == "" {
		t.Fatal("unavailable disk inventory must refuse the wipe")
	}
}

func TestResetScopeFactsPreservesAvailableEmptyInventory(t *testing.T) {
	facts := DiskPolicyFacts{Available: true, Node: "worker-1", Disks: []domain.DiskSnapshot{}}
	_, userDiskBlock := resetScopeFacts([]string{"worker-1"}, ports.WipeModeUserDisks, facts)
	if userDiskBlock != "refusing: no user disks discovered" {
		t.Fatalf("available-empty inventory lost its known state: %q", userDiskBlock)
	}
	_, allDiskBlock := resetScopeFacts([]string{"worker-1"}, ports.WipeModeAll, facts)
	if allDiskBlock != "" {
		t.Fatalf("all-disk mode should preserve the known-empty inventory behavior, got %q", allDiskBlock)
	}
}
