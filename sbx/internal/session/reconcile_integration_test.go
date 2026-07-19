//go:build integration

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func newTestPodman(t *testing.T) (*driver.Podman, string) {
	t.Helper()
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	}
	dir, err := os.MkdirTemp("", "sbx")
	require.NoError(t, err)
	p := driver.NewPodman(dir)
	t.Cleanup(func() {
		root := filepath.Join(dir, "storage")
		runroot := filepath.Join(dir, "runroot")
		_ = exec.Command("podman", "--root", root, "--runroot", runroot, "system", "reset", "--force").Run()
		_ = os.RemoveAll(dir)
	})
	return p, dir
}

func TestReconcileRemovesOrphanAfterKill(t *testing.T) {
	withStateHome(t)
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p, _ := newTestPodman(t)
	if err := p.Preflight(ctx); err != nil {
		t.Skipf("podman unavailable: %v", err)
	}

	e, err := p.Create(ctx, "reconcile-orphan", driver.EnvSpec{})
	require.NoError(t, err)

	list, err := p.List(ctx, "reconcile-orphan")
	require.NoError(t, err)
	require.Len(t, list, 1)

	// Engine has the env; registry is empty (never registered) → orphan.
	require.NoError(t, ReconcileSession(ctx, p, "reconcile-orphan"))

	list, err = p.List(ctx, "reconcile-orphan")
	require.NoError(t, err)
	require.Empty(t, list)
	_ = e
}
