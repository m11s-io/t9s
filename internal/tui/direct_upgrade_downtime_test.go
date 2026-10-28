package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func singleCPUpgradeModel(t *testing.T, calls *atomic.Int32) model {
	t.Helper()
	client := application.NewClient(application.Dependencies{SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
		return &testkit.FakeSession{
			NodeReader: &testkit.FakeNodeReader{ListFunc: func(context.Context) (domain.NodeSet, error) {
				return domain.NodeSet{Nodes: []domain.NodeSnapshot{{ID: "cp-id", Name: "cp-1", Role: domain.NodeRoleControl, Version: "v1.14.4"}}}, nil
			}},
			EtcdReader: &testkit.FakeEtcdReader{ListFunc: func(context.Context, []string) (domain.EtcdSet, error) {
				return domain.EtcdSet{Members: []domain.EtcdMemberSnapshot{{MemberID: 1, Hostname: "cp-1", StatusKnown: true}}}, nil
			}},
			EtcdOps: &testkit.FakeEtcdOperations{SnapshotFunc: func(_ context.Context, node, path string) (domain.EtcdSnapshotResult, error) {
				require.Equal(t, int32(0), calls.Load())
				calls.Add(1)
				return domain.EtcdSnapshotResult{Node: node, Path: path, Size: 1024, SHA256: strings.Repeat("a", 64)}, nil
			}},
			NodeController: &testkit.FakeNodeController{UpgradeStreamFunc: func(context.Context, string, string) ports.UpgradeStream {
				require.Equal(t, int32(1), calls.Load())
				calls.Add(1)
				return &directUpgradeTestStream{results: make(chan ports.UpgradeResult), terminal: ports.UpgradeResult{Done: true, Outcome: ports.UpgradeOutcomeApplied}}
			}},
		}, nil
	}}})
	session, e := client.Open(context.Background(), "prod", application.Options{WritesEnabled: true})
	require.NoError(t, e)
	t.Cleanup(func() { session.Close() })
	m := directUpgradeModel(t, session)
	command := m.prepareDirectNodeMutation(application.NodeActionRequest{Kind: application.ActionUpgrade, Targets: []string{"cp-1"}, Image: "installer:v1.14.5"})
	m, _ = applyDirectCommand(t, m, command)
	return m
}
func TestDirectUpgradeDowntimeRequiresSeparateConfirmation(t *testing.T) {
	var calls atomic.Int32
	m := singleCPUpgradeModel(t, &calls)
	require.NotNil(t, m.mutation.prompt)
	assert.Contains(t, renderDirectMutationPrompt(m.mutation.prompt), "d allow downtime")
	m, cmd := updateDirect(m, keyPress('y'))
	assert.Nil(t, cmd)
	assert.Zero(t, calls.Load())
	m, cmd = updateDirect(m, keyPress('d'))
	require.NotNil(t, cmd)
	m, _ = applyDirectCommand(t, m, cmd)
	require.NotNil(t, m.mutation.prompt)
	a := m.mutation.prompt.assessment
	assert.Empty(t, a.Blocked)
	assert.NotEmpty(t, a.SnapshotPath)
	line := ansi.Strip(m.View().Content)
	assert.Contains(t, line, "Allow control-plane downtime")
	assert.Contains(t, line, "Kubernetes API")
	assert.Contains(t, line, a.SnapshotPath)
	assert.Zero(t, calls.Load(), "d prepares only")
	m, cmd = updateDirect(m, keyPress('y'))
	require.NotNil(t, cmd)
	assert.Contains(t, m.mutation.notice, "Taking etcd snapshot")
	assert.Contains(t, m.mutation.notice, a.SnapshotPath)
	for m.mutation.busy {
		m, cmd = updateDirect(m, cmd())
	}
	assert.Equal(t, int32(2), calls.Load())
	require.NotNil(t, m.mutation.upgradeResult)
	require.NotNil(t, m.mutation.upgradeResult.Snapshot)
	assert.Equal(t, a.SnapshotPath, m.mutation.upgradeResult.Snapshot.Path)
	assert.Contains(t, ansi.Strip(m.View().Content), a.SnapshotPath)
}
func TestDirectUpgradeDowntimeCancelDoesNotSnapshot(t *testing.T) {
	var calls atomic.Int32
	m := singleCPUpgradeModel(t, &calls)
	m, cmd := updateDirect(m, keyPress('d'))
	require.NotNil(t, cmd)
	m, _ = applyDirectCommand(t, m, cmd)
	m, cmd = updateDirect(m, tea.KeyPressMsg{Code: tea.KeyEsc})
	assert.Nil(t, cmd)
	assert.Nil(t, m.mutation.prompt)
	assert.Zero(t, calls.Load())
}
func TestDirectUpgradeDowntimeUnavailableForOtherBlocks(t *testing.T) {
	m := directUpgradeModel(t, directUpgradeSession(t, "", nil, nil))
	m.mutation.prompt = &directMutationPrompt{nodeKind: application.ActionUpgrade, session: m.session, assessment: application.Assessment{Targets: []string{"cp-1"}, Blocked: "refusing: no quorum"}}
	m, cmd := updateDirect(m, keyPress('d'))
	assert.Nil(t, cmd)
	assert.NotNil(t, m.mutation.prompt)
	assert.NotContains(t, renderDirectMutationPrompt(m.mutation.prompt), "d allow downtime")
}
func TestDirectUpgradeDowntimeSnapshotFailureNotice(t *testing.T) {
	assert.Contains(t, directUpgradeAdmissionNotice("cp-1", application.ErrUpgradeSnapshotFailed), "snapshot failed")
}

func TestDirectUpgradeDowntimeRejectsStalePrompt(t *testing.T) {
	for _, kind := range []string{"session", "generation", "operation", "read-only"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			m := singleCPUpgradeModel(t, &calls)
			switch kind {
			case "session":
				m.mutation.prompt.session = nil
			case "generation":
				m.mutation.prompt.generation++
			case "operation":
				m.mutation.prompt.operation++
			case "read-only":
				m.options.WritesEnabled = false
			}
			m, cmd := updateDirect(m, keyPress('d'))
			assert.Nil(t, cmd)
			assert.NotNil(t, m.mutation.prompt)
			assert.Zero(t, calls.Load())
		})
	}
}
