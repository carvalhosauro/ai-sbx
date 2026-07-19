// sbx/internal/driver/fake_test.go
package driver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFakeCreateListDestroy(t *testing.T) {
	ctx := context.Background()
	f := NewFake()

	e, err := f.Create(ctx, "sess1", EnvSpec{ComposePath: "compose.yml"})
	require.NoError(t, err)
	require.NotEmpty(t, e.ID)
	require.Equal(t, "running", e.Status)
	require.Contains(t, e.Namespace, "sess1"[:5])

	list, err := f.List(ctx, "sess1")
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, f.Destroy(ctx, e.ID))
	list, _ = f.List(ctx, "sess1")
	require.Len(t, list, 0)
}

func TestFakeDestroyUnknownIsError(t *testing.T) {
	require.Error(t, NewFake().Destroy(context.Background(), "nope"))
}

func TestFakeExecEchoesCmd(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	e, _ := f.Create(ctx, "s", EnvSpec{})
	r, err := f.Exec(ctx, e.ID, []string{"echo", "hi"})
	require.NoError(t, err)
	require.Equal(t, 0, r.ExitCode)
	require.Contains(t, r.Stdout, "echo hi")
}
