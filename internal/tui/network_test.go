package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func networkTestState() application.NetworkState {
	return application.NetworkState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.NetworkSet{Links: []domain.LinkSnapshot{
			{Name: "eth0", Type: "ether", OperationalState: "up", MTU: 1500,
				Addresses: []domain.NetworkAddress{{Address: "10.0.0.5/24", Scope: "global"}}},
			{Name: "lo", Type: "loopback", OperationalState: "unknown", MTU: 65536},
		}},
	}
}

func renderNetwork(width int, state application.NetworkState) string {
	return newNetworkModel(state).view(width)
}

func TestNetworkRenderSemanticColumns(t *testing.T) {
	rendered := renderNetwork(120, networkTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"LINK", "TYPE", "STATE", "MTU", "ADDRESSES"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "eth0")
	assert.Contains(t, ansi.Strip(lines[1]), "ether")
	assert.Contains(t, ansi.Strip(lines[1]), "up")
	assert.Contains(t, ansi.Strip(lines[1]), "1500")
	assert.Contains(t, ansi.Strip(lines[1]), "10.0.0.5/24")
}

func TestNetworkRenderEmptyState(t *testing.T) {
	model := newNetworkModel(application.NetworkState{Status: application.Ready})
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No network interfaces", rendered)
}

func TestNetworkFilterMatchesLinkName(t *testing.T) {
	network := newNetworkModel(networkTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('e'), keyPress('t'), keyPress('h'),
		{Code: tea.KeyEnter},
	} {
		network = network.update(message)
	}

	rendered := network.view(120)

	assert.Contains(t, rendered, "eth0")
	assert.NotContains(t, rendered, "lo ")
}
