// sbx/internal/netpolicy/allow.go
package netpolicy

import (
	"bufio"
	"os"
	"strings"
)

// DefaultAllow is the built-in egress allowlist: the Anthropic API plus the
// package registries an agent needs to install deps. Everything else is denied.
// Suffix-matched by matchDomain, so "api.anthropic.com" also covers subdomains.
func DefaultAllow() []string {
	return []string{
		"api.anthropic.com",
		"registry.npmjs.org",
		"pypi.org",
		"files.pythonhosted.org",
		"crates.io",
		"static.crates.io",
		"deb.debian.org",
		"*.ubuntu.com",
	}
}

// LoadAllow reads a per-project sbx.allow file (one domain per line; '#' comments
// and blank lines ignored) and merges it with DefaultAllow, deduped. A missing
// file is not an error — you just get the defaults. This is network policy only,
// never a declarative-validation engine (invariant §7).
func LoadAllow(path string) ([]string, error) {
	merged := DefaultAllow()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return dedup(merged), nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		merged = append(merged, strings.ToLower(line))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return dedup(merged), nil
}

func dedup(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, d := range in {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
