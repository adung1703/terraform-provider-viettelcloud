// Package wait repeatedly checks an asynchronous backend operation until it
// settles. Deadline derivation, cancellation, timeout attribution, poll spacing
// and not-ready reporting are identical for every resource, so they live here
// rather than in each one.
//
// The SDK owns HTTP retry; this is not a retry loop.
package wait

// settles. Deadline derivation, cancellation, timeout attribution, poll spacing
// and not-ready reporting are identical for every resource, so they live here
// rather than in each one. The SDK owns HTTP retry; this is not a retry loop.

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultInterval = 5 * time.Second
	defaultTimeout  = 10 * time.Minute
)

// ErrNotReady tells the waiter the resource is not in its target state yet and
// polling should continue. Wrap it to explain why; the reason surfaces in the
// timeout error.
//
//	return nil, fmt.Errorf("status=%s: %w", status, waiter.ErrNotReady)
var ErrNotReady = errors.New("not ready")

// ErrTimeout marks a wait whose own timeout elapsed.
var ErrTimeout = errors.New("timeout")

// Waiter repeatedly runs a poll function until it succeeds.
type Waiter[T any] struct {
	interval time.Duration
	timeout  time.Duration
}

// New returns a Waiter that polls every interval and gives up after
// timeout. Non-positive values fall back to five seconds and ten minutes.
func New[T any](interval, timeout time.Duration) *Waiter[T] {
	if interval <= 0 {
		interval = defaultInterval
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Waiter[T]{interval: interval, timeout: timeout}
}

// WaitFor calls poll until it returns a value with a nil error.
//
// The poll function classifies its own outcomes:
//   - (value, nil) means done and value is returned.
//   - (_, ErrNotReady) means keep polling; wrap it to record why.
//   - (_, any other error) means fatal and is returned as-is.
func (w *Waiter[T]) WaitFor(
	ctx context.Context,
	poll func(context.Context) (T, error),
) (T, error) {
	var zero T

	waitCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	timer := time.NewTimer(w.interval)
	defer timer.Stop()

	// The last reason the object was not ready is included in a timeout error.
	var lastErr error

	for {
		value, err := poll(waitCtx)

		switch {
		case err == nil:
			return value, nil
		case errors.Is(err, ErrNotReady):
			lastErr = err
		case waitCtx.Err() != nil:
			return zero, w.fail(ctx, lastErr)
		default:
			return zero, err
		}

		// Reset after poll returns so interval is the gap between calls rather
		// than a fixed schedule that a slow API can outrun.
		resetTimer(timer, w.interval)

		select {
		case <-waitCtx.Done():
			return zero, w.fail(ctx, lastErr)
		case <-timer.C:
		}
	}
}

func (w *Waiter[T]) fail(ctx context.Context, lastErr error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("wait cancelled: %w", ctx.Err())
	}
	if lastErr != nil {
		return fmt.Errorf("%w after %s: %w", ErrTimeout, w.timeout, lastErr)
	}
	return fmt.Errorf("%w after %s", ErrTimeout, w.timeout)
}

func resetTimer(timer *time.Timer, interval time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(interval)
}
