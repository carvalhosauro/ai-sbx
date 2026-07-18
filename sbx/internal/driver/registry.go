// sbx/internal/driver/registry.go
package driver

import "fmt"

// Select builds the Driver named by name (typically sourced from the
// internal SBX_DRIVER env var — never a CLI flag, so the agent-facing
// contract never leaks the backend). stateDir scopes the driver's own
// storage/runroot so it never collides with the host's default engine.
func Select(name, stateDir string) (Driver, error) {
	switch name {
	case "", "podman":
		return NewPodman(stateDir), nil
	case "fake":
		return NewFake(), nil
	default:
		return nil, DriverError{
			Code:    "unknown_driver",
			Message: fmt.Sprintf("unknown driver %q", name),
			Hint:    "set SBX_DRIVER to podman or fake",
		}
	}
}
