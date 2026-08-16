package talos_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/m11s-io/t9s/internal/adapters/talos"
	"github.com/m11s-io/t9s/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionFactoryOpenUsesConfiguredTalosconfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-talosconfig")
	var factory ports.SessionFactory = talos.NewSessionFactory(path)

	session, err := factory.Open(context.Background(), "production")

	require.Error(t, err)
	assert.Nil(t, session)
	assert.ErrorContains(t, err, "open Talos session")
}
