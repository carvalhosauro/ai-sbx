package session

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/gustavocarvalho/sbx/internal/driver"
)

// ReconcileSession removes engine containers for sessionID that are labeled
// sbx.managed=true but absent from the live registry.
func ReconcileSession(ctx context.Context, drv driver.Driver, sessionID string) error {
	reg, err := OpenRegistry(sessionID)
	if err != nil {
		return err
	}
	known := map[string]struct{}{}
	for _, e := range reg.List() {
		known[e.ID] = struct{}{}
	}
	listed, err := drv.List(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, e := range listed {
		if _, ok := known[e.ID]; !ok {
			_ = drv.Destroy(ctx, e.ID)
		}
	}
	return nil
}

// ReconcileStale walks $XDG_STATE_HOME/sbx/*, and for each sibling session
// whose registry is Alive but supervisor PID is dead OR deadline passed,
// runs DestroyAll + MarkEnded.
func ReconcileStale(ctx context.Context, drvFor func(sessionID string) (driver.Driver, error)) error {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	root := filepath.Join(base, "sbx")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	now := time.Now().Unix()
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		sid := ent.Name()
		reg, err := OpenRegistry(sid)
		if err != nil || !reg.Alive {
			continue
		}
		deadPID := !AlivePID(reg.SupervisorPID)
		pastDeadline := reg.DeadlineUnix > 0 && now > reg.DeadlineUnix
		if !deadPID && !pastDeadline {
			continue
		}
		drv, err := drvFor(sid)
		if err != nil {
			continue
		}
		_ = DestroyAll(ctx, drv, sid)
		_ = reg.MarkEnded()
	}
	return nil
}
