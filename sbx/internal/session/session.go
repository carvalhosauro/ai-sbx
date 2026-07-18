// sbx/internal/session/session.go
package session

import (
	"os"
	"path/filepath"
)

// StateDir returns the per-session directory used to scope a driver's own
// storage (e.g. podman --root/--runroot), keeping it isolated from the
// host's default engine state.
func StateDir(sessionID string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "sbx", sessionID)
}
