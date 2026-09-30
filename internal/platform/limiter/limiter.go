// Package limiter bounds how many calls run at once against a backend.
package limiter

import (
	"context"
	"fmt"
)

// Limiter is a counting semaphore. Each AI client owns its own Limiter so
// their limits are configured separately.
type Limiter struct {
	slots chan struct{}
}

// New returns a Limiter that allows at most n concurrent calls.
func New(n int) (*Limiter, error) {
	if n < 1 {
		return nil, fmt.Errorf("limiter: max concurrency must be >= 1, got %d", n)
	}
	return &Limiter{slots: make(chan struct{}, n)}, nil
}

// Do runs fn once a slot is free, or returns ctx.Err() if ctx ends first.
func (l *Limiter) Do(ctx context.Context, fn func(context.Context) error) error {
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-l.slots }()
	return fn(ctx)
}

// Cap reports the maximum number of concurrent calls.
func (l *Limiter) Cap() int { return cap(l.slots) }
