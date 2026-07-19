// sbx/internal/cli/env.go
package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/gustavocarvalho/sbx/internal/naming"
	"github.com/gustavocarvalho/sbx/internal/session"
	"github.com/spf13/cobra"
)

type ctxKey struct{}

type deps struct {
	drv     driver.Driver
	session string
	json    bool
}

func depsFrom(cmd *cobra.Command) deps {
	if d, ok := cmd.Context().Value(ctxKey{}).(deps); ok {
		return d
	}
	return deps{}
}

func printerFor(cmd *cobra.Command, d deps) Printer {
	return Printer{W: cmd.OutOrStdout(), JSON: d.json}
}

func resolveSession(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if s := os.Getenv("SBX_SESSION"); s != "" {
		return s
	}
	return "default"
}

func newEnvCmd() *cobra.Command {
	env := &cobra.Command{
		Use:   "env",
		Short: "Ephemeral test environments (create, exec, logs, status, destroy)",
	}
	subs := []*cobra.Command{newCreateCmd(), newExecCmd(), newLogsCmd(), newStatusCmd(), newDestroyCmd()}
	for _, sub := range subs {
		renderErrorsOnReturn(sub)
	}
	env.AddCommand(subs...)
	return env
}

// renderErrorsOnReturn wraps a subcommand's RunE so an actionable CLIError is
// written to the command's output the moment it occurs. cobra's root command
// runs with SilenceErrors so its own default error printing never fires, and
// even when unsilenced it only prints err.Error() (the bare Message) — never
// Code or Hint. Wiring writeError here, rather than only in the top-level
// Execute(), means both production and direct cobra Command.Execute() callers
// (as used by the CLI contract tests) observe a rendered, actionable error.
func renderErrorsOnReturn(c *cobra.Command) {
	inner := c.RunE
	if inner == nil {
		return
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		err := inner(cmd, args)
		if err != nil {
			writeError(cmd.OutOrStdout(), depsFrom(cmd).json, err)
		}
		return err
	}
}

// maxEnvs is the per-session cap on live environments. Enforced in the CLI
// (create path), not the driver, so the agent-facing contract never leaks the
// backend. Default 8; override with SBX_MAX_ENVS.
func maxEnvs() int {
	if v := os.Getenv("SBX_MAX_ENVS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 8
}

func newCreateCmd() *cobra.Command {
	var from string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new ephemeral environment (optionally from a compose file)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			_ = session.ReconcileSession(cmd.Context(), d.drv, d.session)
			existing, err := d.drv.List(cmd.Context(), d.session)
			if err != nil {
				return CLIError{Code: "list_failed", Message: err.Error(), Hint: "the engine may be unavailable; run `sbx env status`"}
			}
			if limit := maxEnvs(); len(existing) >= limit {
				return CLIError{
					Code:    "limit_exceeded",
					Message: fmt.Sprintf("session already has %d environment(s) (limit %d)", len(existing), limit),
					Hint:    "destroy one with `sbx env destroy <id>` (or `--all`), or raise SBX_MAX_ENVS",
				}
			}
			reg, err := session.OpenRegistry(d.session)
			if err != nil {
				return err
			}
			seq, err := reg.NextSeq()
			if err != nil {
				return err
			}
			name := naming.EnvName(d.session, seq)
			spec := driver.EnvSpec{ComposePath: from, Name: name}
			e, err := d.drv.Create(cmd.Context(), d.session, spec)
			if err != nil {
				return CLIError{Code: "create_failed", Message: err.Error(), Hint: "check the compose file path and that the engine is available"}
			}
			_ = reg.Add(session.EnvRecord{
				ID: e.ID, Name: e.Name, Namespace: e.Namespace,
				Network: e.Network, Project: e.Project,
			})
			return printerFor(cmd, d).Env(e)
		},
	}
	c.Flags().StringVar(&from, "from", "", "path to a compose.yml to bring the environment up from")
	return c
}

func newExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "exec <id> -- <cmd>...",
		Short:              "Run a command inside the environment",
		Args:               cobra.MinimumNArgs(2),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			id, rest := args[0], args[1:]
			r, err := d.drv.Exec(cmd.Context(), id, rest)
			if err != nil {
				return CLIError{Code: "not_found", Message: err.Error(), Hint: "run `sbx env status --json` to list ids"}
			}
			if err := printerFor(cmd, d).Exec(r); err != nil {
				return err
			}
			if r.ExitCode != 0 {
				return CLIError{Code: "exec_nonzero", Message: "command exited non-zero"}
			}
			return nil
		},
	}
}

func newLogsCmd() *cobra.Command {
	var service string
	var tail int
	c := &cobra.Command{
		Use:   "logs <id>",
		Short: "Show logs for the environment (optionally a single service)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			s, err := d.drv.Logs(cmd.Context(), args[0], driver.LogOpts{Service: service, Tail: tail})
			if err != nil {
				return CLIError{Code: "not_found", Message: err.Error(), Hint: "run `sbx env status --json` to list ids"}
			}
			return printerFor(cmd, d).Raw(s)
		},
	}
	c.Flags().StringVar(&service, "service", "", "limit logs to one service")
	c.Flags().IntVar(&tail, "tail", 0, "show only the last N lines (0 = all)")
	return c
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [id]",
		Short: "Show one environment or list all in this session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			p := printerFor(cmd, d)
			if len(args) == 1 {
				e, err := d.drv.Status(cmd.Context(), args[0])
				if err != nil {
					return CLIError{Code: "not_found", Message: err.Error(), Hint: "omit the id to list all environments"}
				}
				return p.Env(e)
			}
			list, err := d.drv.List(cmd.Context(), d.session)
			if err != nil {
				return CLIError{Code: "list_failed", Message: err.Error()}
			}
			return p.Envs(list)
		},
	}
}

func newDestroyCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "destroy <id> | --all",
		Short: "Destroy one environment or all in this session",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			if all {
				if err := session.DestroyAll(cmd.Context(), d.drv, d.session); err != nil {
					return err
				}
				return nil
			}
			if len(args) != 1 {
				return CLIError{Code: "usage", Message: "provide an environment id or --all", Hint: "e.g. `sbx env destroy env001` or `sbx env destroy --all`"}
			}
			if err := d.drv.Destroy(cmd.Context(), args[0]); err != nil {
				return CLIError{Code: "not_found", Message: err.Error(), Hint: "run `sbx env status --json` to list ids"}
			}
			reg, err := session.OpenRegistry(d.session)
			if err != nil {
				return err
			}
			_ = reg.Remove(args[0])
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "destroy every environment in the session")
	return c
}
