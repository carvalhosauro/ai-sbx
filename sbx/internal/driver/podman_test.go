// sbx/internal/driver/podman_test.go
package driver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPodmanBaseArgsUsesOwnRoots(t *testing.T) {
	p := NewPodman("/tmp/sbx-state")
	args := p.baseArgs()
	require.Contains(t, args, "--root")
	require.Contains(t, args, filepath.Join("/tmp/sbx-state", "storage"))
	require.Contains(t, args, "--runroot")
}

func TestPodmanNeverReferencesHostDockerSocket(t *testing.T) {
	p := NewPodman("/tmp/sbx-state")
	for _, a := range p.baseArgs() {
		require.NotContains(t, a, "docker.sock")
		require.NotContains(t, a, "/var/run/docker")
	}
}

func TestPodmanCreateArgsShape(t *testing.T) {
	p := NewPodman("/s")
	args := p.createArgs(containerSpec{
		name:      "sbx-abc-001",
		session:   "abcdef",
		namespace: "sbx-abc-001",
		image:     "alpine:3",
		network:   "sbx-abc-001-net",
		extraNets: []string{"proxy-net"},
		envVars:   map[string]string{"HTTP_PROXY": "http://127.0.0.1:8080"},
	})
	joined := strings.Join(args, " ")
	require.Contains(t, joined, "run -d")
	require.Contains(t, joined, "--name sbx-abc-001")
	require.Contains(t, joined, "--label sbx.session=abcdef")
	require.Contains(t, joined, "--label sbx.namespace=sbx-abc-001")
	require.Contains(t, joined, "--label sbx.managed=true")
	require.Contains(t, joined, "--network sbx-abc-001-net")
	require.Contains(t, joined, "--network proxy-net")
	require.Contains(t, joined, "--env HTTP_PROXY=http://127.0.0.1:8080")
	require.Contains(t, joined, "alpine:3")
	// invariantes de segurança: roots próprios, nunca porta fixa aqui.
	require.Contains(t, joined, "--root")
	require.Contains(t, joined, "--runroot")
	require.NotContains(t, joined, "-p 0.0.0.0")
}

func TestPodmanNetworkArgsShape(t *testing.T) {
	p := NewPodman("/s")
	require.Contains(t, strings.Join(p.networkCreateArgs("sbx-abc-001-net"), " "), "network create sbx-abc-001-net")
	require.Contains(t, strings.Join(p.networkRemoveArgs("sbx-abc-001-net"), " "), "network rm sbx-abc-001-net")
	// rede também roda nos roots próprios (isolamento do engine do host)
	require.Contains(t, strings.Join(p.networkCreateArgs("x"), " "), "--root")
}

func TestPodmanCreateRejectsCompose(t *testing.T) {
	p := NewPodman(t.TempDir())
	_, err := p.Create(context.Background(), "s", EnvSpec{ComposePath: "compose.yml"})
	require.Error(t, err)
	de, ok := err.(DriverError)
	require.True(t, ok, "expected a DriverError, got %T", err)
	require.Equal(t, "compose_unsupported", de.Code)
}
