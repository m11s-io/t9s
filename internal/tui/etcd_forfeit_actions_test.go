package tui

import (
	"context"
	"testing"

	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEtcdForfeitKeyWithWritesDisabledIsInert(t *testing.T) {
	root := etcdTestModel(t, false, &testkit.FakeEtcdOperations{})

	updated, cmd := root.Update(keyPress('F'))

	assert.Nil(t, cmd)
	assert.Nil(t, updated.(model).application.PendingEtcdAction)
}

func TestEtcdForfeitKeyOpensConfirmPrompt(t *testing.T) {
	root := etcdTestModel(t, true, &testkit.FakeEtcdOperations{})

	updated, _ := root.Update(keyPress('F'))
	rootModel := updated.(model)

	require.NotNil(t, rootModel.application.PendingEtcdAction)
	assert.Equal(t, application.EtcdActionForfeitLeadership, rootModel.application.PendingEtcdAction.Kind)
	assert.Contains(t, rootModel.activePrompt(), "Forfeit")
	assert.Contains(t, rootModel.activePrompt(), "(y/n)")
}

func TestEtcdForfeitKeyConfirmedCallsOperations(t *testing.T) {
	var forfeited []string
	operations := &testkit.FakeEtcdOperations{
		ForfeitLeadershipFunc: func(_ context.Context, node string) error {
			forfeited = append(forfeited, node)
			return nil
		},
	}
	root := etcdTestModel(t, true, operations)
	updated, _ := root.Update(keyPress('F'))
	rootModel := updated.(model)
	require.NotNil(t, rootModel.application.PendingEtcdAction)

	updated, cmd := rootModel.Update(keyPress('y'))
	require.NotNil(t, cmd)
	cmd()

	assert.Nil(t, updated.(model).application.PendingEtcdAction)
	assert.Equal(t, []string{"cp-1"}, forfeited)
}

func TestEtcdActionHintsShowForfeitWhenWritesEnabled(t *testing.T) {
	assert.NotContains(t, hintKeys(actionHints(viewEtcd, false)), "F")
	assert.Contains(t, hintKeys(actionHints(viewEtcd, true)), "F")
}
