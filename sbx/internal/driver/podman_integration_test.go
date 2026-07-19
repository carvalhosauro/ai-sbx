// sbx/internal/driver/podman_integration_test.go
//go:build integration

package driver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/naming"
	"github.com/stretchr/testify/require"
)

func composeProviderAvailable() bool {
	if _, err := exec.LookPath("docker-compose"); err == nil {
		return true
	}
	if _, err := exec.LookPath("podman-compose"); err == nil {
		return true
	}
	if exec.Command("podman", "compose", "version").Run() == nil {
		return true
	}
	return false
}

func newTestPodman(t *testing.T) *Podman {
	t.Helper()
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	}
	// NOTE: deliberately NOT t.TempDir() here. t.TempDir() nests the dir
	// under "<TempDir>/<sanitized-test-name>/001", which on this machine
	// produces a path like ".../TestPodmanCreateDestroyIntegration.../001"
	// (~60+ chars once "/runroot" is appended). Rootless podman rejects
	// runroot paths over 50 chars ("the specified runroot is longer than
	// 50 characters") because it must fit a unix socket path underneath.
	// A short os.MkdirTemp prefix keeps the whole path well under that
	// limit; we manage its removal ourselves below instead of relying on
	// testing's built-in TempDir cleanup.
	dir, err := os.MkdirTemp("", "sbx")
	require.NoError(t, err)
	p := NewPodman(dir)
	t.Cleanup(func() {
		// Rootless overlay storage is owned by mapped subuids (100000+);
		// only a process inside podman's own user namespace (i.e. podman
		// itself) can remove it, so reset storage before RemoveAll.
		_ = exec.Command("podman", append(p.baseArgs(), "system", "reset", "--force")...).Run()
		_ = os.RemoveAll(dir)
	})
	return p
}

func TestPodmanCreateDestroyIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))

	e, err := p.Create(ctx, "itest01", EnvSpec{})
	require.NoError(t, err)
	require.Equal(t, "running", e.Status)

	st, err := p.Status(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, "running", st.Status)

	list, err := p.List(ctx, "itest01")
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, p.Destroy(ctx, e.ID))
	list, _ = p.List(ctx, "itest01")
	require.Len(t, list, 0)
}

func TestPodmanExecAndLogsIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))
	e, err := p.Create(ctx, "itest02", EnvSpec{})
	require.NoError(t, err)
	defer p.Destroy(ctx, e.ID)

	r, err := p.Exec(ctx, e.ID, []string{"sh", "-c", "echo hello && exit 3"})
	require.NoError(t, err)
	require.Contains(t, r.Stdout, "hello")
	require.Equal(t, 3, r.ExitCode)

	// Logs: the container runs `sleep infinity`, so output may legitimately
	// be empty. This exercises the Logs code path end-to-end, including
	// the --tail branch, rather than asserting on log content.
	_, err = p.Logs(ctx, e.ID, LogOpts{})
	require.NoError(t, err)

	_, err = p.Logs(ctx, e.ID, LogOpts{Tail: 10})
	require.NoError(t, err)
}

func TestPodmanMultiEnvPerSessionIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))

	e1, err := p.Create(ctx, "itest03", EnvSpec{})
	require.NoError(t, err)
	e2, err := p.Create(ctx, "itest03", EnvSpec{})
	require.NoError(t, err)

	require.NotEqual(t, e1.Name, e2.Name)
	require.Contains(t, e1.Name, "-001")
	require.Contains(t, e2.Name, "-002")

	list, err := p.List(ctx, "itest03")
	require.NoError(t, err)
	require.Len(t, list, 2)

	require.NoError(t, p.Destroy(ctx, e1.ID))
	require.NoError(t, p.Destroy(ctx, e2.ID))

	list, err = p.List(ctx, "itest03")
	require.NoError(t, err)
	require.Len(t, list, 0)
}

func inspectNetworks(t *testing.T, p *Podman, id string) map[string]any {
	t.Helper()
	out, _, err := p.run(context.Background(), append(p.baseArgs(), "inspect", id, "--format", "{{json .NetworkSettings.Networks}}"))
	require.NoError(t, err)
	m := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &m))
	return m
}

func TestPodmanNetworkPerNamespaceIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))

	e1, err := p.Create(ctx, "itnet01", EnvSpec{})
	require.NoError(t, err)
	defer p.Destroy(ctx, e1.ID)
	e2, err := p.Create(ctx, "itnet01", EnvSpec{})
	require.NoError(t, err)
	defer p.Destroy(ctx, e2.ID)

	// nomes de rede distintos e determinísticos por namespace
	require.NotEqual(t, e1.Network, e2.Network)
	require.Equal(t, naming.Network(e1.Namespace), e1.Network)

	// cada container está anexado SOMENTE à sua própria rede — não enxerga a do outro
	nets1 := inspectNetworks(t, p, e1.ID)
	_, has1 := nets1[e1.Network]
	_, hasOther := nets1[e2.Network]
	require.True(t, has1, "env1 deve estar na sua própria rede %s", e1.Network)
	require.False(t, hasOther, "env1 NÃO pode estar na rede do env2 %s", e2.Network)
}

func TestPodmanComposeLifecycleIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	if !composeProviderAvailable() {
		t.Skip("no podman compose provider (docker-compose/podman-compose) installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))

	wd, _ := os.Getwd()
	compose := filepath.Join(wd, "..", "..", "testdata", "compose.min.yml")

	e, err := p.Create(ctx, "itcomp1", EnvSpec{ComposePath: compose})
	require.NoError(t, err)
	require.Equal(t, e.Namespace, e.Project) // project == namespace
	defer p.Destroy(ctx, e.ID)

	st, err := p.Status(ctx, e.ID)
	require.NoError(t, err)
	require.Equal(t, "running", st.Status)

	// logs de um único serviço não devem falhar
	_, err = p.Logs(ctx, e.ID, LogOpts{Service: "web"})
	require.NoError(t, err)

	require.NoError(t, p.Destroy(ctx, e.ID))
	ids, _ := p.composeContainerIDs(ctx, e.ID)
	require.Len(t, ids, 0, "compose teardown deve remover todos os containers do projeto")
}

func portForService(env Env, svc string) int {
	for _, pm := range env.Ports {
		if svc == "" || pm.Service == svc {
			return pm.Host
		}
	}
	return 0
}

func TestPodmanSingleDynamicPortsDistinctIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))

	img := EnvSpec{Labels: map[string]string{"image": "docker.io/library/nginx:alpine"}} // nginx EXPOSE 80
	e1, err := p.Create(ctx, "itport", img)
	require.NoError(t, err)
	defer p.Destroy(ctx, e1.ID)
	e2, err := p.Create(ctx, "itport", img)
	require.NoError(t, err)
	defer p.Destroy(ctx, e2.ID)

	s1, err := p.Status(ctx, e1.ID)
	require.NoError(t, err)
	s2, err := p.Status(ctx, e2.ID)
	require.NoError(t, err)
	require.NotEmpty(t, s1.Ports, "nginx deve publicar 80 num host-port dinâmico")
	require.NotEmpty(t, s2.Ports)
	require.NotEqual(t, s1.Ports[0].Host, s2.Ports[0].Host, "dois envs → host-ports distintos")

	// status --json (List) mostra os dois envs com portas
	list, err := p.List(ctx, "itport")
	require.NoError(t, err)
	require.Len(t, list, 2)
	for _, e := range list {
		require.NotEmpty(t, e.Ports, "List deve incluir host-ports dinâmicos por env")
	}
}

func TestPodmanComposeDynamicPortsDistinctIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	if !composeProviderAvailable() {
		t.Skip("no podman compose provider installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	require.NoError(t, p.Preflight(ctx))
	wd, _ := os.Getwd()
	compose := filepath.Join(wd, "..", "..", "testdata", "compose.min.yml")

	e1, err := p.Create(ctx, "itcport", EnvSpec{ComposePath: compose})
	require.NoError(t, err)
	defer p.Destroy(ctx, e1.ID)
	e2, err := p.Create(ctx, "itcport", EnvSpec{ComposePath: compose})
	require.NoError(t, err)
	defer p.Destroy(ctx, e2.ID)

	s1, err := p.Status(ctx, e1.ID)
	require.NoError(t, err)
	s2, err := p.Status(ctx, e2.ID)
	require.NoError(t, err)
	require.NotZero(t, portForService(s1, "web"))
	require.NotEqual(t, portForService(s1, "web"), portForService(s2, "web"),
		"dois envs do MESMO compose devem receber host-ports diferentes")
}
