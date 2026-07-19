package session

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func Wait(ctx context.Context, deadline time.Time, cleanup func(context.Context) error) error {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	var timer <-chan time.Time
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d < 0 {
			d = 0
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timer = t.C
	}

	select {
	case <-sigCtx.Done():
	case <-timer:
	}

	var once sync.Once
	var err error
	once.Do(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err = cleanup(cctx)
	})
	return err
}

// AlivePID reports whether a process with pid appears alive (signal 0).
func AlivePID(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
