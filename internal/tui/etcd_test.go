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

func etcdTestState() application.EtcdState {
	return application.EtcdState{
		Status: application.Ready,
		Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, IsLeader: true, StatusKnown: true, DBSize: 2097152, RaftIndex: 4200},
			{Hostname: "cp-2", MemberID: 2, StatusKnown: false},
		}},
	}
}

func TestEtcdRenderSemanticColumns(t *testing.T) {
	rendered := renderEtcd(120, etcdTestState())
	lines := strings.Split(rendered, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t,
		[]string{"MEMBER", "ROLE", "DB", "SIZE", "RAFT", "INDEX", "ERRORS"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "cp-1")
	assert.Contains(t, ansi.Strip(lines[1]), "Leader")
	assert.Contains(t, ansi.Strip(lines[1]), "2.0 MiB")
	assert.Contains(t, ansi.Strip(lines[2]), "cp-2")
	assert.Contains(t, ansi.Strip(lines[2]), "?")
}

func TestEtcdRenderEmptyState(t *testing.T) {
	model := newEtcdModel(application.EtcdState{Status: application.Ready})
	rendered := model.viewSized(contentSize{Width: 80, Height: 10})
	assert.Equal(t, "No etcd members", rendered)
}

func TestEtcdFilterMatchesHostname(t *testing.T) {
	etcd := newEtcdModel(etcdTestState())
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('c'), keyPress('p'), keyPress('-'), keyPress('2'),
		{Code: tea.KeyEnter},
	} {
		etcd = etcd.update(message)
	}

	rendered := etcd.view(120)

	assert.Contains(t, rendered, "cp-2")
	assert.NotContains(t, rendered, "cp-1")
}
