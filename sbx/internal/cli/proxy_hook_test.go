package cli

import (
	"testing"

	"github.com/gustavocarvalho/sbx/internal/netpolicy"
	"github.com/gustavocarvalho/sbx/internal/session"
	"github.com/stretchr/testify/require"
)

func TestStartStopSessionProxyRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	addr, err := startSessionProxy("p1")
	require.NoError(t, err)
	require.Contains(t, addr, "host.containers.internal")
	r, _ := session.OpenRegistry("p1")
	// startSessionProxy itself may not write registry — supervise does; here we only check Stop
	require.NoError(t, stopSessionProxy("p1"))
	_ = r
}

func TestProxyEnvMergeHelper(t *testing.T) {
	env := netpolicy.ProxyEnv("http://host.containers.internal:9")
	require.Equal(t, "http://host.containers.internal:9", env["HTTP_PROXY"])
}
