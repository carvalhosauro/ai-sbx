// sbx/internal/driver/podman_test.go
package driver

import (
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
	args := p.createArgs("sbx-abc-001", "abcdef", "alpine:3")
	joined := strings.Join(args, " ")
	require.Contains(t, joined, "run -d")
	require.Contains(t, joined, "--name sbx-abc-001")
	require.Contains(t, joined, "--label sbx.session=abcdef")
	require.Contains(t, joined, "--label sbx.managed=true")
	require.Contains(t, joined, "alpine:3")
	// nunca publica porta fixa aqui; portas dinâmicas entram em M2
	require.NotContains(t, joined, "-p 0.0.0.0")
}
