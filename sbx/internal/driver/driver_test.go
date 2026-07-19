// sbx/internal/driver/driver_test.go
package driver

import "testing"

func TestEnvSpecZeroValueUsable(t *testing.T) {
	var s EnvSpec
	if s.Labels != nil {
		t.Fatal("zero EnvSpec should have nil Labels")
	}
}
