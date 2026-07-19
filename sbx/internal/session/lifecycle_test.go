package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitFiresOnCancel(t *testing.T) {
	var n atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Wait(ctx, time.Time{}, func(context.Context) error {
			n.Add(1)
			return nil
		})
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	require.Equal(t, int32(1), n.Load())
}

func TestWaitFiresOnDeadline(t *testing.T) {
	var n atomic.Int32
	ctx := context.Background()
	deadline := time.Now().Add(50 * time.Millisecond)
	err := Wait(ctx, deadline, func(context.Context) error {
		n.Add(1)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), n.Load())
}

func TestWaitCleanupOnlyOnce(t *testing.T) {
	var n atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	deadline := time.Now().Add(50 * time.Millisecond)
	done := make(chan error, 1)
	go func() {
		done <- Wait(ctx, deadline, func(context.Context) error {
			n.Add(1)
			return nil
		})
	}()
	cancel()
	require.NoError(t, <-done)
	time.Sleep(80 * time.Millisecond) // deadline também passaria
	require.Equal(t, int32(1), n.Load())
}
