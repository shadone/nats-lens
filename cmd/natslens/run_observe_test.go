package main

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/store"
)

// startRunStore observes the store already owned by run; reopening the file
// during collection would rerun setup and obscure startup failures.
func startRunStore(t *testing.T, conf string) (*store.Store, func() error) {
	t.Helper()
	ready := make(chan api.Systems, 1)
	stop := startRun(t, conf, func(s api.Systems) { ready <- s })
	select {
	case reg := <-ready:
		sys, ok := reg.Default()
		if !ok {
			t.Fatal("run opened no system")
		}
		p, ok := sys.Provider.(*storeprov.Provider)
		if !ok {
			t.Fatalf("run provider %T is not a store provider", sys.Provider)
		}
		return p.St, stop
	case <-time.After(20 * time.Second):
		t.Fatal("run never opened its store")
		return nil, stop
	}
}

func waitForStoredCount(t *testing.T, st *store.Store, what, query string, minimum int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var n int
	for {
		if err := st.DB().QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatalf("waiting for %s (last count %d, want >= %d): %v", what, n, minimum, err)
		}
		if n >= minimum {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("never reached %s: count %d, want >= %d: %v", what, n, minimum, ctx.Err())
		case <-ticker.C:
		}
	}
}
