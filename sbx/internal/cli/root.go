package cli

import (
	"context"
	"os"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/gustavocarvalho/sbx/internal/session"
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
	root.AddCommand(newEnvCmd(), newSessionCmd())
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

// newProductionRootCmd wires the real driver selection + preflight into
// PersistentPreRunE, where the parsed --session flag is available. This
// makes flag → env → default resolution the ONE source of truth for both
// the driver's storage root (session.StateDir) and deps.session — unlike
// the old Execute(), which resolved the session from the env var alone
// (before flag parsing) to build the driver, while deps.session used the
// --session flag: two sources of truth, and --session gave no storage
// isolation.
func newProductionRootCmd() *cobra.Command {
	root := NewRootCmd()
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		jsonMode, _ := cmd.Flags().GetBool("json")
		flagSessionValue, _ := cmd.Flags().GetString("session")
		sess := resolveSession(flagSessionValue)

		drv, err := driver.Select(os.Getenv("SBX_DRIVER"), session.StateDir(sess))
		if err != nil {
			return err
		}
		if pf, ok := drv.(interface{ Preflight(context.Context) error }); ok {
			if err := pf.Preflight(cmd.Context()); err != nil {
				return err
			}
		}

		ctx := context.WithValue(cmd.Context(), ctxKey{}, deps{
			drv:     drv,
			session: sess,
			json:    jsonMode,
		})
		cmd.SetContext(ctx)
		return nil
	}
	return root
}

func Execute() int {
	root := newProductionRootCmd()
	if err := root.Execute(); err != nil {
		// CLIErrors originating inside a subcommand's RunE are already
		// rendered by renderErrorsOnReturn (env.go); only render here for
		// errors that never reach a subcommand's RunE (e.g. cobra's own
		// arg/flag validation, or driver selection/preflight failures from
		// PersistentPreRunE above), so the message isn't printed twice.
		if _, alreadyRendered := err.(CLIError); !alreadyRendered {
			jsonMode, _ := root.Flags().GetBool("json")
			writeError(root.OutOrStderr(), jsonMode, err)
		}
		return 1
	}
	return 0
}
