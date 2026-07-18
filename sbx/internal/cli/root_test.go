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

func TestProductionRootUnknownDriverErrors(t *testing.T) {
	t.Setenv("SBX_DRIVER", "bogus")
	root := newProductionRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--json", "env", "status"})

	err := root.Execute()
	require.Error(t, err)

	// Mirror Execute()'s double-render guard: PersistentPreRunE errors
	// (like this one) are not rendered anywhere else, so the production
	// entry point renders them here, after flags are parsed.
	if _, alreadyRendered := err.(CLIError); !alreadyRendered {
		jsonMode, _ := root.Flags().GetBool("json")
		writeError(root.OutOrStderr(), jsonMode, err)
	}
	require.Contains(t, out.String(), "unknown_driver")
}
