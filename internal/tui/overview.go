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

func renderOverview(model application.Model) string {
	diagnoses := application.EvaluateHealth(model)

	nodeWarning, nodeCritical, nodeProblematic := overviewSeverityCounts(diagnoses, "node")
	totalNodes := len(model.Nodes.Value.Nodes)
	etcdWarning, etcdCritical, etcdProblematic := overviewSeverityCounts(diagnoses, "etcd-member")
	totalEtcd := len(model.Etcd.Value.Members)

	var view strings.Builder
	view.WriteString(fmt.Sprintf("NODES     %d/%d healthy, %d warning, %d critical\n",
		totalNodes-nodeProblematic, totalNodes, nodeWarning, nodeCritical))
	view.WriteString(fmt.Sprintf("ETCD      %d/%d healthy, %d warning, %d critical\n",
		totalEtcd-etcdProblematic, totalEtcd, etcdWarning, etcdCritical))

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
