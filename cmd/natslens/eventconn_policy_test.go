package main

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nats-io/nats.go"
)

// Failed dials keep doubling to the cap even after reset has elapsed.
// Virtual time tests the policy without a server or wall-clock tolerances;
// TestRedialBacksOffWhenDialFails retains the real connection transition.
func TestRedialBackoffPolicy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		start := time.Now()
		var attempts []time.Duration
		ec := &eventConn{label: "shop", log: quietLog(),
			backoff: redialBackoff{first: 20 * time.Millisecond, max: 400 * time.Millisecond, reset: 100 * time.Millisecond},
			dial: func() (*nats.Conn, error) {
				attempts = append(attempts, time.Since(start))
				if len(attempts) == 6 {
					cancel()
				}
				return nil, errors.New("credentials unreadable")
			},
		}
		done := make(chan struct{})
		go func() { defer close(done); ec.keep(ctx, time.Millisecond) }()
		<-done
		want := []time.Duration{20 * time.Millisecond, 60 * time.Millisecond, 140 * time.Millisecond, 300 * time.Millisecond, 620 * time.Millisecond, 1020 * time.Millisecond}
		if !slices.Equal(attempts, want) {
			t.Fatalf("dial schedule %v, want %v", attempts, want)
		}
	})
}
