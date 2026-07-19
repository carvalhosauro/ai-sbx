// sbx/internal/driver/podman.go
package driver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gustavocarvalho/sbx/internal/naming"
)

const defaultImage = "docker.io/library/alpine:3"

const (
	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
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

type containerSpec struct {
	name       string
	session    string
	namespace  string
	image      string
	network    string
	extraNets  []string
	envVars    map[string]string
	publishAll bool
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (p *Podman) createArgs(cs containerSpec) []string {
	args := append(p.baseArgs(), "run", "-d",
		"--name", cs.name,
		"--label", "sbx.session="+cs.session,
		"--label", "sbx.namespace="+cs.namespace,
		"--label", "sbx.managed=true")
	if cs.network != "" {
		args = append(args, "--network", cs.network)
	}
	for _, n := range cs.extraNets {
		args = append(args, "--network", n)
	}
	for _, k := range sortedKeys(cs.envVars) { // sorted → argv determinístico
		args = append(args, "--env", k+"="+cs.envVars[k])
	}
	if cs.publishAll {
		args = append(args, "-P") // publica portas EXPOSTAS em host-ports dinâmicos
	}
	args = append(args, cs.image, "sleep", "infinity")
	return args
}

func (p *Podman) networkCreateArgs(net string) []string {
	return append(p.baseArgs(), "network", "create", net)
}

func (p *Podman) networkRemoveArgs(net string) []string {
	return append(p.baseArgs(), "network", "rm", net)
}

func (p *Podman) run(ctx context.Context, args []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, p.bin, args...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

func (p *Podman) Create(ctx context.Context, sessionID string, spec EnvSpec) (Env, error) {
	if spec.ComposePath != "" {
		return p.createCompose(ctx, sessionID, spec)
	}
	image := defaultImage
	if v := spec.Labels["image"]; v != "" {
		image = v
	}
	// Sequência derivada de estado durável (containers existentes da sessão),
	// não de memória de processo — sobrevive entre invocações da CLI.
	existing, err := p.List(ctx, sessionID)
	if err != nil {
		return Env{}, err // p.List já retorna DriverError
	}
	namespace := naming.EnvName(sessionID, len(existing)+1)
	network := naming.Network(namespace)
	if _, errs, err := p.run(ctx, p.networkCreateArgs(network)); err != nil {
		return Env{}, DriverError{Code: "network_failed", Message: strings.TrimSpace(errs), Hint: "could not create the per-namespace network; check rootless networking (netavark/aardvark-dns)"}
	}
	cs := containerSpec{
		name:      namespace,
		session:   sessionID,
		namespace: namespace,
		image:     image,
		network:   network,
		extraNets: spec.Networks,
		envVars:   spec.EnvVars,
	}
	if _, errs, err := p.run(ctx, p.createArgs(cs)); err != nil {
		_, _, _ = p.run(ctx, p.networkRemoveArgs(network)) // rollback best-effort
		return Env{}, DriverError{Code: "create_failed", Message: strings.TrimSpace(errs), Hint: "verify the image name and that rootless podman can pull it"}
	}
	return Env{ID: namespace, Name: namespace, Namespace: namespace, Status: "running", Network: network}, nil
}

func (p *Podman) Destroy(ctx context.Context, id string) error {
	if p.containerExists(ctx, id) {
		return p.destroySingle(ctx, id)
	}
	return p.destroyCompose(ctx, id)
}

func isNotFoundStderr(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "no such") || strings.Contains(s, "not found")
}

func (p *Podman) destroySingle(ctx context.Context, id string) error {
	if _, errs, err := p.run(ctx, append(p.baseArgs(), "rm", "-f", id)); err != nil && !isNotFoundStderr(errs) {
		return DriverError{Code: "destroy_failed", Message: strings.TrimSpace(errs), Hint: "id may not exist; run `sbx env status --json`"}
	}
	// Remove a rede do namespace; ignora "not found" para Destroy ser idempotente.
	_, _, _ = p.run(ctx, p.networkRemoveArgs(naming.Network(id)))
	return nil
}

type composePublisher struct {
	URL           string `json:"URL"`
	TargetPort    int    `json:"TargetPort"`
	PublishedPort int    `json:"PublishedPort"`
	Protocol      string `json:"Protocol"`
}

type composePSRow struct {
	Name       string             `json:"Name"`
	Service    string             `json:"Service"`
	State      string             `json:"State"`
	Publishers []composePublisher `json:"Publishers"`
}

// parseComposePS tolerates both a JSON array (some providers) and
// newline-delimited JSON objects (docker compose v2). Pure — unit tested.
func parseComposePS(raw string) ([]composePSRow, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var rows []composePSRow
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	var rows []composePSRow
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r composePSRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func (p *Podman) composeArgs(namespace string, rest ...string) []string {
	args := append(p.baseArgs(), "compose", "-p", namespace)
	return append(args, rest...)
}

func (p *Podman) composeUpArgs(namespace, file string) []string {
	return p.composeArgs(namespace, "-f", file, "up", "-d")
}

func (p *Podman) containerExists(ctx context.Context, id string) bool {
	_, _, err := p.run(ctx, append(p.baseArgs(), "container", "exists", id))
	return err == nil
}

func (p *Podman) createCompose(ctx context.Context, sessionID string, spec EnvSpec) (Env, error) {
	existing, err := p.List(ctx, sessionID)
	if err != nil {
		return Env{}, err
	}
	namespace := naming.EnvName(sessionID, len(existing)+1)
	if _, errs, err := p.run(ctx, p.composeUpArgs(namespace, spec.ComposePath)); err != nil {
		return Env{}, DriverError{Code: "compose_failed", Message: strings.TrimSpace(errs), Hint: "check the compose file is valid and that a `podman compose` provider (docker-compose or podman-compose) is installed"}
	}
	// Portas são populadas em statusCompose (Task 2.4 lê de volta os host-ports).
	return Env{ID: namespace, Name: namespace, Namespace: namespace, Project: namespace, Status: "running", Network: namespace + "_default"}, nil
}

func (p *Podman) composeContainerIDs(ctx context.Context, namespace string) ([]string, error) {
	out, _, err := p.run(ctx, append(p.baseArgs(), "ps", "-aq", "--filter", "label="+composeProjectLabel+"="+namespace))
	if err != nil {
		return nil, err
	}
	return strings.Fields(strings.TrimSpace(out)), nil
}

func (p *Podman) composeVolumeNames(ctx context.Context, namespace string) ([]string, error) {
	out, _, err := p.run(ctx, append(p.baseArgs(), "volume", "ls", "-q", "--filter", "label="+composeProjectLabel+"="+namespace))
	if err != nil {
		return nil, err
	}
	return strings.Fields(strings.TrimSpace(out)), nil
}

// destroyCompose tears a project down by its compose project label, so it does
// NOT need the original compose file at destroy time.
func (p *Podman) destroyCompose(ctx context.Context, namespace string) error {
	ids, _ := p.composeContainerIDs(ctx, namespace)
	for _, id := range ids {
		_, _, _ = p.run(ctx, append(p.baseArgs(), "rm", "-f", id))
	}
	// compose cria uma rede default "<project>_default"
	// ⚠️ CONFIRMAR EM RUNTIME: nome da rede default "<project>_default"
	_, _, _ = p.run(ctx, p.networkRemoveArgs(namespace+"_default"))
	vols, _ := p.composeVolumeNames(ctx, namespace)
	for _, v := range vols {
		_, _, _ = p.run(ctx, append(p.baseArgs(), "volume", "rm", v))
	}
	if len(ids) == 0 {
		return DriverError{Code: "not_found", Message: "no environment named " + namespace, Hint: "run `sbx env status --json` to list ids"}
	}
	return nil
}

func (p *Podman) statusSingle(ctx context.Context, id string) (Env, error) {
	out, errs, err := p.run(ctx, append(p.baseArgs(), "inspect", id, "--format", "{{.State.Status}}"))
	if err != nil {
		return Env{}, DriverError{Code: "not_found", Message: strings.TrimSpace(errs), Hint: "run `sbx env status` to list ids"}
	}
	// Portas são lidas de volta em 2.4.
	return Env{ID: id, Name: id, Namespace: id, Status: strings.TrimSpace(out), Network: naming.Network(id)}, nil
}

func (p *Podman) composePS(ctx context.Context, namespace string) ([]composePSRow, error) {
	out, errs, err := p.run(ctx, p.composeArgs(namespace, "ps", "--format", "json"))
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(errs))
	}
	return parseComposePS(out)
}

func (p *Podman) statusCompose(ctx context.Context, namespace string) (Env, error) {
	rows, err := p.composePS(ctx, namespace)
	if err != nil || len(rows) == 0 {
		return Env{}, DriverError{Code: "not_found", Message: "no environment named " + namespace, Hint: "run `sbx env status --json` to list ids"}
	}
	env := Env{ID: namespace, Name: namespace, Namespace: namespace, Project: namespace, Network: namespace + "_default", Status: "running"}
	for _, r := range rows {
		st := strings.ToLower(r.State)
		if !strings.Contains(st, "run") && !strings.Contains(st, "up") {
			env.Status = "degraded"
		}
		// portas por serviço são anexadas em 2.4 (readComposePorts)
	}
	return env, nil
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
	if p.containerExists(ctx, id) {
		return p.statusSingle(ctx, id)
	}
	return p.statusCompose(ctx, id)
}

func (p *Podman) Exec(ctx context.Context, id string, cmd []string) (ExecResult, error) {
	args := append(p.baseArgs(), "exec", id)
	args = append(args, cmd...)
	c := exec.CommandContext(ctx, p.bin, args...)
	var out, errb strings.Builder
	c.Stdout, c.Stderr = &out, &errb
	err := c.Run()
	res := ExecResult{Stdout: out.String(), Stderr: errb.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, nil // non-zero exit is a valid result, not a driver error
	}
	if err != nil {
		return ExecResult{}, DriverError{Code: "exec_failed", Message: strings.TrimSpace(errb.String()), Hint: "id may not exist or container not running"}
	}
	return res, nil
}

func (p *Podman) Logs(ctx context.Context, id string, opts LogOpts) (string, error) {
	if opts.Service == "" && p.containerExists(ctx, id) {
		args := append(p.baseArgs(), "logs")
		if opts.Tail > 0 {
			args = append(args, "--tail", fmt.Sprintf("%d", opts.Tail))
		}
		args = append(args, id)
		out, errs, err := p.run(ctx, args)
		if err != nil {
			return "", DriverError{Code: "not_found", Message: strings.TrimSpace(errs), Hint: "run `sbx env status` to list ids"}
		}
		return out, nil
	}
	// Compose: resolve os containers do projeto (+ serviço) por label, e usa
	// `podman logs` direto — file-independent.
	psArgs := append(p.baseArgs(), "ps", "-aq", "--filter", "label="+composeProjectLabel+"="+id)
	if opts.Service != "" {
		psArgs = append(psArgs, "--filter", "label="+composeServiceLabel+"="+opts.Service)
	}
	out, errs, err := p.run(ctx, psArgs)
	if err != nil {
		return "", DriverError{Code: "not_found", Message: strings.TrimSpace(errs), Hint: "run `sbx env status --json` to list ids/services"}
	}
	cids := strings.Fields(strings.TrimSpace(out))
	if len(cids) == 0 {
		return "", DriverError{Code: "not_found", Message: "no matching service for " + id, Hint: "run `sbx env status --json` to list ids/services"}
	}
	var buf strings.Builder
	for _, cid := range cids {
		largs := append(p.baseArgs(), "logs")
		if opts.Tail > 0 {
			largs = append(largs, "--tail", fmt.Sprintf("%d", opts.Tail))
		}
		largs = append(largs, cid)
		lout, _, lerr := p.run(ctx, largs)
		if lerr != nil {
			continue
		}
		buf.WriteString(lout)
	}
	return buf.String(), nil
}
