// sbx/internal/driver/podman.go
package driver

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gustavocarvalho/sbx/internal/naming"
)

const defaultImage = "docker.io/library/alpine:3"

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
	cmd := exec.CommandContext(ctx, p.bin, append(p.baseArgs(), "info", "--format", "{{.Host.Security.Rootless}}")...)
	var errb strings.Builder
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		return CLIErrorLike("engine_broken",
			"podman info failed: "+strings.TrimSpace(errb.String()),
			"check rootless setup: `podman info`; ensure subuid/subgid ranges exist for your user")
	}
	if strings.TrimSpace(string(out)) != "true" {
		return CLIErrorLike("not_rootless",
			"podman is not running rootless",
			"this tool requires rootless podman for host isolation; do not run as root")
	}
	return nil
}

var (
	seqMu sync.Mutex
	seqBy = map[string]int{}
)

func envNameFor(session string) string {
	seqMu.Lock()
	defer seqMu.Unlock()
	seqBy[session]++
	return naming.EnvName(session, seqBy[session])
}

func (p *Podman) createArgs(name, session, image string) []string {
	args := append(p.baseArgs(), "run", "-d",
		"--name", name,
		"--label", "sbx.session="+session,
		"--label", "sbx.managed=true",
		image, "sleep", "infinity")
	return args
}

func (p *Podman) run(ctx context.Context, args []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, p.bin, args...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

func (p *Podman) Create(ctx context.Context, sessionID string, spec EnvSpec) (Env, error) {
	image := defaultImage
	if v := spec.Labels["image"]; v != "" {
		image = v
	}
	name := envNameFor(sessionID) // uses naming + a per-session seq (see Task 1.2)
	if _, errs, err := p.run(ctx, p.createArgs(name, sessionID, image)); err != nil {
		return Env{}, DriverError{Code: "create_failed", Message: strings.TrimSpace(errs), Hint: "verify the image name and that rootless podman can pull it"}
	}
	return Env{ID: name, Name: name, Namespace: name, Status: "running"}, nil
}

func (p *Podman) Destroy(ctx context.Context, id string) error {
	if _, errs, err := p.run(ctx, append(p.baseArgs(), "rm", "-f", id)); err != nil {
		return DriverError{Code: "destroy_failed", Message: strings.TrimSpace(errs), Hint: "id may not exist; run `sbx env status --json`"}
	}
	return nil
}

type psRow struct {
	Names  []string `json:"Names"`
	State  string   `json:"State"`
	Labels map[string]string
}

func (p *Podman) List(ctx context.Context, sessionID string) ([]Env, error) {
	out, errs, err := p.run(ctx, append(p.baseArgs(), "ps", "-a", "--filter", "label=sbx.session="+sessionID, "--format", "json"))
	if err != nil {
		return nil, DriverError{Code: "list_failed", Message: strings.TrimSpace(errs)}
	}
	var rows []psRow
	if strings.TrimSpace(out) != "" {
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			return nil, DriverError{Code: "parse_failed", Message: err.Error()}
		}
	}
	var envs []Env
	for _, r := range rows {
		name := ""
		if len(r.Names) > 0 {
			name = r.Names[0]
		}
		envs = append(envs, Env{ID: name, Name: name, Namespace: name, Status: r.State})
	}
	return envs, nil
}

func (p *Podman) Status(ctx context.Context, id string) (Env, error) {
	// M1: derive from List of all sessions is overkill; inspect by name.
	out, errs, err := p.run(ctx, append(p.baseArgs(), "inspect", id, "--format", "{{.State.Status}}"))
	if err != nil {
		return Env{}, DriverError{Code: "not_found", Message: strings.TrimSpace(errs), Hint: "run `sbx env status` to list ids"}
	}
	return Env{ID: id, Name: id, Namespace: id, Status: strings.TrimSpace(out)}, nil
}

// Exec/Logs implemented in Task 1.3.
