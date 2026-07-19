// sbx/internal/cli/env_test.go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func run(t *testing.T, d driver.Driver, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmdWithDriver(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestEnvCreateThenStatusJSON(t *testing.T) {
	d := driver.NewFake()
	out, err := run(t, d, "--json", "--session", "sess1", "env", "create", "--from", "compose.yml")
	require.NoError(t, err)
	var created driver.Env
	require.NoError(t, json.Unmarshal([]byte(out), &created))
	require.NotEmpty(t, created.ID)

	out, err = run(t, d, "--json", "--session", "sess1", "env", "status")
	require.NoError(t, err)
	var list []driver.Env
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	require.Len(t, list, 1)
}

func TestEnvExecPassesCommandAfterDashDash(t *testing.T) {
	d := driver.NewFake()
	out, _ := run(t, d, "--session", "s", "env", "create")
	id := firstField(out)
	out, err := run(t, d, "--session", "s", "env", "exec", id, "--", "echo", "hi")
	require.NoError(t, err)
	require.Contains(t, out, "echo hi")
}

func TestEnvDestroyUnknownReturnsActionableError(t *testing.T) {
	out, err := run(t, driver.NewFake(), "--json", "env", "destroy", "nope")
	require.Error(t, err)
	require.Contains(t, out, "not_found")
	require.Contains(t, out, "hint")
}

func TestEnvDestroyAll(t *testing.T) {
	d := driver.NewFake()
	_, _ = run(t, d, "--session", "s", "env", "create")
	_, _ = run(t, d, "--session", "s", "env", "create")
	_, err := run(t, d, "--session", "s", "env", "destroy", "--all")
	require.NoError(t, err)
	out, _ := run(t, d, "--json", "--session", "s", "env", "status")
	require.Contains(t, out, "[]")
}

func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' || s[i] == '\n' || s[i] == ' ' {
			return s[:i]
		}
	}
	return s
}
