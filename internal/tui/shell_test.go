package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellGolden(t *testing.T) {
	for _, size := range []struct {
		width, height int
	}{{width: 80, height: 24}, {width: 120, height: 40}} {
		name := strconv.Itoa(size.width) + "x" + strconv.Itoa(size.height)
		t.Run(name, func(t *testing.T) {
			root := readyRootModel()
			root.splash = false
			updated, _ := root.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			got := updated.(model).View().Content + "\n"
			path := filepath.Join("testdata", "shell-"+name+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}

			want, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
		})
	}
}

func TestRootUsesAlternateScreenAndExactTerminalDimensions(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{name: "standard", width: 80, height: 24},
		{name: "wide", width: 120, height: 40},
		{name: "tiny", width: 10, height: 4},
	} {
		t.Run(size.name, func(t *testing.T) {
			root := readyRootModel()
			updated, _ := root.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			view := updated.(model).View()

			assert.True(t, view.AltScreen)
			lines := strings.Split(view.Content, "\n")
			require.Len(t, lines, size.height)
			for _, line := range lines {
				assert.LessOrEqual(t, ansi.StringWidth(line), size.width, "%q", line)
				assert.Equal(t, strings.TrimRight(line, " "), line, "shell lines must not contain trailing spaces")
			}
		})
	}
}

func TestRootSetsBlackTerminalBackgroundForTheWholeViewport(t *testing.T) {
	view := readyRootModel().View()
	require.NotNil(t, view.BackgroundColor)

	red, green, blue, alpha := view.BackgroundColor.RGBA()
	assert.Equal(t, uint32(0), red)
	assert.Equal(t, uint32(0), green)
	assert.Equal(t, uint32(0), blue)
	assert.Equal(t, uint32(0xffff), alpha)
}

func TestSplashStartsAlongsideApplicationLoading(t *testing.T) {
	root := readyRootModel()

	assert.Contains(t, root.View().Content, "Inspect Talos clusters")
	assert.NotContains(t, root.View().Content, "NAME")

	command := root.Init()
	require.NotNil(t, command)
	batch, ok := command().(tea.BatchMsg)
	require.True(t, ok, "startup must batch the splash timer with application loading")
	require.Len(t, batch, 3)

	updated, _ := root.Update(applicationMessage{message: application.ContextsLoaded{
		Generation:  1,
		Contexts:    []domain.ClusterContext{{Name: "prod", Current: true}},
		ContextName: "prod",
	}})
	root = updated.(model)
	assert.Contains(t, root.View().Content, "Inspect Talos clusters")
	assert.Equal(t, "prod", root.application.ContextName)

	updated, _ = root.Update(splashDoneMsg{})
	assert.Contains(t, updated.(model).View().Content, "NAME")
}

func TestNodeAutoRefreshTickRefreshesNodesWhileNodeViewIsActive(t *testing.T) {
	root := readyRootModel()
	require.Equal(t, viewNodes, root.views.top().Kind)
	require.Equal(t, application.Ready, root.application.Nodes.Status)

	updated, cmd := root.Update(nodeAutoRefreshTickMsg{})

	require.NotNil(t, cmd, "the heartbeat must reschedule itself")
	assert.Equal(t, application.Loading, updated.(model).application.Nodes.Status, "a tick on the node view must trigger the same refresh as pressing r")
}

func TestNodeAutoRefreshTickSkipsRefreshOffTheNodeView(t *testing.T) {
	root := readyRootModel()
	root.views = root.views.push(viewFrame{Kind: viewProcesses, Label: "cp-1 > processes"})

	updated, cmd := root.Update(nodeAutoRefreshTickMsg{})

	require.NotNil(t, cmd, "the heartbeat must keep rescheduling even when it skips a refresh")
	assert.Equal(t, application.Ready, updated.(model).application.Nodes.Status, "leaving the node view untouched must not disturb its loaded state")
}

func TestNodeAutoRefreshTickSkipsRefreshWhileFiltering(t *testing.T) {
	root := readyRootModel()
	root.nodes = root.nodes.update(keyPress('/'))
	require.True(t, root.filtering())

	updated, cmd := root.Update(nodeAutoRefreshTickMsg{})

	require.NotNil(t, cmd)
	assert.Equal(t, application.Ready, updated.(model).application.Nodes.Status, "a tick during filtering must not disturb the list being filtered")
}

func readyRootModel() model {
	runner := application.NewRunner(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return nil, nil
		}},
	})
	return newModel(context.Background(), false, application.Model{
		ContextName: "prod",
		Nodes: application.NodeState{Status: application.Ready, Value: domain.NodeSet{Nodes: []domain.NodeSnapshot{{
			ID: "cp-1", Name: "cp-1", Role: domain.NodeRoleControl, Kubernetes: domain.KubernetesUnknown,
		}}}},
	}, runner)
}
