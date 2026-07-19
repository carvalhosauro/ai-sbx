package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/gustavocarvalho/sbx/internal/session"
	"github.com/spf13/cobra"
)

func init() {
	reconcileOnStart = func(ctx context.Context, d deps) error {
		_ = session.ReconcileStale(ctx, func(sid string) (driver.Driver, error) {
			return driver.Select(os.Getenv("SBX_DRIVER"), session.StateDir(sid))
		})
		return session.ReconcileSession(ctx, d.drv, d.session)
	}
}

// startSupervisor forks `sbx session supervise`. Overridable in tests.
var startSupervisor = defaultStartSupervisor

func defaultStartSupervisor(sessionID, timeout string) (int, error) {
	bin, err := os.Executable()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(bin, "--session", sessionID, "session", "supervise", "--timeout", timeout)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

func newSessionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "session",
		Short: "Session lifecycle (start/end); auto-destroys environments on timeout or end",
	}
	subs := []*cobra.Command{newSessionStartCmd(), newSessionEndCmd(), newSessionSuperviseCmd()}
	for _, s := range subs {
		renderErrorsOnReturn(s)
	}
	c.AddCommand(subs...)
	return c
}

func newSessionStartCmd() *cobra.Command {
	var timeoutStr string
	c := &cobra.Command{
		Use:   "start",
		Short: "Start a supervised session (spawns background supervise)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			reg, err := session.OpenRegistry(d.session)
			if err != nil {
				return err
			}
			if reg.Alive && session.AlivePID(reg.SupervisorPID) {
				return CLIError{
					Code:    "session_active",
					Message: fmt.Sprintf("session %q already has a live supervisor (pid %d)", d.session, reg.SupervisorPID),
					Hint:    "run: sbx --session " + d.session + " session end",
				}
			}
			// Reconcile before start (Task 4.4 wires the call; until then optional no-op).
			if reconcileOnStart != nil {
				_ = reconcileOnStart(cmd.Context(), d)
			}
			pid, err := startSupervisor(d.session, timeoutStr)
			if err != nil {
				return err
			}
			var deadline time.Time
			if timeoutStr != "" && timeoutStr != "0" {
				dur, err := time.ParseDuration(timeoutStr)
				if err != nil {
					return CLIError{Code: "bad_timeout", Message: err.Error(), Hint: "use Go duration, e.g. 30m, 1h"}
				}
				if dur > 0 {
					deadline = time.Now().Add(dur)
				}
			}
			if err := reg.SetSupervisor(pid, deadline); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "session %s started (supervisor pid %d, timeout %s)\n", d.session, pid, timeoutStr)
			return nil
		},
	}
	c.Flags().StringVar(&timeoutStr, "timeout", "30m", "session lifetime (0 = until session end / signal)")
	return c
}

func newSessionEndCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "end",
		Short: "End the session: signal supervisor or destroy-all + mark ended",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			reg, err := session.OpenRegistry(d.session)
			if err != nil {
				return err
			}
			if session.AlivePID(reg.SupervisorPID) {
				p, err := os.FindProcess(reg.SupervisorPID)
				if err == nil {
					_ = p.Signal(syscall.SIGTERM)
					// give supervise a moment; fall through to local cleanup as belt-and-suspenders
					time.Sleep(200 * time.Millisecond)
				}
			}
			if err := session.DestroyAll(cmd.Context(), d.drv, d.session); err != nil {
				return err
			}
			// stop proxy if this process holds it — Task 4.5; until then MarkEnded is enough
			if stopSessionProxy != nil {
				_ = stopSessionProxy(d.session)
			}
			reg2, err := session.OpenRegistry(d.session)
			if err != nil {
				return err
			}
			return reg2.MarkEnded()
		},
	}
}

func newSessionSuperviseCmd() *cobra.Command {
	var timeoutStr string
	c := &cobra.Command{
		Use:    "supervise",
		Short:  "Internal: block until timeout/signal, then destroy-all",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			reg, err := session.OpenRegistry(d.session)
			if err != nil {
				return err
			}
			// Proxy start happens here (Task 4.5 via hook).
			if startSessionProxy != nil {
				addr, err := startSessionProxy(d.session)
				if err != nil {
					return err
				}
				_ = reg.SetProxy(addr)
			}
			var deadline time.Time
			if timeoutStr != "" && timeoutStr != "0" {
				dur, err := time.ParseDuration(timeoutStr)
				if err != nil {
					return err
				}
				if dur > 0 {
					deadline = time.Now().Add(dur)
					_ = reg.SetSupervisor(os.Getpid(), deadline)
				} else {
					_ = reg.SetSupervisor(os.Getpid(), time.Time{})
				}
			} else {
				_ = reg.SetSupervisor(os.Getpid(), time.Time{})
			}
			return session.Wait(cmd.Context(), deadline, func(ctx context.Context) error {
				_ = session.DestroyAll(ctx, d.drv, d.session)
				if stopSessionProxy != nil {
					_ = stopSessionProxy(d.session)
				}
				reg2, err := session.OpenRegistry(d.session)
				if err != nil {
					return err
				}
				return reg2.MarkEnded()
			})
		},
	}
	c.Flags().StringVar(&timeoutStr, "timeout", "30m", "session lifetime")
	return c
}

// Hooks filled in Tasks 4.4 / 4.5 (nil-safe).
var reconcileOnStart func(ctx context.Context, d deps) error
var startSessionProxy func(sessionID string) (addr string, err error)
var stopSessionProxy func(sessionID string) error
