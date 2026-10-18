package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectEtcdFiltersAndPreservesSelectedMemberIDAcrossRefresh(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.width, model.height = 120, 30
	members := []domain.EtcdMemberSnapshot{{Hostname: "cp-1", MemberID: 1, StatusKnown: true}, {Hostname: "cp-2", MemberID: 2, StatusKnown: true}}
	model.cluster.etcd = directEtcdState{Status: directReady, Value: domain.EtcdSet{Members: members}}
	model.etcd = newEtcdModel().normalizeSelection(members, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	model, _ = updateDirect(model, keyPress('j'))
	assert.Equal(t, etcdMemberKey(members[1]), model.etcd.selectedKey)

	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune{'c', 'p', '-', '2'} {
		model, _ = updateDirect(model, keyPress(key))
	}
	assert.Contains(t, model.View().Content, "/cp-2")
	assert.Contains(t, model.View().Content, "cp-2")
	assert.NotContains(t, ansi.Strip(model.View().Content), "cp-1")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, model.View().Content, "/cp-2")

	request, _ := model.requests.begin("cluster-etcd", model.generation())
	reordered := []domain.EtcdMemberSnapshot{{Hostname: "cp-2", MemberID: 2, StatusKnown: true}, {Hostname: "cp-1", MemberID: 1, StatusKnown: true}}
	model, _ = updateDirect(model, directEtcdLoaded{request: request.stamp, etcd: domain.EtcdSet{Members: reordered}})
	assert.Equal(t, etcdMemberKey(reordered[0]), model.etcd.selectedKey)
	assert.Contains(t, model.View().Content, "cp-2")
}

func TestDirectEtcdPartialAndUnknownMembersRemainVisible(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	request, _ := model.requests.begin("cluster-etcd", model.generation())
	members := []domain.EtcdMemberSnapshot{{Hostname: "healthy", MemberID: 1, StatusKnown: true}, {Hostname: "unknown", MemberID: 2, Errors: []string{"status unavailable"}}}
	model, _ = updateDirect(model, directEtcdLoaded{request: request.stamp, etcd: domain.EtcdSet{Members: members}})
	assert.Equal(t, directPartial, model.cluster.etcd.Status)
	content := ansi.Strip(model.View().Content)
	assert.Contains(t, content, "ETCD PARTIAL")
	assert.Contains(t, content, "healthy")
	assert.Contains(t, content, "unknown")
	assert.Contains(t, content, "?")

	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune{'h', 'e', 'a', 'l', 't', 'h', 'y'} {
		model, _ = updateDirect(model, keyPress(key))
	}
	content = ansi.Strip(model.View().Content)
	assert.Contains(t, content, "ETCD PARTIAL")
	assert.Contains(t, content, "healthy")
	assert.NotContains(t, content, "unknown")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "/healthy")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model, _ = updateDirect(model, keyPress('/'))
	model, _ = updateDirect(model, keyPress('z'))
	assert.Contains(t, ansi.Strip(model.View().Content), "/z")
	assert.Contains(t, ansi.Strip(model.View().Content), "No etcd members match z")
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "/z")
}

func TestDirectEtcdAggregateErrorKeepsPartialMembers(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	request, _ := model.requests.begin("cluster-etcd", model.generation())
	members := []domain.EtcdMemberSnapshot{{Hostname: "usable", MemberID: 1, StatusKnown: true}}
	model, _ = updateDirect(model, directEtcdLoaded{request: request.stamp, etcd: domain.EtcdSet{Members: members}, err: errors.New("private transport detail")})
	assert.Equal(t, directPartial, model.cluster.etcd.Status)
	assert.NotContains(t, model.cluster.etcd.Err, "private transport detail")
	assert.Contains(t, ansi.Strip(model.View().Content), "usable")
}

func TestDirectEtcdRefreshUsesOnlyEtcdRequestSlotAndIgnoresStaleResults(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.session = &application.Session{}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	model, command := updateDirect(model, keyPress('r'))
	require.NotNil(t, command)
	assert.Contains(t, model.requests.slots, "cluster-etcd")
	assert.NotContains(t, model.requests.slots, "cluster-nodes-services")
	old, _ := model.requests.begin("cluster-etcd", model.generation())
	fresh, _ := model.requests.begin("cluster-etcd", model.generation())
	model.cluster.etcd = directEtcdState{Status: directReady, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "current", MemberID: 3}}}}
	model, _ = updateDirect(model, directEtcdLoaded{request: old.stamp, etcd: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "stale", MemberID: 1}}}})
	assert.Equal(t, "current", model.cluster.etcd.Value.Members[0].Hostname)
	model, _ = updateDirect(model, directEtcdLoaded{request: fresh.stamp, etcd: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "fresh", MemberID: 2}}}})
	assert.Equal(t, "fresh", model.cluster.etcd.Value.Members[0].Hostname)
}

func TestDirectEtcdAliasesRouteFromPalette(t *testing.T) {
	for _, alias := range []string{"etcd", "et"} {
		t.Run(alias, func(t *testing.T) {
			model := newDirectModel(context.Background(), nil, LaunchOptions{})
			model.splash = false
			model, _ = updateDirect(model, keyPress(':'))
			var command tea.Cmd
			for _, char := range alias {
				model, command = updateDirect(model, keyPress(char))
			}
			model, command = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
			require.NotNil(t, command)
			model, _ = updateDirect(model, command())
			assert.Equal(t, viewEtcd, model.views.top().Kind)
		})
	}
}

func TestDirectEtcdFilterResetOnContextChangeAndSelectionFallsForwardWhenMemberDisappears(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	members := []domain.EtcdMemberSnapshot{{Hostname: "cp-1", MemberID: 1}, {Hostname: "cp-2", MemberID: 2}}
	model.cluster.etcd = directEtcdState{Status: directReady, Value: domain.EtcdSet{Members: members}}
	model.etcd = model.etcd.normalizeSelection(members, 0)
	model.etcd = model.etcd.update(keyPress('j'), members)
	model.cluster.etcd.Value.Members = []domain.EtcdMemberSnapshot{members[0]}
	model.etcd = model.etcd.normalizeSelection(model.cluster.etcd.Value.Members, model.etcd.control.Cursor())
	assert.Equal(t, etcdMemberKey(members[0]), model.etcd.selectedKey)

	model.etcd = model.etcd.startFilter("cp", model.cluster.etcd.Value.Members)
	model.etcd.selectedKey = etcdMemberKey(members[0])
	model.beginSessionOpen("next")
	assert.Empty(t, model.etcd.filter)
	assert.False(t, model.etcd.filtering)
	assert.Empty(t, model.etcd.selectedKey)
}

func TestDirectEtcdPaletteWorksGloballyAndReadOnlyKeysAreInert(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{WritesEnabled: true})
	model.splash = false
	model.cluster.etcd = directEtcdState{Status: directReady, Value: domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{Hostname: "cp-1", MemberID: 1}}}}
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})
	assert.NotContains(t, model.View().Content, "Defragment")
	assert.NotContains(t, model.View().Content, "Snapshot")
	model, command := updateDirect(model, keyPress('d'))
	assert.Nil(t, command)
	model, _ = updateDirect(model, keyPress(':'))
	assert.True(t, model.palette.active)
	for _, key := range []tea.KeyPressMsg{keyPress('n'), keyPress('o'), keyPress('d'), keyPress('e'), keyPress('s'), {Code: tea.KeyEnter}} {
		model, command = updateDirect(model, key)
	}
	require.NotNil(t, command)
	model, _ = updateDirect(model, command())
	assert.Equal(t, viewNodes, model.views.top().Kind)
}

func TestDirectEtcdAcceptedFilterUsesVisibleFallbackForRefreshIdentity(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	model.splash = false
	members := []domain.EtcdMemberSnapshot{
		{Hostname: "cp-1", MemberID: 1, StatusKnown: true},
		{Hostname: "worker2", MemberID: 2, StatusKnown: true},
		{Hostname: "worker3", MemberID: 3, StatusKnown: true},
	}
	model.cluster.etcd = directEtcdState{Status: directReady, Value: domain.EtcdSet{Members: members}}
	model.etcd = model.etcd.normalizeSelection(members, 0)
	model.views = model.views.replaceRoot(viewFrame{Kind: viewEtcd, Label: "etcd"})

	model, _ = updateDirect(model, keyPress('/'))
	for _, key := range []rune("worker") {
		model, _ = updateDirect(model, keyPress(key))
	}
	model, _ = updateDirect(model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Contains(t, ansi.Strip(model.View().Content), "/worker")

	request, _ := model.requests.begin("cluster-etcd", model.generation())
	reordered := []domain.EtcdMemberSnapshot{members[2], members[1], members[0]}
	model, _ = updateDirect(model, directEtcdLoaded{request: request.stamp, etcd: domain.EtcdSet{Members: reordered}})
	assert.Equal(t, etcdMemberKey(members[1]), model.etcd.selectedKey)
	assert.Equal(t, 1, model.etcd.control.Cursor())
	selected, ok := model.etcd.selected(model.cluster.etcd.Value.Members)
	require.True(t, ok)
	assert.Equal(t, "worker2", selected.Hostname)
	assert.Contains(t, ansi.Strip(model.View().Content), "worker2")
}

func TestDirectEtcdHostnameFallbackIdentitySurvivesRefresh(t *testing.T) {
	model := newDirectModel(context.Background(), nil, LaunchOptions{})
	members := []domain.EtcdMemberSnapshot{{Hostname: "worker-a"}, {Hostname: "worker-b"}}
	model.cluster.etcd = directEtcdState{Status: directReady, Value: domain.EtcdSet{Members: members}}
	model.etcd = model.etcd.normalizeSelection(members, 0)
	model.etcd = model.etcd.update(keyPress('j'), members)
	assert.Equal(t, "hostname:worker-b", model.etcd.selectedKey)

	reordered := []domain.EtcdMemberSnapshot{members[1], members[0]}
	model.cluster.etcd.Value.Members = reordered
	model.etcd = model.etcd.normalizeSelection(reordered, model.etcd.control.Cursor())
	selected, ok := model.etcd.selected(reordered)
	require.True(t, ok)
	assert.Equal(t, "worker-b", selected.Hostname)
}
