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

func TestNetworkName(t *testing.T) {
	if Network("sbx-abcd-001") != "sbx-abcd-001-net" {
		t.Fatalf("got %q", Network("sbx-abcd-001"))
	}
}

func TestVolumeName(t *testing.T) {
	if Volume("sbx-abcd-001", "data") != "sbx-abcd-001-data" {
		t.Fatalf("got %q", Volume("sbx-abcd-001", "data"))
	}
}
