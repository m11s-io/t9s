package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func problemsTestDiagnoses() []domain.Diagnosis {
	return []domain.Diagnosis{
		{RuleID: "node-readiness", Severity: domain.SeverityCritical, Summary: "node not ready", ResourceKind: "node", ResourceID: "node-1", ResourceName: "worker-1"},
		{RuleID: "etcd-member-unhealthy", Severity: domain.SeverityWarning, Summary: "etcd member reporting errors", ResourceKind: "etcd-member", ResourceID: "2", ResourceName: "cp-2"},
	}
}

func renderProblems(width int, diagnoses []domain.Diagnosis) string {
	return newProblemsModel(diagnoses).view(width)
}

func TestProblemsRenderSemanticColumns(t *testing.T) {
	rendered := renderProblems(120, problemsTestDiagnoses())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"SEVERITY", "KIND", "RESOURCE", "SUMMARY"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "critical")
	assert.Contains(t, ansi.Strip(lines[1]), "node")
	assert.Contains(t, ansi.Strip(lines[1]), "worker-1")
	assert.Contains(t, ansi.Strip(lines[1]), "node not ready")
}

func TestProblemsRenderEmptyState(t *testing.T) {
	model := newProblemsModel(nil)
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No problems", rendered)
}

func TestProblemsFilterMatchesResourceName(t *testing.T) {
	problems := newProblemsModel(problemsTestDiagnoses())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('w'), keyPress('o'), keyPress('r'),
		{Code: tea.KeyEnter},
	} {
		problems = problems.update(message)
	}

	rendered := problems.view(120)

	assert.Contains(t, rendered, "worker-1")
	assert.NotContains(t, rendered, "cp-2")
}
