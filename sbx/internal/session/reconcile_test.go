package session

import (
	"context"
	"testing"
	"time"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestReconcileSessionRemovesEngineOnlyOrphans(t *testing.T) {
	withStateHome(t)
	ctx := context.Background()
	fake := driver.NewFake()
	e, err := fake.Create(ctx, "s", driver.EnvSpec{Name: "orphan"})
	require.NoError(t, err)
	r, _ := OpenRegistry("s")
	// registry deliberately empty → engine entry is orphan
	require.NoError(t, ReconcileSession(ctx, fake, "s"))
	list, _ := fake.List(ctx, "s")
	require.Empty(t, list)
	_ = e // silence
	require.Empty(t, r.List())
}

func TestReconcileSessionKeepsRegistered(t *testing.T) {
	withStateHome(t)
	ctx := context.Background()
	fake := driver.NewFake()
	e, _ := fake.Create(ctx, "s", driver.EnvSpec{Name: "keep"})
	r, _ := OpenRegistry("s")
	_ = r.Add(EnvRecord{ID: e.ID, Name: e.Name, Namespace: e.Namespace})
	require.NoError(t, ReconcileSession(ctx, fake, "s"))
	list, _ := fake.List(ctx, "s")
	require.Len(t, list, 1)
}

func TestReconcileStaleEndsDeadSupervisorSessions(t *testing.T) {
	withStateHome(t)
	ctx := context.Background()
	r, _ := OpenRegistry("dead")
	_ = r.SetSupervisor(999999, time.Now().Add(-time.Minute)) // dead pid + past deadline
	fake := driver.NewFake()
	_, _ = fake.Create(ctx, "dead", driver.EnvSpec{Name: "x"})
	_ = r.Add(EnvRecord{ID: "x", Name: "x", Namespace: "x"})

	require.NoError(t, ReconcileStale(ctx, func(string) (driver.Driver, error) { return fake, nil }))
	r2, _ := OpenRegistry("dead")
	require.False(t, r2.Alive)
	list, _ := fake.List(ctx, "dead")
	require.Empty(t, list)
}
