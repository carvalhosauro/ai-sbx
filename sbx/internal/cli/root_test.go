package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootHelpListsEnvGroup(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), "env")
	require.Contains(t, out.String(), "Ephemeral test environments")
}

func TestRootHasJSONAndSessionFlags(t *testing.T) {
	cmd := NewRootCmd()
	require.NotNil(t, cmd.PersistentFlags().Lookup("json"))
	require.NotNil(t, cmd.PersistentFlags().Lookup("session"))
}
