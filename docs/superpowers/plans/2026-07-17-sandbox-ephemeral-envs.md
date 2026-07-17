# Sandbox — Ambientes de Teste Efêmeros (CLI `sbx`) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Uma CLI em Go (`sbx`) que dá a agentes de IA primitivas para subir/derrubar N ambientes de container efêmeros e isolados, num engine rootless próprio do produto — sem nunca tocar o Docker/credenciais do host.

**Architecture:** CLI irmã do `ai-jail` (não o forka — decisão D1 do ADR 0001). Topologia **ao lado** (D5): um engine rootless controlado pelo produto (storage/rede/socket próprios) roda fora do `bwrap`; o agente roda sob `ai-jail --no-docker` com só o socket do produto mapeado via `--rw-map`. Backend por trás de uma interface `Driver` plugável (D6): 1º driver Podman rootless, depois Docker-rootless. O contrato de CLI não vaza o backend.

**Tech Stack:** Go 1.22+; `spf13/cobra` (subcomandos + `--help` rico); stdlib `encoding/json` (`--json`); `os/exec` para dirigir o `podman` CLI (sem client pesado no início); testes com `testing` + `testify/require`. Podman rootless (Linux nativo + WSL2).

## Global Constraints

Copiados verbatim do ADR 0001 (invariantes de segurança — inegociáveis):

- **O socket do Docker do host NUNCA é exposto ao agente.** O produto jamais binda `/var/run/docker.sock`; usa socket próprio por sessão.
- **Nenhuma credencial do host** (`~/.aws`, `~/.config/gcloud`, `.env*`, `~/.ssh`, keychains) é montada/visível no ambiente.
- **Rede com allowlist:** por padrão só `api.anthropic.com` + registries (npm, PyPI, crates.io, apt). Endpoints de prod/homolog inacessíveis por rede.
- **Ambientes rodam num engine PRÓPRIO da sandbox**, não o do host.
- **Ciclo de vida atrelado à sessão:** ao encerrar a sessão (ou por timeout), todos os ambientes são destruídos automaticamente, mesmo sem cleanup do agente.
- **Driver plugável:** o contrato exposto ao agente não pode vazar detalhes do backend.
- **Saída amigável para LLM:** erros explícitos e acionáveis; opção `--json`.
- **Sem** motor de validação declarativa (quem julga sucesso é o agente). **Sem** root permanente no host. **Sem** UI gráfica.

Convenções de projeto:
- Go module: `github.com/gustavocarvalho/sbx` (ajustar path final).
- Binário: `sbx`. Namespace único por ambiente: `sbx-<session8>-<env8>`.
- Portas **sempre dinâmicas** (nunca fixas no host); publicadas via mapeamento efêmero e lidas de volta.
- `gofmt` + `go vet` limpos antes de todo commit. `golangci-lint` no CI.
- Toda saída de erro tem forma acionável e, sob `--json`, o shape `{"error": {"code": "...", "message": "...", "hint": "..."}}`.

---

## File Structure

```
sbx/
  go.mod
  cmd/sbx/main.go                 -- entrypoint; monta root cobra command
  internal/cli/
    root.go                       -- root cmd, flag global --json, --session
    env.go                        -- grupo `env` e subcomandos create/exec/logs/status/destroy
    output.go                     -- render humano vs --json (Printer)
    errors.go                     -- CLIError {Code,Message,Hint}; mapeamento p/ exit codes
  internal/driver/
    driver.go                     -- interface Driver + tipos (EnvSpec, Env, ExecResult, ...)
    fake.go                       -- driver em memória (M0; base de testes de CLI)
    podman.go                     -- driver Podman rootless via os/exec (M1+)
    docker.go                     -- driver Docker-rootless (M6)
    registry.go                   -- seleção de driver por nome (não vaza no contrato)
  internal/session/
    session.go                    -- id de sessão, diretório de estado, registry de envs (JSON)
    lifecycle.go                  -- supervisor: timeout + destroy-all no fim (M4)
  internal/naming/
    naming.go                     -- nomes/namespaces únicos, alocação lógica de portas
  internal/netpolicy/
    proxy.go                      -- proxy de egresso + allowlist (M3)
  internal/aijail/
    compose.go                    -- receita de invocação ai-jail (flags) + trecho CLAUDE.md (M5)
  testdata/
    compose.min.yml               -- compose mínimo p/ testes de integração
```

Responsabilidades isoladas: `cli` só traduz argumentos↔driver e formata saída; `driver` só fala com backend; `session` só gerencia estado/ciclo de vida; `netpolicy` só rede. Arquivos que mudam juntos moram juntos (cada subcomando + seu teste).

---

## Milestones (visão)

| M | Entrega testável isolada |
|---|---|
| M0 | Esqueleto CLI + contrato de comandos + driver *fake* em memória. Testes de CLI ponta-a-ponta sem engine. |
| M1 | Driver Podman rootless: `create`/`destroy` de 1 ambiente, namespace único, storage/runroot próprios. |
| M2 | N ambientes paralelos sem colisão (portas dinâmicas, redes/volumes por namespace). |
| M3 | Allowlist de egresso (proxy) + prod/homolog inacessível por rede. |
| M4 | Ciclo de vida por sessão + auto-destroy (timeout/fim de sessão). |
| M5 | Costura ai-jail (`--no-docker` + `--rw-map`) + trecho de CLAUDE.md gerado. |
| M6 | Driver Docker-rootless plugável. |

**M0 e M1 estão detalhados em passos bite-sized abaixo.** M2–M6 são backlogs de tarefas (arquivos, interfaces, testes, aceitação); cada um é expandido para passos bite-sized no início do próprio milestone, quando o comportamento real do Podman/proxy já pôde ser confirmado rodando.

---

## MILESTONE 0 — Esqueleto CLI + contrato + driver fake

Objetivo: o contrato de CLI completo e estável, exercitável de ponta a ponta contra um driver em memória. Nenhum container real. Isso trava a **interface exposta ao agente** antes de qualquer backend.

### Task 0.1: Bootstrap do módulo e root command

**Files:**
- Create: `sbx/go.mod`
- Create: `sbx/cmd/sbx/main.go`
- Create: `sbx/internal/cli/root.go`
- Test: `sbx/internal/cli/root_test.go`

**Interfaces:**
- Produces: `cli.NewRootCmd() *cobra.Command`; flag global persistente `--json` (bool) e `--session <id>` (string); `cli.Execute() int` (retorna exit code).

- [ ] **Step 1: Inicializar módulo e dependência cobra**

Run:
```bash
cd sbx && go mod init github.com/gustavocarvalho/sbx && go get github.com/spf13/cobra@latest github.com/stretchr/testify@latest
```
Expected: `go.mod` criado com `cobra` e `testify` em `require`.

- [ ] **Step 2: Escrever o teste que falha (root --help lista `env`)**

```go
// sbx/internal/cli/root_test.go
package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootHelpListsEnvGroup(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), "env")
	require.Contains(t, out.String(), "Ephemeral test environments")
}

func TestRootHasJSONAndSessionFlags(t *testing.T) {
	cmd := NewRootCmd()
	require.NotNil(t, cmd.PersistentFlags().Lookup("json"))
	require.NotNil(t, cmd.PersistentFlags().Lookup("session"))
}
```

- [ ] **Step 3: Rodar o teste e ver falhar**

Run: `cd sbx && go test ./internal/cli/ -run TestRoot -v`
Expected: FAIL (compilação: `NewRootCmd` indefinido).

- [ ] **Step 4: Implementar root command mínimo**

```go
// sbx/internal/cli/root.go
package cli

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "sbx",
		Short:         "Ephemeral, isolated container test environments for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().Bool("json", false, "machine-readable JSON output")
	root.PersistentFlags().String("session", "", "session id (defaults to $SBX_SESSION or a generated id)")
	root.AddCommand(newEnvCmd())
	return root
}
```

```go
// sbx/cmd/sbx/main.go
package main

import (
	"os"

	"github.com/gustavocarvalho/sbx/internal/cli"
)

func main() { os.Exit(cli.Execute()) }
```

```go
// append to sbx/internal/cli/root.go
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		return 1
	}
	return 0
}
```

- [ ] **Step 5: Stub do grupo `env` para o teste compilar**

```go
// sbx/internal/cli/env.go
package cli

import "github.com/spf13/cobra"

func newEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Ephemeral test environments (create, exec, logs, status, destroy)",
	}
}
```

- [ ] **Step 6: Rodar os testes e ver passar**

Run: `cd sbx && go test ./internal/cli/ -run TestRoot -v`
Expected: PASS (2 testes).

- [ ] **Step 7: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: bootstrap sbx CLI root command"
```

### Task 0.2: Interface `Driver` + tipos do domínio

**Files:**
- Create: `sbx/internal/driver/driver.go`
- Test: `sbx/internal/driver/driver_test.go`

**Interfaces:**
- Produces:
  ```go
  type EnvSpec struct { ComposePath string; Labels map[string]string }
  type PortMap struct { Service string; Container int; Host int }
  type Env struct { ID, Name, Namespace, Status string; Ports []PortMap }
  type ExecResult struct { ExitCode int; Stdout, Stderr string }
  type LogOpts struct { Service string; Tail int }
  type Driver interface {
      Name() string
      Create(ctx context.Context, sessionID string, spec EnvSpec) (Env, error)
      Exec(ctx context.Context, id string, cmd []string) (ExecResult, error)
      Logs(ctx context.Context, id string, opts LogOpts) (string, error)
      Status(ctx context.Context, id string) (Env, error)
      List(ctx context.Context, sessionID string) ([]Env, error)
      Destroy(ctx context.Context, id string) error
  }
  ```

- [ ] **Step 1: Escrever teste de contrato (compile-time) do tipo**

```go
// sbx/internal/driver/driver_test.go
package driver

import "testing"

func TestEnvSpecZeroValueUsable(t *testing.T) {
	var s EnvSpec
	if s.Labels != nil {
		t.Fatal("zero EnvSpec should have nil Labels")
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/driver/ -v`
Expected: FAIL (pacote não compila: tipos indefinidos).

- [ ] **Step 3: Implementar `driver.go`**

```go
// sbx/internal/driver/driver.go
package driver

import "context"

type EnvSpec struct {
	ComposePath string
	Labels      map[string]string
}

type PortMap struct {
	Service   string `json:"service"`
	Container int    `json:"container"`
	Host      int    `json:"host"`
}

type Env struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Status    string    `json:"status"`
	Ports     []PortMap `json:"ports"`
}

type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type LogOpts struct {
	Service string
	Tail    int
}

type Driver interface {
	Name() string
	Create(ctx context.Context, sessionID string, spec EnvSpec) (Env, error)
	Exec(ctx context.Context, id string, cmd []string) (ExecResult, error)
	Logs(ctx context.Context, id string, opts LogOpts) (string, error)
	Status(ctx context.Context, id string) (Env, error)
	List(ctx context.Context, sessionID string) ([]Env, error)
	Destroy(ctx context.Context, id string) error
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `cd sbx && go test ./internal/driver/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: define Driver interface and domain types"
```

### Task 0.3: Driver fake em memória

**Files:**
- Create: `sbx/internal/driver/fake.go`
- Test: `sbx/internal/driver/fake_test.go`

**Interfaces:**
- Consumes: `Driver`, `Env`, `EnvSpec`, `ExecResult`, `LogOpts`.
- Produces: `func NewFake() *Fake` implementando `Driver`. `Create` gera IDs determinísticos por contador; guarda envs num map; `Destroy` remove; `List` filtra por sessão. `Name() == "fake"`.

- [ ] **Step 1: Escrever os testes que falham (ciclo de vida em memória)**

```go
// sbx/internal/driver/fake_test.go
package driver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFakeCreateListDestroy(t *testing.T) {
	ctx := context.Background()
	f := NewFake()

	e, err := f.Create(ctx, "sess1", EnvSpec{ComposePath: "compose.yml"})
	require.NoError(t, err)
	require.NotEmpty(t, e.ID)
	require.Equal(t, "running", e.Status)
	require.Contains(t, e.Namespace, "sess1"[:5])

	list, err := f.List(ctx, "sess1")
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, f.Destroy(ctx, e.ID))
	list, _ = f.List(ctx, "sess1")
	require.Len(t, list, 0)
}

func TestFakeDestroyUnknownIsError(t *testing.T) {
	require.Error(t, NewFake().Destroy(context.Background(), "nope"))
}

func TestFakeExecEchoesCmd(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	e, _ := f.Create(ctx, "s", EnvSpec{})
	r, err := f.Exec(ctx, e.ID, []string{"echo", "hi"})
	require.NoError(t, err)
	require.Equal(t, 0, r.ExitCode)
	require.Contains(t, r.Stdout, "echo hi")
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/driver/ -run TestFake -v`
Expected: FAIL (`NewFake` indefinido).

- [ ] **Step 3: Implementar o fake**

```go
// sbx/internal/driver/fake.go
package driver

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Fake struct {
	mu   sync.Mutex
	seq  int
	envs map[string]envRec
}

type envRec struct {
	env     Env
	session string
}

func NewFake() *Fake { return &Fake{envs: map[string]envRec{}} }

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Create(_ context.Context, sessionID string, _ EnvSpec) (Env, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := fmt.Sprintf("env%03d", f.seq)
	short := sessionID
	if len(short) > 5 {
		short = short[:5]
	}
	e := Env{
		ID:        id,
		Name:      "sbx-" + short + "-" + id,
		Namespace: "sbx-" + short + "-" + id,
		Status:    "running",
		Ports:     nil,
	}
	f.envs[id] = envRec{env: e, session: sessionID}
	return e, nil
}

func (f *Fake) Exec(_ context.Context, id string, cmd []string) (ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.envs[id]; !ok {
		return ExecResult{}, fmt.Errorf("environment %q not found", id)
	}
	return ExecResult{ExitCode: 0, Stdout: strings.Join(cmd, " ") + "\n"}, nil
}

func (f *Fake) Logs(_ context.Context, id string, _ LogOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.envs[id]; !ok {
		return "", fmt.Errorf("environment %q not found", id)
	}
	return "fake logs for " + id + "\n", nil
}

func (f *Fake) Status(_ context.Context, id string) (Env, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.envs[id]
	if !ok {
		return Env{}, fmt.Errorf("environment %q not found", id)
	}
	return r.env, nil
}

func (f *Fake) List(_ context.Context, sessionID string) ([]Env, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Env
	for _, r := range f.envs {
		if r.session == sessionID {
			out = append(out, r.env)
		}
	}
	return out, nil
}

func (f *Fake) Destroy(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.envs[id]; !ok {
		return fmt.Errorf("environment %q not found", id)
	}
	delete(f.envs, id)
	return nil
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `cd sbx && go test ./internal/driver/ -run TestFake -v`
Expected: PASS (3 testes).

- [ ] **Step 5: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: add in-memory fake driver"
```

### Task 0.4: Printer humano/JSON + erros acionáveis

**Files:**
- Create: `sbx/internal/cli/output.go`
- Create: `sbx/internal/cli/errors.go`
- Test: `sbx/internal/cli/output_test.go`

**Interfaces:**
- Produces:
  - `type Printer struct { W io.Writer; JSON bool }`; `func (p Printer) Env(e driver.Env) error`; `func (p Printer) Envs(list []driver.Env) error`; `func (p Printer) Exec(r driver.ExecResult) error`; `func (p Printer) Raw(s string) error`.
  - `type CLIError struct { Code, Message, Hint string }` implementando `error`; `func (e CLIError) Error() string`; `func writeError(w io.Writer, jsonMode bool, err error)`.

- [ ] **Step 1: Escrever testes que falham (JSON e humano)**

```go
// sbx/internal/cli/output_test.go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestPrinterEnvJSON(t *testing.T) {
	var b bytes.Buffer
	p := Printer{W: &b, JSON: true}
	require.NoError(t, p.Env(driver.Env{ID: "env001", Status: "running"}))
	var got map[string]any
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))
	require.Equal(t, "env001", got["id"])
}

func TestPrinterEnvHuman(t *testing.T) {
	var b bytes.Buffer
	p := Printer{W: &b, JSON: false}
	require.NoError(t, p.Env(driver.Env{ID: "env001", Status: "running"}))
	require.Contains(t, b.String(), "env001")
	require.Contains(t, b.String(), "running")
}

func TestWriteErrorJSONShape(t *testing.T) {
	var b bytes.Buffer
	writeError(&b, true, CLIError{Code: "not_found", Message: "environment \"x\" not found", Hint: "run `sbx env status --json` to list ids"})
	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))
	require.Equal(t, "not_found", got["error"]["code"])
	require.NotEmpty(t, got["error"]["hint"])
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/cli/ -run 'TestPrinter|TestWriteError' -v`
Expected: FAIL (símbolos indefinidos).

- [ ] **Step 3: Implementar `errors.go` e `output.go`**

```go
// sbx/internal/cli/errors.go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

type CLIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (e CLIError) Error() string { return e.Message }

func writeError(w io.Writer, jsonMode bool, err error) {
	ce, ok := err.(CLIError)
	if !ok {
		ce = CLIError{Code: "internal", Message: err.Error()}
	}
	if jsonMode {
		_ = json.NewEncoder(w).Encode(map[string]CLIError{"error": ce})
		return
	}
	fmt.Fprintf(w, "error: %s\n", ce.Message)
	if ce.Hint != "" {
		fmt.Fprintf(w, "hint: %s\n", ce.Hint)
	}
}
```

```go
// sbx/internal/cli/output.go
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gustavocarvalho/sbx/internal/driver"
)

type Printer struct {
	W    io.Writer
	JSON bool
}

func (p Printer) enc(v any) error {
	e := json.NewEncoder(p.W)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func (p Printer) Env(e driver.Env) error {
	if p.JSON {
		return p.enc(e)
	}
	_, err := fmt.Fprintf(p.W, "%s\t%s\t%s\n", e.ID, e.Status, e.Namespace)
	return err
}

func (p Printer) Envs(list []driver.Env) error {
	if p.JSON {
		return p.enc(list)
	}
	for _, e := range list {
		if err := p.Env(e); err != nil {
			return err
		}
	}
	return nil
}

func (p Printer) Exec(r driver.ExecResult) error {
	if p.JSON {
		return p.enc(r)
	}
	if r.Stdout != "" {
		fmt.Fprint(p.W, r.Stdout)
	}
	if r.Stderr != "" {
		fmt.Fprint(p.W, r.Stderr)
	}
	return nil
}

func (p Printer) Raw(s string) error {
	_, err := io.WriteString(p.W, s)
	return err
}
```

- [ ] **Step 4: Rodar e ver passar**

Run: `cd sbx && go test ./internal/cli/ -run 'TestPrinter|TestWriteError' -v`
Expected: PASS (3 testes).

- [ ] **Step 5: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: add JSON/human Printer and actionable CLIError"
```

### Task 0.5: Subcomandos `env` cabeados ao driver (via injeção)

**Files:**
- Modify: `sbx/internal/cli/env.go`
- Modify: `sbx/internal/cli/root.go`
- Test: `sbx/internal/cli/env_test.go`

**Interfaces:**
- Consumes: `driver.Driver`, `Printer`, `CLIError`, `Fake`, `NewRootCmd`.
- Produces: `newEnvCmd()` com subcomandos `create --from <compose>`, `exec <id> -- <cmd...>`, `logs <id> [--service s] [--tail n]`, `status [<id>]`, `destroy <id> | --all`. Driver e sessão resolvidos por um `deps` struct pendurado no `context` do comando: `type deps struct { drv driver.Driver; session string; json bool }`. Helper de teste `newRootCmdWithDriver(d driver.Driver) *cobra.Command`.

- [ ] **Step 1: Escrever os testes ponta-a-ponta da CLI (contra o fake)**

```go
// sbx/internal/cli/env_test.go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func run(t *testing.T, d driver.Driver, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmdWithDriver(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestEnvCreateThenStatusJSON(t *testing.T) {
	d := driver.NewFake()
	out, err := run(t, d, "--json", "--session", "sess1", "env", "create", "--from", "compose.yml")
	require.NoError(t, err)
	var created driver.Env
	require.NoError(t, json.Unmarshal([]byte(out), &created))
	require.NotEmpty(t, created.ID)

	out, err = run(t, d, "--json", "--session", "sess1", "env", "status")
	require.NoError(t, err)
	var list []driver.Env
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	require.Len(t, list, 1)
}

func TestEnvExecPassesCommandAfterDashDash(t *testing.T) {
	d := driver.NewFake()
	out, _ := run(t, d, "--session", "s", "env", "create")
	id := firstField(out)
	out, err := run(t, d, "--session", "s", "env", "exec", id, "--", "echo", "hi")
	require.NoError(t, err)
	require.Contains(t, out, "echo hi")
}

func TestEnvDestroyUnknownReturnsActionableError(t *testing.T) {
	out, err := run(t, driver.NewFake(), "--json", "env", "destroy", "nope")
	require.Error(t, err)
	require.Contains(t, out, "not_found")
	require.Contains(t, out, "hint")
}

func TestEnvDestroyAll(t *testing.T) {
	d := driver.NewFake()
	_, _ = run(t, d, "--session", "s", "env", "create")
	_, _ = run(t, d, "--session", "s", "env", "create")
	_, err := run(t, d, "--session", "s", "env", "destroy", "--all")
	require.NoError(t, err)
	out, _ := run(t, d, "--json", "--session", "s", "env", "status")
	require.Contains(t, out, "[]")
}
```

Helper (mesmo arquivo):
```go
func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' || s[i] == '\n' || s[i] == ' ' {
			return s[:i]
		}
	}
	return s
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/cli/ -run TestEnv -v`
Expected: FAIL (`newRootCmdWithDriver` e subcomandos indefinidos).

- [ ] **Step 3: Implementar injeção de deps + subcomandos**

```go
// sbx/internal/cli/env.go
package cli

import (
	"context"
	"os"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/spf13/cobra"
)

type ctxKey struct{}

type deps struct {
	drv     driver.Driver
	session string
	json    bool
}

func depsFrom(cmd *cobra.Command) deps {
	if d, ok := cmd.Context().Value(ctxKey{}).(deps); ok {
		return d
	}
	return deps{}
}

func printerFor(cmd *cobra.Command, d deps) Printer {
	return Printer{W: cmd.OutOrStdout(), JSON: d.json}
}

func resolveSession(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if s := os.Getenv("SBX_SESSION"); s != "" {
		return s
	}
	return "default"
}

func newEnvCmd() *cobra.Command {
	env := &cobra.Command{
		Use:   "env",
		Short: "Ephemeral test environments (create, exec, logs, status, destroy)",
	}
	env.AddCommand(newCreateCmd(), newExecCmd(), newLogsCmd(), newStatusCmd(), newDestroyCmd())
	return env
}

func newCreateCmd() *cobra.Command {
	var from string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new ephemeral environment (optionally from a compose file)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			e, err := d.drv.Create(cmd.Context(), d.session, driver.EnvSpec{ComposePath: from})
			if err != nil {
				return CLIError{Code: "create_failed", Message: err.Error(), Hint: "check the compose file path and that the engine is available"}
			}
			return printerFor(cmd, d).Env(e)
		},
	}
	c.Flags().StringVar(&from, "from", "", "path to a compose.yml to bring the environment up from")
	return c
}

func newExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "exec <id> -- <cmd>...",
		Short:              "Run a command inside the environment",
		Args:               cobra.MinimumNArgs(2),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			id, rest := args[0], args[1:]
			r, err := d.drv.Exec(cmd.Context(), id, rest)
			if err != nil {
				return CLIError{Code: "not_found", Message: err.Error(), Hint: "run `sbx env status --json` to list ids"}
			}
			if err := printerFor(cmd, d).Exec(r); err != nil {
				return err
			}
			if r.ExitCode != 0 {
				return CLIError{Code: "exec_nonzero", Message: "command exited non-zero"}
			}
			return nil
		},
	}
}

func newLogsCmd() *cobra.Command {
	var service string
	var tail int
	c := &cobra.Command{
		Use:   "logs <id>",
		Short: "Show logs for the environment (optionally a single service)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			s, err := d.drv.Logs(cmd.Context(), args[0], driver.LogOpts{Service: service, Tail: tail})
			if err != nil {
				return CLIError{Code: "not_found", Message: err.Error(), Hint: "run `sbx env status --json` to list ids"}
			}
			return printerFor(cmd, d).Raw(s)
		},
	}
	c.Flags().StringVar(&service, "service", "", "limit logs to one service")
	c.Flags().IntVar(&tail, "tail", 0, "show only the last N lines (0 = all)")
	return c
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [id]",
		Short: "Show one environment or list all in this session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			p := printerFor(cmd, d)
			if len(args) == 1 {
				e, err := d.drv.Status(cmd.Context(), args[0])
				if err != nil {
					return CLIError{Code: "not_found", Message: err.Error(), Hint: "omit the id to list all environments"}
				}
				return p.Env(e)
			}
			list, err := d.drv.List(cmd.Context(), d.session)
			if err != nil {
				return CLIError{Code: "list_failed", Message: err.Error()}
			}
			return p.Envs(list)
		},
	}
}

func newDestroyCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "destroy <id> | --all",
		Short: "Destroy one environment or all in this session",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			if all {
				list, err := d.drv.List(cmd.Context(), d.session)
				if err != nil {
					return CLIError{Code: "list_failed", Message: err.Error()}
				}
				for _, e := range list {
					if err := d.drv.Destroy(cmd.Context(), e.ID); err != nil {
						return CLIError{Code: "destroy_failed", Message: err.Error()}
					}
				}
				return nil
			}
			if len(args) != 1 {
				return CLIError{Code: "usage", Message: "provide an environment id or --all", Hint: "e.g. `sbx env destroy env001` or `sbx env destroy --all`"}
			}
			if err := d.drv.Destroy(cmd.Context(), args[0]); err != nil {
				return CLIError{Code: "not_found", Message: err.Error(), Hint: "run `sbx env status --json` to list ids"}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "destroy every environment in the session")
	return c
}

var _ = context.Background
```

```go
// append to sbx/internal/cli/root.go
import (
	"context"

	"github.com/gustavocarvalho/sbx/internal/driver"
)

// wireDeps reads persistent flags, builds deps, and injects them into ctx
// via PersistentPreRunE so every subcommand shares one driver + session.
func wireDeps(root *cobra.Command, d driver.Driver) {
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		jsonMode, _ := cmd.Flags().GetBool("json")
		sess, _ := cmd.Flags().GetString("session")
		ctx := context.WithValue(cmd.Context(), ctxKey{}, deps{
			drv:     d,
			session: resolveSession(sess),
			json:    jsonMode,
		})
		cmd.SetContext(ctx)
		return nil
	}
}

func newRootCmdWithDriver(d driver.Driver) *cobra.Command {
	root := NewRootCmd()
	wireDeps(root, d)
	return root
}
```

Ajustar `NewRootCmd`/`Execute` para produção usarem o driver real quando existir (por ora, fake): em `Execute()`, trocar para `newRootCmdWithDriver(driver.NewFake())` e centralizar o render de erro:

```go
// replace Execute() in root.go
func Execute() int {
	root := newRootCmdWithDriver(driver.NewFake())
	if err := root.Execute(); err != nil {
		jsonMode, _ := root.Flags().GetBool("json")
		writeError(root.OutOrStderr(), jsonMode, err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Rodar toda a suíte e ver passar**

Run: `cd sbx && go test ./... -v`
Expected: PASS (todos os testes de cli + driver).

- [ ] **Step 5: Sanidade manual do `--help` e de um ciclo fake**

Run:
```bash
cd sbx && go run ./cmd/sbx env --help && go run ./cmd/sbx --session demo env create && go run ./cmd/sbx --json --session demo env status
```
Expected: help lista create/exec/logs/status/destroy; create imprime uma linha de env; status `--json` imprime array com 1 env.

- [ ] **Step 6: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: wire env subcommands to injected driver (contract stable on fake)"
```

**Aceitação M0:** `go test ./...` verde; `sbx env --help` documenta todo o contrato; ciclo create→status→exec→logs→destroy funciona ponta-a-ponta contra o fake, com `--json` e erros acionáveis. Nenhum container real ainda.

---

## MILESTONE 1 — Driver Podman rootless (`create`/`destroy` de 1 ambiente)

Objetivo: primeiro backend real. `sbx env create/destroy` sobe/derruba UM ambiente num Podman rootless com **storage/runroot próprios do produto** (nunca o do host), namespace único. Sem compose ainda (single container a partir de uma imagem simples) — compose entra em M2.

**Pré-requisito de ambiente:** `podman` instalado; rootless configurado (`/etc/subuid`+`/etc/subgid` com faixa para o usuário); em WSL2, `systemd=true` em `/etc/wsl.conf`. Task 1.0 valida isso.

**Estratégia de teste:** testes de integração marcados com build tag `//go:build integration` + skip automático se `podman` ausente (`t.Skip`). Rodados via `go test -tags integration ./...`. Unit tests do driver validam a **construção de argv** (determinística, sem executar podman).

### Task 1.0: Detecção de engine + preflight

**Files:**
- Create: `sbx/internal/driver/podman.go` (parte 1: struct + preflight)
- Test: `sbx/internal/driver/podman_test.go`

**Interfaces:**
- Produces: `type Podman struct { bin string; root string; runroot string }`; `func NewPodman(stateDir string) *Podman`; `func (p *Podman) Preflight(ctx) error` (erro acionável se `podman` ausente ou rootless quebrado); `func (p *Podman) baseArgs() []string` retornando `["--root", p.root, "--runroot", p.runroot]`. `Name() == "podman"`.

- [ ] **Step 1: Teste unitário de `baseArgs` (determinístico, sem podman)**

```go
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
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/driver/ -run TestPodman -v`
Expected: FAIL (`NewPodman` indefinido).

- [ ] **Step 3: Implementar struct + baseArgs + Preflight**

```go
// sbx/internal/driver/podman.go
package driver

import (
	"context"
	"fmt"
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
```

`CLIErrorLike` é um construtor de erro neutro no pacote `driver` (o pacote `cli` mapeia para `CLIError`). Adicionar:

```go
// append to sbx/internal/driver/driver.go
type DriverError struct{ Code, Message, Hint string }

func (e DriverError) Error() string { return e.Message }

func CLIErrorLike(code, msg, hint string) error {
	return DriverError{Code: code, Message: msg, Hint: hint}
}
```

E no pacote `cli`, converter `DriverError`→`CLIError` no render de erro (Task 1.4).

- [ ] **Step 4: Rodar e ver passar**

Run: `cd sbx && go test ./internal/driver/ -run TestPodman -v`
Expected: PASS (2 testes).

- [ ] **Step 5: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: podman driver skeleton with own storage roots and preflight"
```

### Task 1.1: `Create`/`Destroy` reais (single container) — argv + integração

**Files:**
- Modify: `sbx/internal/driver/podman.go`
- Modify: `sbx/internal/driver/podman_test.go` (unit de argv)
- Create: `sbx/internal/driver/podman_integration_test.go` (`//go:build integration`)
- Modify: `sbx/internal/naming/naming.go` (Create: nome/namespace único)

**Interfaces:**
- Consumes: `baseArgs`, `naming.EnvName(session, seq)`.
- Produces: `Podman.Create` roda `podman <base> run -d --name <ns> --label sbx.session=<s> --label sbx.managed=true <image>` (imagem default `docker.io/library/alpine:3` com `sleep infinity`, configurável via `EnvSpec.Labels["image"]`); retorna `Env{Status:"running"}`. `Destroy` roda `podman <base> rm -f <name>`. `List` usa `podman <base> ps -a --filter label=sbx.session=<s> --format json`.
- Produces (naming): `func EnvName(session string, seq int) string` → `sbx-<session8>-<NNN>`.

- [ ] **Step 1: Teste unitário de construção de argv do Create (sem executar)**

Refatorar para uma função pura `runArgs`:
```go
// será testada
func (p *Podman) createArgs(name, session, image string) []string
```

```go
// append to sbx/internal/driver/podman_test.go
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
```
(adicionar `import "strings"`)

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/driver/ -run TestPodmanCreateArgs -v`
Expected: FAIL (`createArgs` indefinido).

- [ ] **Step 3: Implementar `createArgs`, `Create`, `Destroy`, `List`, `Status`**

```go
// append to sbx/internal/driver/podman.go
import (
	"encoding/json"
	"os"
)

const defaultImage = "docker.io/library/alpine:3"

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
var _ = os.Getenv
```

- [ ] **Step 4: Rodar unit e ver passar**

Run: `cd sbx && go test ./internal/driver/ -run TestPodmanCreateArgs -v`
Expected: PASS.

- [ ] **Step 5: Escrever teste de integração (real podman, skip se ausente)**

```go
// sbx/internal/driver/podman_integration_test.go
//go:build integration

package driver

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPodmanCreateDestroyIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := NewPodman(t.TempDir())
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
```

- [ ] **Step 6: Rodar integração (na sua máquina com podman)**

Run: `cd sbx && go test -tags integration ./internal/driver/ -run Integration -v`
Expected: PASS (ou SKIP se sem podman). Se FAIL por rootless/subuid, corrigir ambiente (Preflight aponta o hint) antes de seguir.

- [ ] **Step 7: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: real podman create/destroy/list/status (single container)"
```

### Task 1.2: Nomes/namespaces únicos + seq por sessão

**Files:**
- Create: `sbx/internal/naming/naming.go`
- Test: `sbx/internal/naming/naming_test.go`
- Modify: `sbx/internal/driver/podman.go` (usar `naming.EnvName`)

**Interfaces:**
- Produces: `func Short(s string, n int) string`; `func EnvName(session string, seq int) string` → `sbx-<session≤8>-<NNN>`. `envNameFor(session)` no driver usa um contador guardado no `session` state (M4) — por ora, um contador em memória por `*Podman` protege contra colisão dentro do processo; unicidade entre processos vem do próprio podman (`--name` duplicado falha, tratado como erro acionável).

- [ ] **Step 1: Teste de nomes únicos e truncagem**

```go
// sbx/internal/naming/naming_test.go
package naming

import "testing"

func TestEnvNameFormat(t *testing.T) {
	got := EnvName("abcdefghijkl", 7)
	if got != "sbx-abcdefgh-007" {
		t.Fatalf("got %q", got)
	}
}

func TestEnvNameShortSession(t *testing.T) {
	if EnvName("ab", 1) != "sbx-ab-001" {
		t.Fatalf("got %q", EnvName("ab", 1))
	}
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/naming/ -v`
Expected: FAIL (pacote inexistente).

- [ ] **Step 3: Implementar `naming.go`**

```go
// sbx/internal/naming/naming.go
package naming

import "fmt"

func Short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func EnvName(session string, seq int) string {
	return fmt.Sprintf("sbx-%s-%03d", Short(session, 8), seq)
}
```

- [ ] **Step 4: Cabear no driver (contador em memória + colisão acionável)**

```go
// append to sbx/internal/driver/podman.go
import (
	"sync"

	"github.com/gustavocarvalho/sbx/internal/naming"
)

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
```

- [ ] **Step 5: Rodar tudo e ver passar**

Run: `cd sbx && go test ./... -v`
Expected: PASS (unit). Integração continua atrás da tag.

- [ ] **Step 6: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: unique per-session env naming"
```

### Task 1.3: `Exec` e `Logs` reais no Podman

**Files:**
- Modify: `sbx/internal/driver/podman.go`
- Modify: `sbx/internal/driver/podman_integration_test.go`

**Interfaces:**
- Produces: `Podman.Exec` → `podman <base> exec <id> <cmd...>`, captura stdout/stderr/exit; `Podman.Logs` → `podman <base> logs [--tail N] <id>` (serviço ignorado no single-container; usado no compose em M2).

- [ ] **Step 1: Teste de integração de Exec/Logs**

```go
// append to sbx/internal/driver/podman_integration_test.go
func TestPodmanExecAndLogsIntegration(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not installed")
	}
	ctx := context.Background()
	p := NewPodman(t.TempDir())
	require.NoError(t, p.Preflight(ctx))
	e, err := p.Create(ctx, "itest02", EnvSpec{})
	require.NoError(t, err)
	defer p.Destroy(ctx, e.ID)

	r, err := p.Exec(ctx, e.ID, []string{"sh", "-c", "echo hello && exit 3"})
	require.NoError(t, err)
	require.Contains(t, r.Stdout, "hello")
	require.Equal(t, 3, r.ExitCode)
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test -tags integration ./internal/driver/ -run TestPodmanExec -v`
Expected: FAIL (`Exec` retorna zero/erro — ainda não implementado).

- [ ] **Step 3: Implementar Exec/Logs**

```go
// append to sbx/internal/driver/podman.go
import "errors"

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
```

- [ ] **Step 4: Rodar integração e ver passar**

Run: `cd sbx && go test -tags integration ./internal/driver/ -run TestPodmanExec -v`
Expected: PASS (ou SKIP).

- [ ] **Step 5: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: podman exec (with exit code) and logs"
```

### Task 1.4: Selecionar driver real na produção + mapear DriverError→CLIError

**Files:**
- Create: `sbx/internal/driver/registry.go`
- Create: `sbx/internal/session/session.go` (só o `StateDir` por ora)
- Modify: `sbx/internal/cli/root.go` (Execute usa driver real; converte DriverError)
- Modify: `sbx/internal/cli/errors.go` (aceitar DriverError)
- Test: `sbx/internal/cli/errors_test.go`

**Interfaces:**
- Produces: `driver.Select(name, stateDir) (Driver, error)` — `"podman"`→Podman, `"fake"`→Fake, `"docker"`→(M6). Nome vem de env `SBX_DRIVER` (default `podman`); **não** é flag do contrato do agente (não vaza backend). `session.StateDir(sessionID) string` → `$XDG_STATE_HOME/sbx/<session>` (fallback `~/.local/state`).

- [ ] **Step 1: Teste de conversão DriverError→CLIError no writeError**

```go
// sbx/internal/cli/errors_test.go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestWriteErrorMapsDriverError(t *testing.T) {
	var b bytes.Buffer
	writeError(&b, true, driver.DriverError{Code: "engine_missing", Message: "podman not found", Hint: "install podman"})
	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))
	require.Equal(t, "engine_missing", got["error"]["code"])
	require.Equal(t, "install podman", got["error"]["hint"])
}
```

- [ ] **Step 2: Rodar e ver falhar**

Run: `cd sbx && go test ./internal/cli/ -run TestWriteErrorMapsDriver -v`
Expected: FAIL (writeError não reconhece DriverError → cai em "internal").

- [ ] **Step 3: Implementar registry, StateDir e mapeamento de erro**

```go
// sbx/internal/driver/registry.go
package driver

import "fmt"

func Select(name, stateDir string) (Driver, error) {
	switch name {
	case "", "podman":
		return NewPodman(stateDir), nil
	case "fake":
		return NewFake(), nil
	default:
		return nil, DriverError{Code: "unknown_driver", Message: fmt.Sprintf("unknown driver %q", name), Hint: "set SBX_DRIVER to podman or fake"}
	}
}
```

```go
// sbx/internal/session/session.go
package session

import (
	"os"
	"path/filepath"
)

func StateDir(sessionID string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "sbx", sessionID)
}
```

```go
// modify writeError in sbx/internal/cli/errors.go: recognize DriverError
// replace the type assertion block:
func writeError(w io.Writer, jsonMode bool, err error) {
	var ce CLIError
	switch e := err.(type) {
	case CLIError:
		ce = e
	case driver.DriverError:
		ce = CLIError{Code: e.Code, Message: e.Message, Hint: e.Hint}
	default:
		ce = CLIError{Code: "internal", Message: err.Error()}
	}
	if jsonMode {
		_ = json.NewEncoder(w).Encode(map[string]CLIError{"error": ce})
		return
	}
	fmt.Fprintf(w, "error: %s\n", ce.Message)
	if ce.Hint != "" {
		fmt.Fprintf(w, "hint: %s\n", ce.Hint)
	}
}
```
(adicionar `import ".../internal/driver"` em errors.go)

```go
// modify Execute() in root.go to build the real driver + preflight
func Execute() int {
	sess := resolveSession(os.Getenv("SBX_SESSION"))
	drv, err := driver.Select(os.Getenv("SBX_DRIVER"), session.StateDir(sess))
	if err != nil {
		writeError(os.Stderr, false, err)
		return 1
	}
	if pf, ok := drv.(interface{ Preflight(context.Context) error }); ok {
		if err := pf.Preflight(context.Background()); err != nil {
			writeError(os.Stderr, false, err)
			return 1
		}
	}
	root := newRootCmdWithDriver(drv)
	if err := root.Execute(); err != nil {
		jsonMode, _ := root.Flags().GetBool("json")
		writeError(root.OutOrStderr(), jsonMode, err)
		return 1
	}
	return 0
}
```
(adicionar imports `os`, `.../internal/session`)

- [ ] **Step 4: Rodar toda a suíte unit e ver passar**

Run: `cd sbx && go test ./... -v`
Expected: PASS. (`SBX_DRIVER=fake go test` continua cobrindo a CLI sem podman.)

- [ ] **Step 5: Sanidade manual real (com podman)**

Run:
```bash
cd sbx && go build -o /tmp/sbx ./cmd/sbx && \
  SBX_SESSION=demo /tmp/sbx env create && \
  SBX_SESSION=demo /tmp/sbx --json env status && \
  SBX_SESSION=demo /tmp/sbx env destroy --all
```
Expected: cria container real no storage do produto; status lista 1; destroy limpa. `podman ps -a` do host **não** deve mostrar (roots separados).

- [ ] **Step 6: Commit**

```bash
cd sbx && gofmt -w . && go vet ./... && git add . && git commit -m "feat: select real podman driver in production with preflight and error mapping"
```

**Aceitação M1:** com podman rootless, `sbx env create/status/exec/logs/destroy` operam UM container real no storage/runroot do produto; nada aparece no `podman`/`docker` default do host; erros de engine são acionáveis; unit tests rodam sem podman (fake), integração roda com `-tags integration`.

---

## MILESTONE 2 — N ambientes paralelos sem colisão (compose + portas dinâmicas)

Backlog (expandir para bite-sized no início do M2, após confirmar comportamento de `podman kube`/compose e publicação de portas rodando):

- **T2.1 — Rede/volume por namespace.** `Create` cria `podman network create sbx-<ns>` e volumes prefixados; `Destroy` remove rede+volumes. Teste integração: dois envs simultâneos têm redes distintas; nenhum enxerga o outro.
  - Interfaces: estender `Env` com `Network string`; `Podman.createNetwork/removeNetwork`.
- **T2.2 — Compose via `podman`.** `Create --from compose.yml` sobe o projeto com nome de projeto = namespace (`podman compose -p <ns> up -d` OU `podman kube play` — decidir na abertura do M2 medindo paridade). `Logs --service` e `Status` por serviço.
  - Interfaces: `EnvSpec.ComposePath` já existe; `Env.Ports` populado a partir de `podman compose ... ps --format json`.
- **T2.3 — Portas dinâmicas.** Nunca porta fixa; publicar com host-port efêmero (`-P` / compose sem host port) e **ler de volta** a porta atribuída via inspect; expor em `Env.Ports`. Teste: subir 2 envs do mesmo compose, portas host **diferentes**, sem conflito.
- **T2.4 — Limite por sessão.** Config `SBX_MAX_ENVS` (default p.ex. 8); `Create` recusa com erro acionável ao exceder. Teste unit com fake.
- **Aceitação M2:** N envs do mesmo compose sobem juntos sem colisão de nome/rede/volume/porta; `status --json` mostra portas host distintas; limite respeitado.

## MILESTONE 3 — Allowlist de egresso (rede) + prod/homolog inacessível

Backlog:

- **T3.1 — Proxy de egresso.** `internal/netpolicy/proxy.go`: sobe um proxy HTTP(S) CONNECT com allowlist de domínios (default: `api.anthropic.com`, registries npm/PyPI/crates.io/apt). Ambientes recebem `HTTP_PROXY`/`HTTPS_PROXY` apontando pro proxy e a rede do container **só alcança o proxy** (rede interna sem gateway default).
  - Interfaces: `proxy.Start(allow []string) (addr string, stop func())`; injeção via `EnvSpec` env vars + rede interna.
- **T3.2 — Allowlist configurável.** Arquivo `sbx.allow` (lista de domínios) por projeto/sessão; merge com defaults. **Não** é o motor de validação proibido — é só rede.
- **T3.3 — Negação por default comprovada.** Teste integração: de dentro do env, `curl https://api.anthropic.com` (204/headers) passa; `curl https://<host-prod-falso>` **falha/timeout**. Teste que uma credencial no env NÃO alcança endpoint fora da allowlist.
- **T3.4 (opcional, decisão Fase 2 Q1) — Endurecimento kernel.** nftables/Landlock recusando egresso fora do proxy, além do proxy. Só se exigido.
- **Aceitação M3:** egresso default bloqueado exceto allowlist; endpoints prod/homolog inacessíveis por rede mesmo com credencial vazada no contexto.

## MILESTONE 4 — Ciclo de vida por sessão + auto-destroy

Backlog:

- **T4.1 — Registry persistente de envs.** `internal/session`: JSON em `StateDir` com envs vivos por sessão (id, namespace, rede, volumes, portas). `Create`/`Destroy` atualizam. `seq` por sessão persiste aqui (substitui o contador em memória de M1).
- **T4.2 — Supervisor/timeout.** `lifecycle.go`: processo supervisor por sessão que, em `SIGTERM`/fim/`--timeout`, executa `destroy --all` idempotente lendo o registry. Garante limpeza mesmo se o agente esquecer.
  - Interfaces: `sbx session start --timeout 30m` (interno, chamado pela costura M5); `sbx session end`.
- **T4.3 — Reconciliação órfãos.** Na inicialização, `sbx` varre `podman ps --filter label=sbx.managed=true` e remove o que não está em nenhum registry vivo. Teste: matar supervisor deixa órfão; próxima invocação limpa.
- **Aceitação M4:** encerrar a sessão (ou timeout) destrói todos os envs; órfãos são reconciliados; nada vaza entre sessões.

## MILESTONE 5 — Costura ai-jail + trecho CLAUDE.md

Backlog:

- **T5.1 — Receita de invocação.** `internal/aijail/compose.go`: gera a linha de comando `ai-jail --no-docker --rw-map <socket-do-produto> [--mask .env*] <agente>` e/ou um wrapper `sbx shell` que sobe supervisor (M4) + invoca ai-jail já cabeado. Valida que o socket exposto é o do produto, nunca `/var/run/docker.sock`.
- **T5.2 — Gerador de CLAUDE.md.** `sbx bootstrap-claude-md` imprime bloco pronto documentando as primitivas + regras ("nunca use .env de prod/homolog; crie `.env.sandbox`", "portas são dinâmicas, leia de `sbx env status --json`", fluxo típico create→exec→logs→destroy). Teste: saída contém os comandos e as regras-chave.
- **T5.3 — `--help` e exemplos.** Revisar `Short/Long/Example` de cada comando p/ um LLM usar corretamente só lendo `--help`.
- **Aceitação M5:** um comando sobe o agente sob ai-jail já cabeado ao engine do produto (host intocado); `bootstrap-claude-md` gera bloco colável; `--help` cobre o fluxo típico.

## MILESTONE 6 — Driver Docker-rootless plugável

Backlog:

- **T6.1 — `internal/driver/docker.go`** implementando `Driver` via `dockerd-rootless` por sessão (DOCKER_HOST/data-root próprios), espelhando as mesmas semânticas dos testes do Podman (reuso da suíte de contrato).
- **T6.2 — Suíte de contrato compartilhada.** Extrair os testes de comportamento num `drivertest.RunContract(t, factory)` rodado contra fake, podman e docker.
- **T6.3 — Seleção por `SBX_DRIVER=docker`** no registry.
- **Aceitação M6:** mesma suíte de contrato verde nos dois engines reais; contrato de CLI idêntico; artefatos OCI do usuário rodam nos dois.

---

## Self-Review

**1. Cobertura do spec (requisitos do produto no prompt/ADR):**
- Socket host nunca exposto → M1 (roots próprios, sem docker.sock; teste `TestPodmanNeverReferencesHostDockerSocket`), M5 (costura valida socket do produto). ✔
- Sem credenciais do host → M3 (rede) + M5 (`--mask`, ai-jail já esconde dotdirs). ✔
- Allowlist de rede → M3. ✔
- Engine próprio da sandbox → M1 (`--root`/`--runroot`). ✔
- Primitivas create/exec/logs/status/destroy + `--all` + `--json` → M0 (contrato) + M1/M2 (real). ✔
- Namespace único / N paralelos / portas dinâmicas → M1 (naming) + M2. ✔
- Driver plugável sem vazar backend → M0 (interface) + M1.4 (`SBX_DRIVER` interno) + M6. ✔
- Ciclo de vida por sessão + auto-destroy + limite → M4 (+ T2.4). ✔
- Ergonomia LLM (`--help`, CLAUDE.md, erros acionáveis) → M0 (erros) + M5. ✔
- Fora de escopo (validação declarativa/root/GUI) → não há tarefa que os introduza. ✔

**2. Placeholders:** M0/M1 têm código completo em cada passo. M2–M6 são backlogs explicitamente marcados "expandir para bite-sized no início do milestone" — decisão consciente (evita fabricar código Podman/proxy não verificado). Não são placeholders dentro de um milestone detalhado.

**3. Consistência de tipos:** `Driver` (0.2) usado por Fake (0.3), Podman (1.x), registry (1.4). `DriverError` (1.0) mapeado em `writeError` (1.4). `EnvName` (1.2) consumido por `envNameFor` (1.1→1.2). `deps`/`ctxKey` (0.5) consistentes. Nota: `Podman.Create` em 1.1 referencia `envNameFor`, definido em 1.2 — **ordem de execução exige 1.2 antes de compilar 1.1**; ao executar, faça 1.1 (argv+Create sem `envNameFor`, usando nome fixo no teste) e feche a fiação de nome em 1.2, OU inverta a ordem 1.1↔1.2. Recomendado: implementar `naming` (1.2 steps 1–3) antes do Create de 1.1.

---

## Execution Handoff

Plano salvo em `docs/superpowers/plans/2026-07-17-sandbox-ephemeral-envs.md`.
