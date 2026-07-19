package session

import (
	"context"

	"github.com/gustavocarvalho/sbx/internal/driver"
)

// DestroyAll destroys every environment known to the registry and/or the
// engine for sessionID. Idempotent: a second call is a no-op success.
// Does NOT MarkEnded — the session/supervisor may still be alive.
func DestroyAll(ctx context.Context, drv driver.Driver, sessionID string) error {
	reg, err := OpenRegistry(sessionID)
	if err != nil {
		return err
	}
	ids := map[string]struct{}{}
	for _, e := range reg.List() {
		ids[e.ID] = struct{}{}
	}
	if listed, err := drv.List(ctx, sessionID); err == nil {
		for _, e := range listed {
			ids[e.ID] = struct{}{}
		}
	}
	for id := range ids {
		_ = drv.Destroy(ctx, id) // best-effort per id
		_ = reg.Remove(id)
	}
	// re-open/save cleared list (Remove already saved; ensure empty)
	reg2, err := OpenRegistry(sessionID)
	if err != nil {
		return err
	}
	for _, e := range reg2.List() {
		_ = reg2.Remove(e.ID)
	}
	return nil
}
