package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/gustavocarvalho/sbx/internal/session"
	"github.com/stretchr/testify/require"
)

func TestSessionStartEndDestroysEnvs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fake := driver.NewFake()

	// Inject in-process supervisor starter for the test.
	orig := startSupervisor
	t.Cleanup(func() { startSupervisor = orig })
	startSupervisor = func(sessionID, timeout string) (int, error) {
		addr, err := startSessionProxy(sessionID)
		if err != nil {
			return 0, err
		}
		r, _ := session.OpenRegistry(sessionID)
		_ = r.SetProxy(addr)
		return 1, nil
	}

	root := newRootCmdWithDriver(fake)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--session", "demo", "session", "start", "--timeout", "30m"})
	require.NoError(t, root.Execute())

	_, err := fake.Create(context.Background(), "demo", driver.EnvSpec{Name: "sbx-demo-001"})
	require.NoError(t, err)
	r, _ := session.OpenRegistry("demo")
	_ = r.Add(session.EnvRecord{ID: "sbx-demo-001", Name: "sbx-demo-001", Namespace: "sbx-demo-001"})

	// end without live PID → DestroyAll + MarkEnded directly
	_ = r.SetSupervisor(0, time.Time{}) // force direct path
	root = newRootCmdWithDriver(fake)
	root.SetArgs([]string{"--session", "demo", "session", "end"})
	require.NoError(t, root.Execute())

	list, _ := fake.List(context.Background(), "demo")
	require.Empty(t, list)
	r2, _ := session.OpenRegistry("demo")
	require.False(t, r2.Alive)
}

func TestSessionHelpListsStartEnd(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"session", "--help"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), "start")
	require.Contains(t, out.String(), "end")
}
