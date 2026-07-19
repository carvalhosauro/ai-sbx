// sbx/internal/driver/driver_test.go
package driver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvSpecZeroValueUsable(t *testing.T) {
	var s EnvSpec
	if s.Labels != nil {
		t.Fatal("zero EnvSpec should have nil Labels")
	}
}

func TestEnvOmitsEmptyNetworkAndProject(t *testing.T) {
	b, err := json.Marshal(Env{ID: "e1", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "network") || strings.Contains(s, "project") {
		t.Fatalf("empty Network/Project must be omitted, got %s", s)
	}
}

func TestEnvIncludesNetworkWhenSet(t *testing.T) {
	b, _ := json.Marshal(Env{ID: "e1", Network: "sbx-x-001-net", Project: "sbx-x-001"})
	s := string(b)
	if !strings.Contains(s, "sbx-x-001-net") || !strings.Contains(s, `"project":"sbx-x-001"`) {
		t.Fatalf("got %s", s)
	}
}

func TestEnvSpecCarriesEnvVarsAndNetworks(t *testing.T) {
	s := EnvSpec{EnvVars: map[string]string{"HTTP_PROXY": "http://127.0.0.1:1"}, Networks: []string{"extra"}}
	if s.EnvVars["HTTP_PROXY"] == "" || len(s.Networks) != 1 {
		t.Fatal("EnvSpec must carry EnvVars and Networks")
	}
}
