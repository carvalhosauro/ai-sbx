// sbx/internal/driver/podman.go
package driver

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

type Podman struct {
	bin     string
	root    string
	runroot string
}

func NewPodman(stateDir string) *Podman {
	return &Podman{
		bin:     "podman",
		root:    filepath.Join(stateDir, "storage"),
		runroot: filepath.Join(stateDir, "runroot"),
	}
}

func (p *Podman) Name() string { return "podman" }

func (p *Podman) baseArgs() []string {
	return []string{"--root", p.root, "--runroot", p.runroot}
}

func (p *Podman) Preflight(ctx context.Context) error {
	if _, err := exec.LookPath(p.bin); err != nil {
		return CLIErrorLike("engine_missing",
			"podman not found on PATH",
			"install podman and ensure rootless is configured (/etc/subuid, /etc/subgid); on WSL2 set systemd=true in /etc/wsl.conf")
	}
	out, err := exec.CommandContext(ctx, p.bin, append(p.baseArgs(), "info", "--format", "{{.Host.Security.Rootless}}")...).CombinedOutput()
	if err != nil {
		return CLIErrorLike("engine_broken",
			"podman info failed: "+strings.TrimSpace(string(out)),
			"check rootless setup: `podman info`; ensure subuid/subgid ranges exist for your user")
	}
	if strings.TrimSpace(string(out)) != "true" {
		return CLIErrorLike("not_rootless",
			"podman is not running rootless",
			"this tool requires rootless podman for host isolation; do not run as root")
	}
	return nil
}
