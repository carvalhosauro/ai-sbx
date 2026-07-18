package cli

import (
	"context"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/spf13/cobra"
)

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

// wireDeps reads persistent flags, builds deps, and injects them into ctx
// via PersistentPreRunE so every subcommand shares one driver + session.
func wireDeps(root *cobra.Command, d driver.Driver) {
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		jsonMode, _ := cmd.Flags().GetBool("json")
		sess, _ := cmd.Flags().GetString("session")
		ctx := context.WithValue(cmd.Context(), ctxKey{}, deps{
			drv:     d,
			session: resolveSession(sess),
			json:    jsonMode,
		})
		cmd.SetContext(ctx)
		return nil
	}
}

func newRootCmdWithDriver(d driver.Driver) *cobra.Command {
	root := NewRootCmd()
	wireDeps(root, d)
	return root
}

func Execute() int {
	root := newRootCmdWithDriver(driver.NewFake())
	if err := root.Execute(); err != nil {
		// CLIErrors originating inside a subcommand's RunE are already
		// rendered by renderErrorsOnReturn (env.go); only render here for
		// errors that never reach a subcommand's RunE (e.g. cobra's own
		// arg/flag validation), so the message isn't printed twice.
		if _, alreadyRendered := err.(CLIError); !alreadyRendered {
			jsonMode, _ := root.Flags().GetBool("json")
			writeError(root.OutOrStderr(), jsonMode, err)
		}
		return 1
	}
	return 0
}
