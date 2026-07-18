// sbx/internal/driver/podman_test.go
package driver

import (
	"path/filepath"
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
