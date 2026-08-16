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
	base := application.Model{
		ContextName: "mgmt", Contexts: contexts,
		Nodes:           application.NodeState{Status: application.Ready, Value: nodesTestState().Value},
		Services:        servicesTestState(),
		Events:          eventsTestState(),
		Etcd:            etcdTestState(),
		Processes:       processesTestState(),
		Disks:           disksTestState(),
		Network:         networkTestState(),
		ResourceBrowser: resourceKindsTestState(),
	}

	tests := []struct {
		name          string
		width, height int
		prepare       func(model) model
	}{
		{name: "services-160x50", width: 160, height: 50, prepare: func(root model) model {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
			return root
		}},
		{name: "contexts-160x50", width: 160, height: 50, prepare: func(root model) model {
			return enterCommand(t, root, "ctx")
		}},
		{name: "events-160x50", width: 160, height: 50, prepare: func(root model) model {
			return enterCommand(t, root, "events")
		}},
		{name: "etcd-160x50", width: 160, height: 50, prepare: func(root model) model {
			return enterCommand(t, root, "etcd")
		}},
		{name: "services-80x24", width: 80, height: 24, prepare: func(root model) model {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewServices, Label: "services"})
			return root
		}},
		{name: "processes-160x50", width: 160, height: 50, prepare: func(root model) model {
			root.views = root.views.push(viewFrame{Kind: viewProcesses, Label: "cp-1 > processes"})
			return root
		}},
		{name: "disks-160x50", width: 160, height: 50, prepare: func(root model) model {
			root.views = root.views.push(viewFrame{Kind: viewDisks, Label: "cp-1 > disks"})
			return root
		}},
		{name: "network-160x50", width: 160, height: 50, prepare: func(root model) model {
			root.views = root.views.push(viewFrame{Kind: viewNetwork, Label: "cp-1 > network"})
			return root
		}},
		{name: "overview-160x50", width: 160, height: 50, prepare: func(root model) model {
			return enterCommand(t, root, "overview")
		}},
		{name: "problems-160x50", width: 160, height: 50, prepare: func(root model) model {
			return enterCommand(t, root, "problems")
		}},
		{name: "resources-kinds-160x50", width: 160, height: 50, prepare: func(root model) model {
			root.views = root.views.replaceRoot(viewFrame{Kind: viewResourceKinds, Label: "resources"})
			return root
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newModel(t.Context(), false, base, nil)
			root.splash = false
			root = test.prepare(root)
			updated, _ := root.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			view := updated.(model).View()
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
