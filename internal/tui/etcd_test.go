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

func etcdTestState() directEtcdState {
	return directEtcdState{
		Status: directReady,
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
		[]string{"MEMBER", "ROLE", "DB", "SIZE", "RAFT", "INDEX", "ERRORS", "ALARMS"},
		strings.Fields(ansi.Strip(lines[0])),
	)
	assert.Contains(t, ansi.Strip(lines[1]), "cp-1")
	assert.Contains(t, ansi.Strip(lines[1]), "Leader")
	assert.Contains(t, ansi.Strip(lines[1]), "2.0 MiB")
	assert.Contains(t, ansi.Strip(lines[2]), "cp-2")
	assert.Contains(t, ansi.Strip(lines[2]), "?")
}

func TestEtcdRoleColumnStaysNearMember(t *testing.T) {
	header := ansi.Strip(strings.Split(renderEtcd(120, etcdTestState()), "\n")[0])
	member := strings.Index(header, "MEMBER")
	role := strings.Index(header, "ROLE")

	assert.GreaterOrEqual(t, member, 0)
	assert.Less(t, role, 35, "the empty part of MEMBER should not push ROLE far from member names")
	assert.Less(t, role-member, 20)
}

func TestEtcdRenderShowsAlarms(t *testing.T) {
	state := directEtcdState{
		Status: directReady,
		Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{
			{Hostname: "cp-1", MemberID: 1, StatusKnown: true, Alarms: []string{"NOSPACE"}},
		}},
	}

	rendered := ansi.Strip(renderEtcd(120, state))

	assert.Contains(t, rendered, "ALARMS")
	assert.Contains(t, rendered, "NOSPACE")
}

func TestEtcdRenderEmptyState(t *testing.T) {
	model := newEtcdModel()
	rendered := model.viewSized(contentSize{Width: 80, Height: 10}, directEtcdState{Status: directReady})
	assert.Equal(t, "No etcd members", rendered)
}

func TestEtcdFilterMatchesHostname(t *testing.T) {
	state := etcdTestState()
	etcd := newEtcdModel().normalizeSelection(state.Value.Members, 0)
	for _, message := range []tea.KeyPressMsg{
		keyPress('/'), keyPress('c'), keyPress('p'), keyPress('-'), keyPress('2'),
		{Code: tea.KeyEnter},
	} {
		etcd = etcd.update(message, state.Value.Members)
	}

	rendered := etcd.view(120, state.Value.Members)

	assert.Contains(t, rendered, "cp-2")
	assert.NotContains(t, rendered, "cp-1")
}
