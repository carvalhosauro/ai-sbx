package cli

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "sbx",
		Short:         "Ephemeral, isolated container test environments for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().Bool("json", false, "machine-readable JSON output")
	root.PersistentFlags().String("session", "", "session id (defaults to $SBX_SESSION or a generated id)")
	root.AddCommand(newEnvCmd())
	return root
}

func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		return 1
	}
	return 0
}
