// sbx/internal/netpolicy/allow_test.go
package netpolicy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultAllowContainsAnthropicAndRegistries(t *testing.T) {
	got := DefaultAllow()
	for _, d := range []string{
		"api.anthropic.com",
		"registry.npmjs.org",
		"pypi.org",
		"files.pythonhosted.org",
		"crates.io",
		"static.crates.io",
		"deb.debian.org",
		"*.ubuntu.com",
	} {
		require.Contains(t, got, d)
	}
}

func TestLoadAllowMissingFileReturnsDefaults(t *testing.T) {
	got, err := LoadAllow(filepath.Join(t.TempDir(), "nope.allow"))
	require.NoError(t, err)
	require.Equal(t, DefaultAllow(), got) // sem dups, dedup preserva a ordem dos defaults
}

func TestLoadAllowMergesWithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sbx.allow")
	require.NoError(t, os.WriteFile(path, []byte("# projeto\ninternal.example.com\nGitHub.com\n\n  \n"), 0o644))

	got, err := LoadAllow(path)
	require.NoError(t, err)
	require.Contains(t, got, "api.anthropic.com")    // default preservado
	require.Contains(t, got, "internal.example.com") // adição do projeto
	require.Contains(t, got, "github.com")           // minusculizado
}

func TestLoadAllowDedupsRepeats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sbx.allow")
	require.NoError(t, os.WriteFile(path, []byte("api.anthropic.com\napi.anthropic.com\n"), 0o644))

	got, err := LoadAllow(path)
	require.NoError(t, err)
	n := 0
	for _, d := range got {
		if d == "api.anthropic.com" {
			n++
		}
	}
	require.Equal(t, 1, n, "domínio repetido deve aparecer uma única vez")
}

// A allowlist carregada deve realmente governar o proxy: prova a integração
// entre 3.2 e 3.1 sem sair da máquina.
func TestLoadedAllowGovernsProxyMatch(t *testing.T) {
	p, err := StartProxy(DefaultAllow())
	require.NoError(t, err)
	defer p.Stop()
	require.True(t, p.allowed("api.anthropic.com:443"))
	require.True(t, p.allowed("archive.ubuntu.com:443"))
	require.False(t, p.allowed("prod.internal.example.com:443"))
}
