package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/domain"
)

type directRunCommand struct{ value string }

// runClusterCommand handles the root-owned cluster navigation aliases while
// each screen is migrated in its own task.
func (m *directModel) runClusterCommand(value string) tea.Cmd {
	previous := m.views
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), ":"))
	var command tea.Cmd
	switch resolveCommand(value) {
	case commandNodes:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewNodes, Label: "nodes"})
	case commandServices:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
	case commandEvents:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
	case commandEtcd:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	case commandOverview:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewOverview, Label: "overview"})
	case commandProblems:
		m.views = m.views.replaceRoot(viewFrame{Kind: viewProblems, Label: "problems"})
	case commandResources:
		kind, _ := resourcesCommandArgument(value)
		command = m.openDirectResources(kind)
	case commandClusterHealth:
		timeout := defaultClusterHealthWait
		if parsed, ok := healthcheckCommandArgument(value); ok {
			timeout = parsed
		}
		_, command = m.openDirectClusterHealth(timeout)
	case commandContexts:
		m.contexts = newContextsModel(m.cluster.contexts, m.cluster.contextName)
	default:
		m.view.notice = unknownCommandNotice(value)
	}
	m.cancelClosedDirectDiagnostics(previous)
	m.cancelClosedDirectResources(previous)
	m.closeStreamsOutsideView(previous)
	return command
}

func directContextKnown(contexts []domain.ClusterContext, name string) bool {
	for _, clusterContext := range contexts {
		if clusterContext.Name == name {
			return true
		}
	}
	return false
}
