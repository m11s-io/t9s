package tui

import (
	"fmt"
	"strings"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
)

const maxOverviewCriticalPreview = 3

func overviewSeverityCounts(diagnoses []domain.Diagnosis, kind string) (warning, critical, problematic int) {
	seen := map[string]bool{}
	for _, diagnosis := range diagnoses {
		if diagnosis.ResourceKind != kind {
			continue
		}
		switch diagnosis.Severity {
		case domain.SeverityWarning:
			warning++
		case domain.SeverityCritical:
			critical++
		}
		seen[diagnosis.ResourceID] = true
	}
	return warning, critical, len(seen)
}

func renderOverview(observations application.HealthInput) string {
	diagnoses := application.EvaluateObservations(observations)

	nodeWarning, nodeCritical, nodeProblematic := overviewSeverityCounts(diagnoses, "node")
	totalNodes := len(observations.Nodes.Nodes)
	etcdWarning, etcdCritical, etcdProblematic := overviewSeverityCounts(diagnoses, "etcd-member")
	totalEtcd := len(observations.Etcd.Members)

	var view strings.Builder
	view.WriteString(fmt.Sprintf("NODES     %d/%d healthy, %d warning, %d critical\n",
		totalNodes-nodeProblematic, totalNodes, nodeWarning, nodeCritical))
	view.WriteString(fmt.Sprintf("ETCD      %d/%d healthy, %d warning, %d critical\n",
		totalEtcd-etcdProblematic, totalEtcd, etcdWarning, etcdCritical))
	view.WriteString(statusRollupLine(observations, diagnoses, "BGP", "bgp-peer", statusGroupBGP))
	view.WriteString(statusRollupLine(observations, diagnoses, "STORAGE", "lvm-physical-volume", statusGroupStorage, "lvm-volume-group", "lvm-logical-volume", "md-array"))
	view.WriteString(statusRollupLine(observations, diagnoses, "VOLUMES", "volume", statusGroupVolumes, "volume-trim", "filesystem-scrub"))
	view.WriteString(systemStatusRollupLine(observations, diagnoses))

	criticalCount := 0
	for _, diagnosis := range diagnoses {
		if diagnosis.Severity != domain.SeverityCritical {
			continue
		}
		if criticalCount == 0 {
			view.WriteString("\n")
		}
		if criticalCount >= maxOverviewCriticalPreview {
			break
		}
		view.WriteString(fmt.Sprintf("! %s %s: %s\n", diagnosis.ResourceKind, diagnosis.ResourceName, diagnosis.Summary))
		criticalCount++
	}

	return strings.TrimSuffix(view.String(), "\n")
}

func directHealthInput(cluster directClusterState) application.HealthInput {
	return application.HealthInput{
		Nodes:             cluster.nodes.Value,
		NodesKnown:        cluster.nodes.Status == directReady || cluster.nodes.Status == directPartial,
		Etcd:              cluster.etcd.Value,
		EtcdKnown:         cluster.etcd.Status == directReady || cluster.etcd.Status == directPartial,
		NodeStatuses:      cluster.nodeStatus.Value,
		NodeStatusesKnown: cluster.nodeStatus.Status == directReady || cluster.nodeStatus.Status == directPartial,
		Services:          cluster.services.Value,
		ServicesKnown:     cluster.services.Status == directReady || cluster.services.Status == directPartial,
	}
}

func directHealthNotices(cluster directClusterState) []string {
	var notices []string
	switch cluster.nodes.Status {
	case directPartial:
		notices = append(notices, "NODES PARTIAL: "+fallback(cluster.nodes.Err))
	case directFailed:
		notices = append(notices, "NODES UNAVAILABLE: "+fallback(cluster.nodes.Err))
	}
	switch cluster.etcd.Status {
	case directPartial:
		notices = append(notices, "ETCD PARTIAL: "+fallback(cluster.etcd.Err))
	case directFailed:
		notices = append(notices, "ETCD UNAVAILABLE: "+fallback(cluster.etcd.Err))
	}
	switch cluster.nodeStatus.Status {
	case directPartial:
		notices = append(notices, "TALOS STATUS PARTIAL: "+fallback(cluster.nodeStatus.Err))
	case directFailed:
		notices = append(notices, "TALOS STATUS UNAVAILABLE: "+fallback(cluster.nodeStatus.Err))
	}
	return notices
}

func withHealthNotices(content string, notices []string) string {
	if len(notices) == 0 {
		return content
	}
	return strings.Join(append(notices, content), "\n")
}
