package application_test

import (
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateHealthEmitsNothingForAHealthyNode(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "cp-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Healthy: 7, Total: 7, Known: true}},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	assert.Empty(t, diagnoses)
}

func TestEvaluateHealthNodeReadinessRule(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "worker-1", Health: domain.HealthUnhealthy, Stage: "maintenance"},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 1)
	assert.Equal(t, "node-readiness", diagnoses[0].RuleID)
	assert.Equal(t, domain.SeverityCritical, diagnoses[0].Severity)
	assert.Equal(t, "node", diagnoses[0].ResourceKind)
	assert.Equal(t, "node-1", diagnoses[0].ResourceID)
	assert.Equal(t, "worker-1", diagnoses[0].ResourceName)
	assert.Contains(t, diagnoses[0].Evidence, "stage=maintenance")
}

func TestEvaluateHealthNodeReadinessRuleUnknownHealth(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-2", Name: "unknown-1", Health: domain.HealthUnknown, Problem: "unreachable"},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 1)
	assert.Equal(t, domain.SeverityUnknown, diagnoses[0].Severity)
	assert.Contains(t, diagnoses[0].Evidence, "problem=unreachable")
}

func TestEvaluateHealthNodeServicesDegradedRule(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "worker-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Healthy: 6, Total: 7, Known: true}},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 1)
	assert.Equal(t, "node-services-degraded", diagnoses[0].RuleID)
	assert.Equal(t, domain.SeverityWarning, diagnoses[0].Severity)
	assert.Contains(t, diagnoses[0].Evidence, "6/7 services healthy")
}

func TestEvaluateHealthNodeServicesDegradedRuleAllUnhealthyIsCritical(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "worker-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Healthy: 0, Total: 7, Known: true}},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 1)
	assert.Equal(t, domain.SeverityCritical, diagnoses[0].Severity)
}

func TestEvaluateHealthNodeServicesDegradedRuleSkippedWhenUnknown(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "unknown-1", Health: domain.HealthHealthy, Services: domain.ServiceSummary{Known: false}},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	assert.Empty(t, diagnoses)
}

func TestEvaluateHealthNodeContributesBothRulesSimultaneously(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "worker-1", Health: domain.HealthUnhealthy, Services: domain.ServiceSummary{Healthy: 6, Total: 7, Known: true}},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 2)
	ruleIDs := []string{diagnoses[0].RuleID, diagnoses[1].RuleID}
	assert.Contains(t, ruleIDs, "node-readiness")
	assert.Contains(t, ruleIDs, "node-services-degraded")
}

func TestEvaluateHealthEtcdMemberUnhealthyRule(t *testing.T) {
	model := application.HealthInput{
		EtcdKnown: true, Etcd: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, StatusKnown: true},
			{Hostname: "cp-2", MemberID: 2, StatusKnown: true, Errors: []string{"connection refused"}},
			{Hostname: "cp-3", MemberID: 3, StatusKnown: false},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 2)
	assert.Equal(t, "etcd-member-unhealthy", diagnoses[0].RuleID)
	assert.Equal(t, "etcd-member", diagnoses[0].ResourceKind)
}

func TestEvaluateHealthEtcdMemberAlarmedRule(t *testing.T) {
	model := application.HealthInput{
		EtcdKnown: true, Etcd: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, StatusKnown: true},
			{Hostname: "cp-2", MemberID: 2, StatusKnown: true, Alarms: []string{"NOSPACE"}},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 1)
	assert.Equal(t, "etcd-member-alarmed", diagnoses[0].RuleID)
	assert.Equal(t, domain.SeverityCritical, diagnoses[0].Severity)
	assert.Equal(t, "cp-2", diagnoses[0].ResourceName)
	assert.Contains(t, diagnoses[0].Evidence, "NOSPACE")
}

func TestEvaluateHealthSkipsCollectionsNotYetLoaded(t *testing.T) {
	model := application.HealthInput{
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "worker-1", Health: domain.HealthUnhealthy},
		}},
		Etcd: domain.EtcdSet{},
	}

	diagnoses := application.EvaluateObservations(model)

	assert.Empty(t, diagnoses)
}

func TestEvaluateHealthSortsBySeverityDescendingThenKindThenName(t *testing.T) {
	model := application.HealthInput{
		NodesKnown: true, Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{
			{ID: "node-1", Name: "b-node", Health: domain.HealthUnknown},
			{ID: "node-2", Name: "a-node", Health: domain.HealthUnhealthy},
		}},
		EtcdKnown: true, Etcd: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, StatusKnown: true, Errors: []string{"unreachable"}},
		}},
	}

	diagnoses := application.EvaluateObservations(model)

	require.Len(t, diagnoses, 3)
	assert.Equal(t, domain.SeverityCritical, diagnoses[0].Severity)
	assert.Equal(t, "a-node", diagnoses[0].ResourceName)
	assert.Equal(t, domain.SeverityWarning, diagnoses[1].Severity)
	assert.Equal(t, domain.SeverityUnknown, diagnoses[2].Severity)
}

func TestHealthUnknownObservationsStayUnknown(t *testing.T) {
	got := application.EvaluateObservations(application.HealthInput{
		Nodes: domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "node", Health: domain.HealthUnhealthy}}},
		Etcd:  domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1", MemberID: 1, StatusKnown: false}}},
	})
	assert.Empty(t, got, "unknown observations must not be interpreted as healthy or unhealthy")

	got = application.EvaluateObservations(application.HealthInput{
		NodesKnown: true,
		Nodes:      domain.NodeSet{Nodes: []domain.NodeSnapshot{{Name: "node", Health: domain.HealthUnknown}}},
		EtcdKnown:  true,
		Etcd:       domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1", MemberID: 1, StatusKnown: false}}},
	})
	require.Len(t, got, 2)
	assert.Equal(t, domain.SeverityUnknown, got[0].Severity)
	assert.Equal(t, "etcd-member", got[0].ResourceKind)
	assert.Equal(t, domain.SeverityUnknown, got[1].Severity)
	assert.Equal(t, "node", got[1].ResourceKind)
}
