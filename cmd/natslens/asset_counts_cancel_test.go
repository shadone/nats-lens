package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// A fill whose start was cancelled returns the fill's own error,
// not the later inventory load's, so the ctx.Err() clause is what stops it.
func TestAssetCountsFillCancelledReturnsTheFillError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := listConfig(t, t.TempDir(), "shop-prod")
	sys := cfg.ResolvedSystems()[0]
	storeWithoutCounts(t, sys.DataDir)
	sentinel := errors.New("fill stopped")
	orig := fillAssetCounts
	t.Cleanup(func() { fillAssetCounts = orig })
	fillAssetCounts = func(_ *store.Store, ctx context.Context) (int64, error) {
		cancel()
		return 0, fmt.Errorf("%w: %w", sentinel, ctx.Err())
	}
	rt := newSystemRuntime(cfg, sys, quietLog())
	err := rt.open(ctx, 2)
	defer closeSystems([]*systemRuntime{rt}, time.Time{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("open = %v, want the fill's error", err)
	}
}
