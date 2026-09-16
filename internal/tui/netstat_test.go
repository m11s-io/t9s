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

func netstatTestState() application.SocketState {
	return application.SocketState{
		Status: application.Ready,
		Node:   "cp-1",
		Value: domain.SocketSet{Sockets: []domain.SocketSnapshot{
			{Protocol: "tcp", State: "listen", LocalAddress: "10.0.0.1:6443", ProcessName: "kubelet", PID: 42},
			{Protocol: "udp", State: "established", LocalAddress: "10.0.0.2:53", RemoteAddress: "10.0.0.9:5000", ProcessName: "coredns", PID: 7},
		}},
	}
}

func renderNetstat(width int, state application.SocketState) string {
	return newNetstatModel(state).view(width)
}

func TestNetstatRenderSemanticColumns(t *testing.T) {
	rendered := renderNetstat(120, netstatTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"PROTO", "STATE", "LOCAL", "REMOTE", "PROCESS"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "tcp")
	assert.Contains(t, ansi.Strip(lines[1]), "listen")
	assert.Contains(t, ansi.Strip(lines[1]), "10.0.0.1:6443")
	assert.Contains(t, ansi.Strip(lines[1]), "kubelet")
}

func TestNetstatRenderEmptyState(t *testing.T) {
	model := newNetstatModel(application.SocketState{Status: application.Ready})
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No sockets", rendered)
}

func TestNetstatFilterMatchesLocalAddress(t *testing.T) {
	netstat := newNetstatModel(netstatTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('1'), keyPress('0'), keyPress('.'), keyPress('0'), keyPress('.'), keyPress('0'), keyPress('.'), keyPress('1'),
		{Code: tea.KeyEnter},
	} {
		netstat = netstat.update(message)
	}

	rendered := netstat.view(120)

	assert.Contains(t, rendered, "10.0.0.1:6443")
	assert.NotContains(t, rendered, "10.0.0.2:53")
}
