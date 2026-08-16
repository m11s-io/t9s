package application

import (
	"fmt"
	"sort"

	"github.com/m11s-io/t9s/internal/domain"
)

func evaluableStatus(status LoadStatus) bool {
	return status == Ready || status == Partial
}

func EvaluateHealth(model Model) []domain.Diagnosis {
	var diagnoses []domain.Diagnosis

	if evaluableStatus(model.Nodes.Status) {
		for _, node := range model.Nodes.Value.Nodes {
			diagnoses = append(diagnoses, evaluateNodeReadiness(node)...)
			diagnoses = append(diagnoses, evaluateNodeServicesDegraded(node)...)
		}
	}

	if evaluableStatus(model.Etcd.Status) {
		for _, member := range model.Etcd.Value.Members {
			diagnoses = append(diagnoses, evaluateEtcdMemberUnhealthy(member)...)
		}
	}

	sort.SliceStable(diagnoses, func(i, j int) bool {
		if diagnoses[i].Severity != diagnoses[j].Severity {
			return diagnoses[i].Severity > diagnoses[j].Severity
		}
		if diagnoses[i].ResourceKind != diagnoses[j].ResourceKind {
			return diagnoses[i].ResourceKind < diagnoses[j].ResourceKind
		}
		return diagnoses[i].ResourceName < diagnoses[j].ResourceName
	})

	return diagnoses
}

func evaluateNodeReadiness(node domain.NodeSnapshot) []domain.Diagnosis {
	var severity domain.Severity
	switch node.Health {
	case domain.HealthUnhealthy:
		severity = domain.SeverityCritical
	case domain.HealthUnknown:
		severity = domain.SeverityUnknown
	default:
		return nil
	}

	evidence := []string{"stage=" + node.Stage}
	if node.Problem != "" {
		evidence = append(evidence, "problem="+node.Problem)
	}

	return []domain.Diagnosis{{
		RuleID:       "node-readiness",
		Severity:     severity,
		Summary:      "node not ready",
		Evidence:     evidence,
		ResourceKind: "node",
		ResourceID:   node.ID,
		ResourceName: node.DisplayName(),
	}}
}

func evaluateNodeServicesDegraded(node domain.NodeSnapshot) []domain.Diagnosis {
	if !node.Services.Known || node.Services.Healthy >= node.Services.Total {
		return nil
	}

	severity := domain.SeverityWarning
	if node.Services.Healthy == 0 {
		severity = domain.SeverityCritical
	}

	return []domain.Diagnosis{{
		RuleID:       "node-services-degraded",
		Severity:     severity,
		Summary:      "services degraded",
		Evidence:     []string{fmt.Sprintf("%d/%d services healthy", node.Services.Healthy, node.Services.Total)},
		ResourceKind: "node",
		ResourceID:   node.ID,
		ResourceName: node.DisplayName(),
	}}
}

func evaluateEtcdMemberUnhealthy(member domain.EtcdMemberSnapshot) []domain.Diagnosis {
	id := fmt.Sprintf("%d", member.MemberID)

	if !member.StatusKnown {
		return []domain.Diagnosis{{
			RuleID:       "etcd-member-unhealthy",
			Severity:     domain.SeverityUnknown,
			Summary:      "etcd member status unknown",
			Evidence:     []string{"status unknown"},
			ResourceKind: "etcd-member",
			ResourceID:   id,
			ResourceName: member.Hostname,
		}}
	}

	if len(member.Errors) == 0 {
		return nil
	}

	return []domain.Diagnosis{{
		RuleID:       "etcd-member-unhealthy",
		Severity:     domain.SeverityWarning,
		Summary:      "etcd member reporting errors",
		Evidence:     append([]string(nil), member.Errors...),
		ResourceKind: "etcd-member",
		ResourceID:   id,
		ResourceName: member.Hostname,
	}}
}
