package limiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRejectsNonPositive(t *testing.T) {
	if _, err := New(0); err == nil {
		t.Fatal("New(0): want error, got nil")
	}
}

func TestDoBoundsConcurrency(t *testing.T) {
	const max = 3
	l, err := New(max)
	if err != nil {
		t.Fatal(err)
	}

	var running, peak atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_ = l.Do(context.Background(), func(context.Context) error {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				running.Add(-1)
				return nil
			})
		})
	}
	wg.Wait()

	if got := peak.Load(); got > max {
		t.Fatalf("peak concurrency = %d, want <= %d", got, max)
	}
}

func TestDoReturnsContextErrorWhileWaiting(t *testing.T) {
	l, err := New(1)
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	go func() {
		_ = l.Do(context.Background(), func(context.Context) error {
			<-release
			return nil
		})
	}()
	defer close(release)

	// Wait until the slot is taken.
	for len(l.slots) == 0 {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	called := false
	err = l.Do(ctx, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if called {
		t.Fatal("fn ran although no slot was free")
	}
}
