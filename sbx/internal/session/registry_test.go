package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func withStateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

func TestOpenRegistryCreatesFile(t *testing.T) {
	withStateHome(t)
	r, err := OpenRegistry("demo")
	require.NoError(t, err)
	require.Equal(t, "demo", r.SessionID)
	require.True(t, r.Alive)
	require.FileExists(t, r.Path())
	require.Equal(t, filepath.Join(StateDir("demo"), "registry.json"), r.Path())
}

func TestNextSeqMonotonicAndPersists(t *testing.T) {
	withStateHome(t)
	r, err := OpenRegistry("s")
	require.NoError(t, err)
	n1, err := r.NextSeq()
	require.NoError(t, err)
	n2, err := r.NextSeq()
	require.NoError(t, err)
	require.Equal(t, 1, n1)
	require.Equal(t, 2, n2)

	r2, err := OpenRegistry("s")
	require.NoError(t, err)
	require.Equal(t, 2, r2.Seq)
	n3, err := r2.NextSeq()
	require.NoError(t, err)
	require.Equal(t, 3, n3)
}

func TestAddRemoveListRoundTrip(t *testing.T) {
	withStateHome(t)
	r, _ := OpenRegistry("s")
	require.NoError(t, r.Add(EnvRecord{ID: "a", Name: "sbx-s-001", Namespace: "sbx-s-001"}))
	require.NoError(t, r.Add(EnvRecord{ID: "b", Name: "sbx-s-002", Namespace: "sbx-s-002", Network: "sbx-s-002-net"}))
	require.Len(t, r.List(), 2)

	r2, _ := OpenRegistry("s")
	require.Len(t, r2.List(), 2)
	require.NoError(t, r2.Remove("a"))
	require.NoError(t, r2.Remove("a")) // idempotent
	require.Len(t, r2.List(), 1)
	require.Equal(t, "b", r2.List()[0].ID)
}

func TestSetSupervisorAndProxyPersist(t *testing.T) {
	withStateHome(t)
	r, _ := OpenRegistry("s")
	deadline := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	require.NoError(t, r.SetSupervisor(4242, deadline))
	require.NoError(t, r.SetProxy("http://host.containers.internal:1234"))

	r2, _ := OpenRegistry("s")
	require.Equal(t, 4242, r2.SupervisorPID)
	require.Equal(t, deadline.Unix(), r2.DeadlineUnix)
	require.Equal(t, "http://host.containers.internal:1234", r2.ProxyAddr)
}

func TestMarkEndedClearsRuntimeFields(t *testing.T) {
	withStateHome(t)
	r, _ := OpenRegistry("s")
	_ = r.Add(EnvRecord{ID: "a", Name: "n", Namespace: "n"})
	_ = r.SetSupervisor(1, time.Now().Add(time.Hour))
	_ = r.SetProxy("http://x:1")
	require.NoError(t, r.MarkEnded())

	r2, _ := OpenRegistry("s")
	require.False(t, r2.Alive)
	require.Zero(t, r2.SupervisorPID)
	require.Zero(t, r2.DeadlineUnix)
	require.Empty(t, r2.ProxyAddr)
	require.Empty(t, r2.List())
}

func TestOpenRegistryRejectsCorruptJSON(t *testing.T) {
	withStateHome(t)
	dir := StateDir("bad")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.json"), []byte("{nope"), 0o644))
	_, err := OpenRegistry("bad")
	require.Error(t, err)
	require.Contains(t, err.Error(), "registry")
}

func TestSaveIsAtomicReadableJSON(t *testing.T) {
	withStateHome(t)
	r, _ := OpenRegistry("s")
	_ = r.Add(EnvRecord{ID: "a", Name: "n", Namespace: "n"})
	b, err := os.ReadFile(r.Path())
	require.NoError(t, err)
	var decoded Registry
	require.NoError(t, json.Unmarshal(b, &decoded))
	require.Equal(t, "s", decoded.SessionID)
	require.Len(t, decoded.Envs, 1)
}
