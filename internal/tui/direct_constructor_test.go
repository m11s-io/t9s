package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/m11s-io/t9s/internal/application"
	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/m11s-io/t9s/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestNewWithCleanupOwnsSessionUntilQuitAndReturnsIdempotentCleanup(t *testing.T) {
	adapter := &testkit.FakeSession{}
	client := application.NewClient(application.Dependencies{
		ContextCatalog: &testkit.FakeContextCatalog{ListFunc: func(context.Context) ([]domain.ClusterContext, error) {
			return []domain.ClusterContext{{Name: "prod", Current: true}}, nil
		}},
		SessionFactory: &testkit.FakeSessionFactory{OpenFunc: func(context.Context, string) (ports.Session, error) {
			return adapter, nil
		}},
	})

	model, cleanup := NewWithCleanup(t.Context(), client, LaunchOptions{NodeFocus: "worker-1", WritesEnabled: true})
	require.NotNil(t, model)
	require.NotNil(t, cleanup)
	direct := model.(directModel)
	startup := direct.Init()().(tea.BatchMsg)
	updated, open := direct.Update(startup[0]())
	direct = updated.(directModel)
	require.NotNil(t, open)
	opened := open()
	updated, _ = direct.Update(opened)
	direct = updated.(directModel)
	require.NotNil(t, direct.session)

	updated, command := direct.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	require.NotNil(t, updated)
	if command != nil {
		_ = command()
	}
	_ = cleanup()
	_ = cleanup()
	require.Eventually(t, func() bool { return adapter.CloseCount() == 1 }, time.Second, time.Millisecond)
}
