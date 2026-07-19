// sbx/internal/driver/podman_test.go
package driver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/naming"
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

	args = p.createArgs(containerSpec{
		name:      "sbx-abc-001",
		session:   "abcdef",
		namespace: "sbx-abc-001",
		image:     "alpine:3",
		envVars:   map[string]string{"B": "2", "A": "1"},
	})
	joined = strings.Join(args, " ")
	idxA := strings.Index(joined, "--env A=1")
	idxB := strings.Index(joined, "--env B=2")
	require.NotEqual(t, -1, idxA)
	require.NotEqual(t, -1, idxB)
	require.Less(t, idxA, idxB, "env vars must be sorted by key in argv")
}

func TestPodmanNetworkArgsShape(t *testing.T) {
	p := NewPodman("/s")
	require.Contains(t, strings.Join(p.networkCreateArgs("sbx-abc-001-net"), " "), "network create sbx-abc-001-net")
	require.Contains(t, strings.Join(p.networkRemoveArgs("sbx-abc-001-net"), " "), "network rm sbx-abc-001-net")
	// rede também roda nos roots próprios (isolamento do engine do host)
	require.Contains(t, strings.Join(p.networkCreateArgs("x"), " "), "--root")
	require.Contains(t, strings.Join(p.networkCreateArgs("x"), " "), "--runroot")
}

func TestPodmanComposeUpArgsShape(t *testing.T) {
	p := NewPodman("/s")
	joined := strings.Join(p.composeUpArgs("sbx-abc-001", "/w/compose.yml"), " ")
	require.Contains(t, joined, "compose -p sbx-abc-001 -f /w/compose.yml up -d")
	require.Contains(t, joined, "--root") // ainda nos roots próprios
}

func TestParseComposePSArray(t *testing.T) {
	raw := `[{"Name":"sbx-abc-001_web_1","Service":"web","State":"running","Publishers":[{"URL":"0.0.0.0","TargetPort":80,"PublishedPort":49153,"Protocol":"tcp"}]}]`
	rows, err := parseComposePS(raw)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "web", rows[0].Service)
	require.Equal(t, 49153, rows[0].Publishers[0].PublishedPort)
	require.Equal(t, 80, rows[0].Publishers[0].TargetPort)
}

func TestParseComposePSNDJSON(t *testing.T) {
	raw := "{\"Name\":\"a_web_1\",\"Service\":\"web\",\"State\":\"running\",\"Publishers\":[{\"TargetPort\":80,\"PublishedPort\":50001}]}\n" +
		"{\"Name\":\"a_worker_1\",\"Service\":\"worker\",\"State\":\"running\",\"Publishers\":[]}\n"
	rows, err := parseComposePS(raw)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "worker", rows[1].Service)
}

func TestParseComposePSEmpty(t *testing.T) {
	rows, err := parseComposePS("")
	require.NoError(t, err)
	require.Len(t, rows, 0)
}

func TestFilterComposeProjects(t *testing.T) {
	sessionID := "abcdefghijkl"
	prefix := composeProjectPrefix(sessionID)
	rows := []psRow{
		{Labels: map[string]string{composeProjectLabel: "sbx-abcdefgh-001"}},
		{Labels: map[string]string{composeProjectLabel: "sbx-abcdefgh-001"}},
		{Labels: map[string]string{composeProjectLabel: "sbx-abcdefgh-002"}},
		{Labels: map[string]string{composeProjectLabel: "other-project"}},
		{Labels: map[string]string{}},
	}
	got := filterComposeProjects(rows, prefix)
	require.Equal(t, []string{"sbx-abcdefgh-001", "sbx-abcdefgh-002"}, got)
}

func TestParseInspectPorts(t *testing.T) {
	raw := `{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"49153"}],"443/tcp":[{"HostIp":"0.0.0.0","HostPort":"49154"}]}`
	ports := parseInspectPorts(raw)
	require.Len(t, ports, 2)
	// ordenado por porta do container
	require.Equal(t, 80, ports[0].Container)
	require.Equal(t, 49153, ports[0].Host)
	require.Equal(t, 443, ports[1].Container)
	require.Equal(t, 49154, ports[1].Host)
}

func TestParseInspectPortsEmpty(t *testing.T) {
	require.Len(t, parseInspectPorts("null"), 0)
	require.Len(t, parseInspectPorts(""), 0)
	require.Len(t, parseInspectPorts("{}"), 0)
}

func TestPodmanCreateArgsPublishesDynamically(t *testing.T) {
	p := NewPodman("/s")
	joined := strings.Join(p.createArgs(containerSpec{name: "n", session: "s", namespace: "n", image: "nginx", publishAll: true}), " ")
	require.Contains(t, joined, "-P")            // publica portas EXPOSTAS dinamicamente
	require.NotContains(t, joined, "-p 0.0.0.0") // nunca porta fixa
}

func TestComposeSeqFormula(t *testing.T) {
	// createCompose and Create (single) share the same seq formula:
	// seq = len(List) + 1, where List already includes singles and compose projects.
	const sessionID = "abcdefghijkl"
	existing := []Env{
		{ID: "sbx-abcdefgh-001"}, // single
		{ID: "sbx-abcdefgh-002"}, // compose project
		{ID: "sbx-abcdefgh-003"}, // compose project
	}
	seq := len(existing) + 1
	require.Equal(t, 4, seq)
	require.Equal(t, "sbx-abcdefgh-004", naming.EnvName(sessionID, seq))
}
