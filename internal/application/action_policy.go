package application

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

// EtcdPolicyFacts is a normalized etcd observation. Available distinguishes a
// successful empty membership set from an unavailable read.
type EtcdPolicyFacts struct {
	Available bool
	Members   []domain.EtcdMemberSnapshot
}

// DiskPolicyFacts is an explicitly available disk inventory for one canonical
// node target. An empty available inventory is different from an unknown one.
type DiskPolicyFacts struct {
	Available bool
	Node      string
	Disks     []domain.DiskSnapshot
}

type etcdQuorumAssessment struct {
	known              bool
	voters             int
	remaining          int
	confirmedRemaining int
	floor              int
	reason             string
}

func (a etcdQuorumAssessment) belowQuorum() bool {
	return a.known && a.remaining < a.floor
}

func (a etcdQuorumAssessment) certainlyBelowQuorum() bool {
	return a.known && a.confirmedRemaining < a.floor
}

// resolveNodeTarget resolves only an identity present in the supplied node
// observation. It returns the canonical node target and rejects ambiguity.
func resolveNodeTarget(nodes []domain.NodeSnapshot, requested string) (domain.NodeSnapshot, bool) {
	if requested == "" {
		return domain.NodeSnapshot{}, false
	}
	matchedIndex := -1
	for i, node := range nodes {
		if nodeMatchesIdentity(node, requested) {
			if matchedIndex != -1 {
				return domain.NodeSnapshot{}, false
			}
			matchedIndex = i
		}
	}
	if matchedIndex == -1 {
		return domain.NodeSnapshot{}, false
	}
	found := nodes[matchedIndex]
	canonical := found.Target()
	if canonical == "" {
		return domain.NodeSnapshot{}, false
	}
	for i, node := range nodes {
		if i != matchedIndex && nodeMatchesIdentity(node, canonical) {
			return domain.NodeSnapshot{}, false
		}
	}
	return found, true
}

func nodeMatchesIdentity(node domain.NodeSnapshot, identity string) bool {
	if identity == "" {
		return false
	}
	if node.Name == identity || node.ID == identity || node.Target() == identity {
		return true
	}
	for _, address := range node.Addresses {
		if address == identity {
			return true
		}
	}
	return false
}

// resolveEtcdMember requires all supplied identity parts to identify the same
// member. Supplying neither identity or resolving to multiple records fails.
func resolveEtcdMember(members []domain.EtcdMemberSnapshot, memberID uint64, hostname string) (domain.EtcdMemberSnapshot, bool) {
	if memberID == 0 && hostname == "" {
		return domain.EtcdMemberSnapshot{}, false
	}
	var found domain.EtcdMemberSnapshot
	count := 0
	for _, member := range members {
		idMatch := memberID == 0 || member.MemberID == memberID
		hostMatch := hostname == "" || member.Hostname == hostname
		if idMatch && hostMatch && (memberID != 0 || hostname != "") {
			found = member
			count++
		}
	}
	if count != 1 {
		return domain.EtcdMemberSnapshot{}, false
	}
	return found, true
}

func assessEtcdQuorumFacts(etcd EtcdPolicyFacts, targets []string) etcdQuorumAssessment {
	if !etcd.Available {
		return etcdQuorumAssessment{reason: "etcd quorum impact unknown (etcd data unavailable)"}
	}
	voters := 0
	for _, member := range etcd.Members {
		if !member.IsLearner {
			voters++
		}
	}
	if voters == 0 {
		return etcdQuorumAssessment{reason: "etcd membership unknown"}
	}
	atRisk, confirmedUnhealthy, unknown := 0, 0, 0
	for _, member := range etcd.Members {
		if member.IsLearner {
			continue
		}
		if memberMatchesAnyTarget(member, targets) {
			atRisk++
			continue
		}
		switch {
		case !member.StatusKnown:
			unknown++
		case len(member.Errors) > 0:
			confirmedUnhealthy++
		}
	}
	return etcdQuorumAssessment{
		known: true, voters: voters,
		remaining:          voters - atRisk - confirmedUnhealthy - unknown,
		confirmedRemaining: voters - atRisk - confirmedUnhealthy,
		floor:              voters/2 + 1,
	}
}

func etcdQuorumWarningFacts(etcd EtcdPolicyFacts, targets []string) string {
	assessment := assessEtcdQuorumFacts(etcd, targets)
	if !assessment.known {
		return "control-plane node(s); " + assessment.reason
	}
	if assessment.belowQuorum() {
		return fmt.Sprintf("control-plane node(s); would drop etcd to %d/%d — below quorum (need %d)", assessment.remaining, assessment.voters, assessment.floor)
	}
	return "control-plane node(s)"
}

func etcdQuorumBlockReasonFacts(etcd EtcdPolicyFacts, targets []string) string {
	assessment := assessEtcdQuorumFacts(etcd, targets)
	if !assessment.certainlyBelowQuorum() {
		return ""
	}
	return fmt.Sprintf("refusing: would drop etcd to %d/%d (need %d)", assessment.confirmedRemaining, assessment.voters, assessment.floor)
}

type etcdMembershipAssessment struct {
	found   bool
	voter   bool
	healthy bool
	target  string
	reason  string
}

func assessEtcdMembershipFacts(etcd EtcdPolicyFacts, memberID uint64, memberHostname string) etcdMembershipAssessment {
	if !etcd.Available {
		return etcdMembershipAssessment{reason: "etcd membership unknown (etcd data unavailable)"}
	}
	member, ok := resolveEtcdMember(etcd.Members, memberID, memberHostname)
	if !ok {
		return etcdMembershipAssessment{reason: "member is not in the current etcd snapshot"}
	}
	return etcdMembershipAssessment{
		found: true, voter: !member.IsLearner,
		healthy: member.StatusKnown && len(member.Errors) == 0,
		target:  membershipTargetForMember(member),
	}
}

func etcdMembershipBlockReasonFacts(etcd EtcdPolicyFacts, kind EtcdActionKind, memberID uint64, memberHostname string) string {
	assessment := assessEtcdMembershipFacts(etcd, memberID, memberHostname)
	if !assessment.found {
		return "refusing: " + assessment.reason
	}
	if kind == EtcdActionRemoveMember && assessment.healthy {
		return "refusing: member is healthy — use leave (L)"
	}
	if assessment.target == "" {
		return "refusing: cannot identify the member to change"
	}
	quorum := assessEtcdQuorumFacts(etcd, []string{assessment.target})
	if !quorum.known {
		return "refusing: etcd quorum impact unknown"
	}
	if quorum.belowQuorum() {
		return fmt.Sprintf("refusing: would drop etcd to %d/%d — below quorum (need %d)", quorum.remaining, quorum.voters, quorum.floor)
	}
	return ""
}

func etcdMembershipWarningFacts(etcd EtcdPolicyFacts, _ EtcdActionKind, memberID uint64, memberHostname string) string {
	assessment := assessEtcdMembershipFacts(etcd, memberID, memberHostname)
	if !assessment.found || !assessment.voter || assessment.target == "" {
		return ""
	}
	quorum := assessEtcdQuorumFacts(etcd, []string{assessment.target})
	if !quorum.known {
		return quorum.reason
	}
	if quorum.remaining <= quorum.floor {
		return fmt.Sprintf("would drop etcd to %d/%d — no fault tolerance (need %d)", quorum.remaining, quorum.voters, quorum.floor)
	}
	return ""
}

func etcdSnapshotNodeForFacts(etcd EtcdPolicyFacts, memberID uint64, memberHostname string) string {
	if etcd.Available {
		for _, member := range etcd.Members {
			if member.IsLearner || member.Hostname == "" || memberMatchesMember(member, memberID, memberHostname) {
				continue
			}
			if member.StatusKnown && len(member.Errors) == 0 {
				return member.Hostname
			}
		}
	}
	return memberHostname
}

// resetQuorumBlockReasonFacts applies the destructive reset gate to normalized
// etcd observations. Unlike ordinary node actions, reset refuses unknown
// quorum and counts unknown peers as lost when calculating the floor.
func resetQuorumBlockReasonFacts(nodes []domain.NodeSnapshot, etcd EtcdPolicyFacts, targets []string) string {
	if !targetsIncludeControlPlane(nodes, targets) {
		return ""
	}
	assessment := assessEtcdQuorumFacts(etcd, targets)
	if !assessment.known {
		return "refusing: " + assessment.reason
	}
	if assessment.belowQuorum() {
		return fmt.Sprintf("refusing: would drop etcd to %d/%d — below quorum (need %d)", assessment.remaining, assessment.voters, assessment.floor)
	}
	return ""
}

func upgradeActionWarningFacts(nodes []domain.NodeSnapshot, etcd EtcdPolicyFacts, targets []string, image string) string {
	warning := ""
	if targetsIncludeControlPlane(nodes, targets) {
		warning = etcdQuorumWarningFacts(etcd, targets)
	}
	if len(targets) == 0 {
		return warning
	}
	for _, node := range nodes {
		for _, target := range targets {
			if node.Target() == target {
				if minor := UpgradeMinorWarning(node.Version, image); minor != "" {
					if warning != "" {
						return warning + "; " + minor
					}
					return minor
				}
			}
		}
	}
	return warning
}

func etcdLeadershipWarningFacts(etcd EtcdPolicyFacts, memberHostname string) string {
	if !etcd.Available {
		return ""
	}
	targetIsLeader := false
	for _, member := range etcd.Members {
		if member.Hostname == memberHostname && member.StatusKnown && member.IsLeader {
			targetIsLeader = true
			break
		}
	}
	if !targetIsLeader {
		return ""
	}
	for _, member := range etcd.Members {
		if member.IsLearner || member.Hostname == memberHostname {
			continue
		}
		if member.StatusKnown && len(member.Errors) == 0 {
			return ""
		}
	}
	return "no healthy follower is known to take leadership"
}

type diskIdentity struct {
	DeviceName string
	Model      string
	Serial     string
	Type       string
	SizeBytes  uint64
	BusPath    string
}

func diskIdentityOf(disk domain.DiskSnapshot) diskIdentity {
	return diskIdentity{disk.DeviceName, disk.Model, disk.Serial, disk.Type, disk.SizeBytes, disk.BusPath}
}

// diskIdentityMatches compares the device token and every stable attribute
// supplied by the caller. Omitted optional attributes do not weaken any
// attribute that is present in both observations.
func diskIdentityMatches(observed domain.DiskSnapshot, requested diskIdentity) bool {
	if NormalizeDeviceToken(observed.DeviceName) != NormalizeDeviceToken(requested.DeviceName) {
		return false
	}
	return (requested.Model == "" || observed.Model == requested.Model) &&
		(requested.Serial == "" || observed.Serial == requested.Serial) &&
		(requested.Type == "" || observed.Type == requested.Type) &&
		(requested.SizeBytes == 0 || observed.SizeBytes == requested.SizeBytes) &&
		(requested.BusPath == "" || observed.BusPath == requested.BusPath)
}

func assessDeviceWipeFacts(inventory DiskPolicyFacts, node string, requested diskIdentity) string {
	if node == "" || requested.DeviceName == "" {
		return "refusing: no device selected"
	}
	if !inventory.Available || inventory.Node != node {
		return fmt.Sprintf("refusing: disk inventory for %s is not loaded", node)
	}
	for _, disk := range inventory.Disks {
		if !diskIdentityMatches(disk, requested) {
			continue
		}
		if disk.SystemDisk {
			return fmt.Sprintf("refusing: %s is the system disk", requested.DeviceName)
		}
		if disk.ReadOnly {
			return fmt.Sprintf("refusing: %s is read-only", requested.DeviceName)
		}
		return ""
	}
	return fmt.Sprintf("refusing: %s is not in the current inventory", requested.DeviceName)
}

func resetScopeFacts(targets []string, mode ports.WipeMode, inventory DiskPolicyFacts) ([]string, string) {
	var preview *ResetPreview
	if inventory.Available && len(targets) == 1 && inventory.Node == targets[0] {
		classified := BuildResetPreview(inventory.Node, domain.DiskSet{Disks: inventory.Disks})
		// The facts say this inventory read completed. BuildResetPreview alone
		// cannot distinguish that known-empty result from a missing read.
		classified.Known = true
		preview = &classified
	}
	return resetScopeForPreview(targets, mode, preview)
}

func resetScopeForPreview(targets []string, mode ports.WipeMode, preview *ResetPreview) ([]string, string) {
	switch mode {
	case ports.WipeModeSystemDisk:
		return nil, ""
	case ports.WipeModeUserDisks:
		if len(targets) != 1 {
			return nil, "refusing: user-disk wipe is single-node only"
		}
		if preview == nil || !preview.Known {
			return nil, "refusing: user disk inventory unknown"
		}
		if len(preview.UserDisks) == 0 {
			return nil, "refusing: no user disks discovered"
		}
		return append([]string(nil), preview.UserDisks...), ""
	default:
		if len(targets) != 1 {
			return nil, "refusing: all-disk wipe is single-node only"
		}
		if preview == nil || !preview.Known {
			return nil, "refusing: user disk inventory unknown"
		}
		return append([]string(nil), preview.UserDisks...), ""
	}
}

func memberMatchesAnyTarget(member domain.EtcdMemberSnapshot, targets []string) bool {
	for _, target := range targets {
		if target == "" {
			continue
		}
		if member.Hostname == target || (member.MemberID != 0 && strconv.FormatUint(member.MemberID, 10) == target) {
			return true
		}
		for _, raw := range member.ClientURLs {
			if endpointHost(raw) == target {
				return true
			}
		}
		for _, raw := range member.PeerURLs {
			if endpointHost(raw) == target {
				return true
			}
		}
	}
	return false
}

func endpointHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
