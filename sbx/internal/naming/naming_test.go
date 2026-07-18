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
