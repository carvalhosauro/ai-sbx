package session

import (
	"context"
	"testing"
	"time"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestDestroyAllUnionsRegistryAndEngineAndIsIdempotent(t *testing.T) {
	withStateHome(t)
	ctx := context.Background()
	fake := driver.NewFake()
	// engine has one env
	e1, err := fake.Create(ctx, "s", driver.EnvSpec{Name: "engine-only"})
	require.NoError(t, err)
	// registry has a stale id not in engine + the engine one
	r, err := OpenRegistry("s")
	require.NoError(t, err)
	require.NoError(t, r.Add(EnvRecord{ID: e1.ID, Name: e1.Name, Namespace: e1.Namespace}))
	require.NoError(t, r.Add(EnvRecord{ID: "ghost", Name: "ghost", Namespace: "ghost"}))

	require.NoError(t, DestroyAll(ctx, fake, "s"))
	list, err := fake.List(ctx, "s")
	require.NoError(t, err)
	require.Empty(t, list)
	r2, _ := OpenRegistry("s")
	require.Empty(t, r2.List())

	// idempotent
	require.NoError(t, DestroyAll(ctx, fake, "s"))
}

func TestDestroyAllKeepsSessionAlive(t *testing.T) {
	withStateHome(t)
	r, _ := OpenRegistry("s")
	_ = r.SetSupervisor(9, time.Now().Add(time.Hour))
	require.NoError(t, DestroyAll(context.Background(), driver.NewFake(), "s"))
	r2, _ := OpenRegistry("s")
	require.True(t, r2.Alive)
	require.Equal(t, 9, r2.SupervisorPID)
}
