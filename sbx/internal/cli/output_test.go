// sbx/internal/cli/output_test.go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestPrinterEnvJSON(t *testing.T) {
	var b bytes.Buffer
	p := Printer{W: &b, JSON: true}
	require.NoError(t, p.Env(driver.Env{ID: "env001", Status: "running"}))
	var got map[string]any
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))
	require.Equal(t, "env001", got["id"])
}

func TestPrinterEnvHuman(t *testing.T) {
	var b bytes.Buffer
	p := Printer{W: &b, JSON: false}
	require.NoError(t, p.Env(driver.Env{ID: "env001", Status: "running"}))
	require.Contains(t, b.String(), "env001")
	require.Contains(t, b.String(), "running")
}

func TestWriteErrorJSONShape(t *testing.T) {
	var b bytes.Buffer
	writeError(&b, true, CLIError{Code: "not_found", Message: "environment \"x\" not found", Hint: "run `sbx env status --json` to list ids"})
	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))
	require.Equal(t, "not_found", got["error"]["code"])
	require.NotEmpty(t, got["error"]["hint"])
}
