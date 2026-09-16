package application

import (
	"testing"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildResetPreviewExcludesReadOnlyDisks(t *testing.T) {
	preview := BuildResetPreview("cp-1", domain.DiskSet{Disks: []domain.DiskSnapshot{
		{DeviceName: "/dev/sda", SystemDisk: true},
		{DeviceName: "/dev/sdb"},
		{DeviceName: "/dev/sr0", ReadOnly: true},
	}})

	require.True(t, preview.Known)
	assert.Equal(t, []string{"/dev/sdb"}, preview.UserDisks,
		"read-only devices must never be listed; the server rejects the whole reset request if any listed disk is read-only")
}

func TestResetQuorumBlockReasonRefusesWhenAnotherVoterHealthUnknown(t *testing.T) {
	// A reset is irreversible. With one peer's health unknown, resetting a
	// healthy control-plane node could leave a single reachable voter, so the
	// pessimistic predicate must refuse even though the best case keeps quorum.
	nodes := []domain.NodeSnapshot{{Name: "cp-1", Role: domain.NodeRoleControl}}
	etcd := EtcdState{Status: Ready, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
		{MemberID: 1, Hostname: "cp-1", StatusKnown: true},
		{MemberID: 2, Hostname: "cp-2", StatusKnown: false},
		{MemberID: 3, Hostname: "cp-3", StatusKnown: true},
	}}}

	block := resetQuorumBlockReason(nodes, etcd, []string{"cp-1"})

	assert.Contains(t, block, "below quorum", "an unknown peer must count as at-risk for an irreversible reset")
}

func TestBuildResetPreviewClassifiesSystemAndUserDisks(t *testing.T) {
	preview := BuildResetPreview("cp-1", domain.DiskSet{Disks: []domain.DiskSnapshot{
		{DeviceName: "sda", SystemDisk: true},
		{DeviceName: "sdb"},
		{DeviceName: "sdc"},
	}})

	assert.True(t, preview.Known)
	assert.Equal(t, "cp-1", preview.Node)
	assert.Equal(t, "sda", preview.SystemDisk)
	assert.Equal(t, []string{"sdb", "sdc"}, preview.UserDisks)

	unknown := BuildResetPreview("cp-1", domain.DiskSet{})
	assert.False(t, unknown.Known)
}

func TestResetConfirmationTokenShapes(t *testing.T) {
	assert.Equal(t, "cp-1", ResetConfirmationToken([]string{"cp-1"}))
	assert.Equal(t, "wipe 2 nodes", ResetConfirmationToken([]string{"cp-1", "cp-2"}))
	assert.Equal(t, "", ResetConfirmationToken(nil))

	assert.NoError(t, ValidateResetConfirmation([]string{"cp-1"}, "cp-1"))
	assert.NoError(t, ValidateResetConfirmation([]string{"a", "b"}, "wipe 2 nodes"))
	assert.Error(t, ValidateResetConfirmation([]string{"cp-1"}, "cp-2"))
	assert.Error(t, ValidateResetConfirmation(nil, ""))
}

func TestResetModeBlockReasonBlocksUserDiskModesWhenPreviewUnknown(t *testing.T) {
	for _, mode := range []ports.WipeMode{ports.WipeModeAll, ports.WipeModeUserDisks} {
		block := resetModeBlockReason([]string{"worker-1"}, ports.ResetOptions{Mode: mode}, nil)
		assert.Contains(t, block, "inventory", "mode %v must refuse an unverifiable user-disk wipe", mode)
	}

	// SYSTEM_DISK does not touch user disks, so an unknown inventory is fine.
	assert.Empty(t, resetModeBlockReason([]string{"worker-1"}, ports.ResetOptions{Mode: ports.WipeModeSystemDisk}, nil))
}

func TestResetScopeListsUserDisksForAllAndUserDiskModes(t *testing.T) {
	preview := BuildResetPreview("worker-1", domain.DiskSet{Disks: []domain.DiskSnapshot{
		{DeviceName: "sda", SystemDisk: true},
		{DeviceName: "sdb"},
		{DeviceName: "sdc"},
	}})

	for _, mode := range []ports.WipeMode{ports.WipeModeAll, ports.WipeModeUserDisks} {
		userDisks, block := resetScope([]string{"worker-1"}, ports.ResetOptions{Mode: mode}, &preview)
		assert.Empty(t, block)
		assert.Equal(t, []string{"sdb", "sdc"}, userDisks, "Talos only wipes listed user disks, so mode %v must list them", mode)
	}
}

func TestResetScopeBlocksUserDiskModesForBulkTargets(t *testing.T) {
	preview := BuildResetPreview("worker-1", domain.DiskSet{Disks: []domain.DiskSnapshot{{DeviceName: "sdb"}}})

	_, block := resetScope([]string{"worker-1", "worker-2"}, ports.ResetOptions{Mode: ports.WipeModeUserDisks}, &preview)

	assert.Contains(t, block, "single-node")
}

func TestResetActionRiskGracefulNotesEtcdLeave(t *testing.T) {
	nodes := []domain.NodeSnapshot{{Name: "worker-1", Role: domain.NodeRoleWorker}}
	warning, _ := resetActionRisk(nodes, EtcdState{}, []string{"worker-1"}, ports.ResetOptions{Mode: ports.WipeModeAll, Graceful: true})

	assert.Contains(t, warning, "etcd")
}
