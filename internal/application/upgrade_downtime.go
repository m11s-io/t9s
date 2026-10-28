package application

import "github.com/m11s-io/t9s/internal/domain"

// A singleton maintenance exception must never mask a partially observed HA
// cluster. Require one control plane and one healthy member, including learners.
func singleControlPlaneUpgradeMember(nodes, targets []domain.NodeSnapshot, etcd EtcdPolicyFacts) uint64 {
	if !etcd.Available || len(etcd.Members) != 1 || len(targets) != 1 || targets[0].Role != domain.NodeRoleControl {
		return 0
	}
	count := 0
	for _, node := range nodes {
		if node.Role == domain.NodeRoleControl {
			count++
		}
	}
	if count != 1 {
		return 0
	}
	member := etcd.Members[0]
	if member.MemberID == 0 || member.IsLearner || !member.StatusKnown || len(member.Errors) > 0 || len(member.Alarms) > 0 {
		return 0
	}
	node := targets[0]
	identities := append([]string{node.Name, node.Target()}, node.Addresses...)
	if !memberMatchesAnyTarget(member, identities) {
		return 0
	}
	return member.MemberID
}
