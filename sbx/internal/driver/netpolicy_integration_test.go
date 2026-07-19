// sbx/internal/driver/netpolicy_integration_test.go
//go:build integration

package driver

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/netpolicy"
	"github.com/stretchr/testify/require"
)

const curlImage = "docker.io/curlimages/curl:8.11.1"

// curlOnNet runs an ephemeral curl container on the given (internal) network,
// mirroring exactly how the driver wires a workload container: same --network,
// same host-gateway alias. Returns curl's exit code + combined output.
func curlOnNet(t *testing.T, p *Podman, network string, env []string, curlArgs ...string) (int, string) {
	t.Helper()
	args := append(p.baseArgs(), "run", "--rm",
		"--network", network,
		"--add-host", "host.containers.internal:host-gateway")
	for _, e := range env {
		args = append(args, "--env", e)
	}
	args = append(args, curlImage)
	args = append(args, curlArgs...)
	cmd := exec.Command("podman", args...)
	out, err := cmd.CombinedOutput()
	code := 0
	switch {
	case cmd.ProcessState != nil:
		code = cmd.ProcessState.ExitCode()
	case err != nil:
		code = -1
	}
	return code, strings.TrimSpace(string(out))
}

func TestNetpolicyEgressAllowlistIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := newTestPodman(t)
	if err := p.Preflight(ctx); err != nil {
		t.Skipf("podman preflight failed: %v", err)
	}

	// deny-by-default: SÓ api.anthropic.com é alcançável.
	proxy, err := netpolicy.StartProxy([]string{"api.anthropic.com"})
	require.NoError(t, err)
	defer proxy.Stop()

	// env workload cabeado ao proxy (env vars) e na rede interna (M2/3.3).
	e, err := p.Create(ctx, "itnp01", EnvSpec{EnvVars: netpolicy.ProxyEnv(proxy.Addr())})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "mkdir /run/runc") ||
			(strings.Contains(msg, "permission denied") && strings.Contains(msg, "runc")) {
			t.Skipf("podman engine/runtime unavailable: %v", err)
		}
		require.NoError(t, err)
	}
	defer p.Destroy(ctx, e.ID)
	require.NotEmpty(t, e.Network)

	proxyEnv := "HTTPS_PROXY=" + proxy.Addr()

	// 1) ALLOWED: api.anthropic.com alcançável ATRAVÉS do proxy (túnel ok → exit 0).
	code, out := curlOnNet(t, p, e.Network, []string{proxyEnv},
		"-sS", "--max-time", "20", "-o", "/dev/null", "https://api.anthropic.com/")
	require.Equal(t, 0, code, "host permitido deve ser alcançável via proxy; saída: %s", out)

	// 2) FAKE PROD: negado pelo proxy (deny-by-default → CONNECT recusado → exit != 0).
	code, out = curlOnNet(t, p, e.Network, []string{proxyEnv},
		"-sS", "--max-time", "20", "-o", "/dev/null", "https://prod.internal.example.com/")
	require.NotEqual(t, 0, code, "host de prod falso deve ser recusado pelo proxy; saída: %s", out)

	// 3) CREDENCIAL VAZADA: mesmo carregando um segredo, o proxy nega o destino
	//    de prod ANTES de conectar → o segredo não sai.
	code, out = curlOnNet(t, p, e.Network, []string{proxyEnv},
		"-sS", "--max-time", "20", "-H", "Authorization: Bearer SUPER-SECRET",
		"-o", "/dev/null", "https://prod.internal.example.com/")
	require.NotEqual(t, 0, code, "credencial vazada não pode alcançar prod; saída: %s", out)

	// 4) EGRESSO DIRETO: sem proxy nas env vars, conexão direta a um IP público
	//    na rede interna não tem rota → falha (prova o bloqueio no nível de rede).
	code, out = curlOnNet(t, p, e.Network, nil,
		"-sS", "--max-time", "8", "-o", "/dev/null", "https://1.1.1.1/")
	require.NotEqual(t, 0, code, "rede interna NÃO pode ter rota default (egresso direto vazou!); saída: %s", out)
}
