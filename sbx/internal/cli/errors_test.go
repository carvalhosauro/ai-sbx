// sbx/internal/cli/errors_test.go
package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gustavocarvalho/sbx/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestWriteErrorMapsDriverError(t *testing.T) {
	var b bytes.Buffer
	writeError(&b, true, driver.DriverError{Code: "engine_missing", Message: "podman not found", Hint: "install podman"})
	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))
	require.Equal(t, "engine_missing", got["error"]["code"])
	require.Equal(t, "install podman", got["error"]["hint"])
}
