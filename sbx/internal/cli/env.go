package cli

import "github.com/spf13/cobra"

func newEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Ephemeral test environments (create, exec, logs, status, destroy)",
	}
}
