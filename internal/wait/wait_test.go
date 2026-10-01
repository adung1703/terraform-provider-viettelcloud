package wait

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// Every test runs inside a synctest bubble so the production-style timeout
// and interval are exercised on a fake clock instead of shortened durations.
const (
	testTimeout      = 30 * time.Second
	testPollInterval = 3 * time.Second
)

func testWaiter[T any]() *Waiter[T] {
	return New[T](testPollInterval, testTimeout)
}

func TestWaitForReturnsTheSuccessfulValue(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		start := time.Now()
		value, err := testWaiter[int]().WaitFor(context.Background(), func(context.Context) (int, error) {
			calls++
			if calls < 2 {
				return 0, fmt.Errorf("value not settled: %w", ErrNotReady)
			}
			return calls * 10, nil
		})
		if err != nil || calls != 2 {
			t.Fatalf("expected two polls, calls=%d error=%v", calls, err)
		}
		if value != 20 {
			t.Fatalf("expected the settled value, got %d", value)
		}
		if waited := time.Since(start); waited != testPollInterval {
			t.Fatalf("expected one interval between polls, waited %s", waited)
		}
	})
}

// A poll slower than the interval must still be followed by a full interval
// of rest so the provider does not hammer an already slow backend.
func TestWaitForSpacesPollsFromTheEndOfEachCall(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const pollDuration = 5 * time.Second

		start := time.Now()
		var started []time.Duration
		_, err := testWaiter[int]().WaitFor(context.Background(), func(context.Context) (int, error) {
			started = append(started, time.Since(start))
			time.Sleep(pollDuration)
			if len(started) < 3 {
				return 0, ErrNotReady
			}
			return len(started), nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []time.Duration{0, pollDuration + testPollInterval, 2 * (pollDuration + testPollInterval)}
		if len(started) != len(want) {
			t.Fatalf("expected %d polls, got %d at %v", len(want), len(started), started)
		}
		for i, at := range want {
			if started[i] != at {
				t.Fatalf("poll %d started at %s, want %s (all: %v)", i+1, started[i], at, started)
			}
		}
	})
}

func TestWaitForTimesOutWithTheLastNotReadyReason(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		start := time.Now()
		_, err := testWaiter[int]().WaitFor(context.Background(), func(context.Context) (int, error) {
			calls++
			return 0, fmt.Errorf("attempt %d still provisioning: %w", calls, ErrNotReady)
		})
		if !errors.Is(err, ErrTimeout) || !errors.Is(err, ErrNotReady) {
			t.Fatalf("expected timeout and not-ready markers, got %v", err)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("attempt %d still provisioning", calls)) {
			t.Fatalf("expected the last not-ready reason, got %q", err)
		}
		if waited := time.Since(start); waited != testTimeout {
			t.Fatalf("expected timeout after %s, waited %s", testTimeout, waited)
		}
	})
}

// Cancellation must win over the waiter's timeout, whether the poll function
// honors the context directly or reaches its own not-ready result.
func TestWaitForReportsCallerCancellation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		poll func(context.Context) (int, error)
	}{
		{
			name: "poll honors context",
			poll: func(ctx context.Context) (int, error) { return 0, ctx.Err() },
		},
		{
			name: "poll reports not ready",
			poll: func(context.Context) (int, error) { return 0, ErrNotReady },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := testWaiter[int]().WaitFor(ctx, test.poll)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation, got %v", err)
				}
				if errors.Is(err, ErrTimeout) {
					t.Fatalf("caller cancellation must not be reported as timeout, got %v", err)
				}
			})
		})
	}
}

func TestWaitForReportsTimeoutReachedInsidePoll(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		_, err := testWaiter[int]().WaitFor(context.Background(), func(ctx context.Context) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		})
		if !errors.Is(err, ErrTimeout) || errors.Is(err, context.Canceled) {
			t.Fatalf("expected waiter timeout, got %v", err)
		}
		if waited := time.Since(start); waited != testTimeout {
			t.Fatalf("expected timeout after %s, waited %s", testTimeout, waited)
		}
	})
}

func TestWaitForReturnsFatalErrorAsIs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		backendErr := errors.New("backend unavailable")
		start := time.Now()
		_, err := testWaiter[int]().WaitFor(context.Background(), func(context.Context) (int, error) {
			return 0, backendErr
		})
		if err != backendErr {
			t.Fatalf("expected the original fatal error, got %v", err)
		}
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("expected failure on the first poll, waited %s", waited)
		}
	})
}

// A caller can classify a terminal state as a fatal error instead of waiting
// for a timeout.
func TestWaitForStopsOnTerminalState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		terminalErr := errors.New("entered error state")
		calls := 0
		_, err := testWaiter[string]().WaitFor(context.Background(), func(context.Context) (string, error) {
			calls++
			return "error", terminalErr
		})
		if err != terminalErr || calls != 1 {
			t.Fatalf("expected immediate terminal failure, calls=%d error=%v", calls, err)
		}
	})
}

// A delete poll can classify not-found as success by returning a nil error.
func TestWaitForLetsCallerTreatNotFoundAsSuccess(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		errNotFound := errors.New("not found")
		calls := 0
		get := func(context.Context) (int, error) {
			calls++
			if calls < 3 {
				return calls, nil
			}
			return 0, errNotFound
		}

		start := time.Now()
		_, err := testWaiter[int]().WaitFor(context.Background(), func(ctx context.Context) (int, error) {
			value, err := get(ctx)
			if errors.Is(err, errNotFound) {
				return 0, nil
			}
			if err != nil {
				return 0, err
			}
			return value, ErrNotReady
		})
		if err != nil || calls != 3 {
			t.Fatalf("expected success when the object disappeared, calls=%d error=%v", calls, err)
		}
		if waited := time.Since(start); waited != 2*testPollInterval {
			t.Fatalf("expected two intervals before success, waited %s", waited)
		}
	})
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		interval time.Duration
		timeout  time.Duration
	}{
		{name: "zero values"},
		{name: "negative values", interval: -time.Second, timeout: -time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			waiter := New[int](test.interval, test.timeout)
			if waiter.interval != defaultInterval || waiter.timeout != defaultTimeout {
				t.Fatalf("expected defaults %s/%s, got %s/%s", defaultInterval, defaultTimeout, waiter.interval, waiter.timeout)
			}
		})
	}
}

func TestNewKeepsPositiveDurations(t *testing.T) {
	t.Parallel()

	waiter := New[int](testPollInterval, testTimeout)
	if waiter.interval != testPollInterval || waiter.timeout != testTimeout {
		t.Fatalf("expected %s/%s, got %s/%s", testPollInterval, testTimeout, waiter.interval, waiter.timeout)
	}
}
