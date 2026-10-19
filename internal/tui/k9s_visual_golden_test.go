package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestK9sVisualGolden(t *testing.T) {
	contexts := []domain.ClusterContext{
		{Name: "ai", Cluster: "ai", Endpoints: []string{"10.10.18.11"}, Nodes: []string{"10.10.18.11", "10.10.18.51"}},
		{Name: "mgmt", Cluster: "mgmt", Endpoints: []string{"10.10.13.11", "10.10.13.12"}, Nodes: []string{"10.10.13.11"}, Current: true},
		{Name: "stage", Cluster: "stage", Endpoints: []string{"10.10.16.11"}},
		{Name: "test", Cluster: "test", Endpoints: []string{"10.10.17.11"}},
	}
	base := newDirectModel(t.Context(), application.NewClient(application.Dependencies{}), LaunchOptions{ContextName: "mgmt"})
	base.splash = false
	base.cluster.contexts = contexts
	base.cluster.nodes = application.NodeState{Status: application.Ready, Value: nodesTestState().Value}
	base.cluster.services = directServiceState{Status: directReady, Value: servicesTestState()}
	base.cluster.events = directEventState{Status: directReady, Value: domain.EventSet{Events: eventsTestState().Value.Events}}
	base.cluster.etcd = directEtcdState{Status: directReady, Value: etcdTestState().Value}
	base.processes = newDirectProcessesModel().setResult("cp-1", processesTestState().Value, nil)
	base.disks = newDirectDisksModel().setResult("cp-1", disksTestState().Value, nil)
	base.network = newDirectNetworkModel().setResult("cp-1", networkTestState().Value, nil)
	base.resourceKinds = base.resourceKinds.setKinds(resourceKindsTestState().Kinds, directReady, "")

	tests := []struct {
		name          string
		width, height int
		prepare       func(directModel) directModel
	}{
		{name: "services-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
			return root
		}},
		{name: "contexts-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.contexts = newContextsModel(contexts, "mgmt")
			return root
		}},
		{name: "events-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewEvents, Label: "events"})
			return root
		}},
		{name: "etcd-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
			return root
		}},
		{name: "services-80x24", width: 80, height: 24, prepare: func(root directModel) directModel {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
			return root
		}},
		{name: "processes-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.push(viewFrame{Kind: viewProcesses, Label: "cp-1 > processes"})
			return root
		}},
		{name: "disks-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.push(viewFrame{Kind: viewDisks, Label: "cp-1 > disks"})
			return root
		}},
		{name: "network-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.push(viewFrame{Kind: viewNetwork, Label: "cp-1 > network"})
			return root
		}},
		{name: "overview-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewOverview, Label: "overview"})
			return root
		}},
		{name: "problems-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewProblems, Label: "problems"})
			return root
		}},
		{name: "resources-kinds-160x50", width: 160, height: 50, prepare: func(root directModel) directModel {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewResourceKinds, Label: "resources"})
			return root
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := base
			root = test.prepare(root)
			updated, _ := root.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			view := updated.(directModel).View()
			got := view.Content + "\n"
			path := filepath.Join("testdata", "k9s-"+test.name+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
			lines := strings.Split(view.Content, "\n")
			require.Len(t, lines, test.height)
			for _, line := range lines {
				assert.LessOrEqual(t, ansi.StringWidth(line), test.width)
			}
			assert.Contains(t, view.Content, "┌")
			assert.Contains(t, view.Content, "┘")
			assert.Contains(t, view.Content, "<")
		})
	}
}
