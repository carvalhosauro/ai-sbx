// sbx/internal/driver/podman_integration_test.go
//go:build integration

package driver

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestPodman(t *testing.T) *Podman {
	t.Helper()
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	}
	// NOTE: deliberately NOT t.TempDir() here. t.TempDir() nests the dir
	// under "<TempDir>/<sanitized-test-name>/001", which on this machine
	// produces a path like ".../TestPodmanCreateDestroyIntegration.../001"
	// (~60+ chars once "/runroot" is appended). Rootless podman rejects
	// runroot paths over 50 chars ("the specified runroot is longer than
	// 50 characters") because it must fit a unix socket path underneath.
	// A short os.MkdirTemp prefix keeps the whole path well under that
	// limit; we manage its removal ourselves below instead of relying on
	// testing's built-in TempDir cleanup.
	dir, err := os.MkdirTemp("", "sbx")
	require.NoError(t, err)
	p := NewPodman(dir)
	t.Cleanup(func() {
		// Rootless overlay storage is owned by mapped subuids (100000+);
		// only a process inside podman's own user namespace (i.e. podman
		// itself) can remove it, so reset storage before RemoveAll.
		_ = exec.Command("podman", append(p.baseArgs(), "system", "reset", "--force")...).Run()
		_ = os.RemoveAll(dir)
	})
	return p
}

func TestPodmanCreateDestroyIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))

	e, err := p.Create(ctx, "itest01", EnvSpec{})
	require.NoError(t, err)
	require.Equal(t, "running", e.Status)

	st, err := p.Status(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, "running", st.Status)

	list, err := p.List(ctx, "itest01")
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, p.Destroy(ctx, e.ID))
	list, _ = p.List(ctx, "itest01")
	require.Len(t, list, 0)
}
