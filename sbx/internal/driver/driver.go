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

// DriverError is a neutral, actionable error type for the driver package. The
// cli package maps it onto its own CLIError when rendering output.
type DriverError struct{ Code, Message, Hint string }

func (e DriverError) Error() string { return e.Message }

func CLIErrorLike(code, msg, hint string) error {
	return DriverError{Code: code, Message: msg, Hint: hint}
}
