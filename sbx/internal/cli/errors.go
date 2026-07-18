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
