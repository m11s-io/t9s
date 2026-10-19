package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellGolden(t *testing.T) {
	for _, size := range []struct {
		width, height int
	}{{width: 80, height: 24}, {width: 120, height: 40}} {
		name := strconv.Itoa(size.width) + "x" + strconv.Itoa(size.height)
		t.Run(name, func(t *testing.T) {
			root := readyDirectRootModel(t)
			root.splash = false
			updated, _ := updateDirect(root, tea.WindowSizeMsg{Width: size.width, Height: size.height})
			got := updated.View().Content + "\n"
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
			root := readyDirectRootModel(t)
			updated, _ := updateDirect(root, tea.WindowSizeMsg{Width: size.width, Height: size.height})
			view := updated.View()

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
	view := readyDirectRootModel(t).View()
	require.NotNil(t, view.BackgroundColor)

	red, green, blue, alpha := view.BackgroundColor.RGBA()
	assert.Equal(t, uint32(0), red)
	assert.Equal(t, uint32(0), green)
	assert.Equal(t, uint32(0), blue)
	assert.Equal(t, uint32(0xffff), alpha)
}

func readyDirectRootModel(t *testing.T) model {
	root := newDirectNodesModel(t, LaunchOptions{ContextName: "prod"}, []domain.NodeSnapshot{{
		ID: "cp-1", Name: "cp-1", Role: domain.NodeRoleControl, Kubernetes: domain.KubernetesUnknown,
	}})
	root.splash = false
	return root
}
