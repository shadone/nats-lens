package main

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// An epoch without asset counts, as a release before them stored it, in
// the data directory of each system.
func storeWithoutCounts(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	if _, err := st.DB().Exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers, expected_names, observed_names,
    missing_names, endpoint_failures, partial, errors) VALUES (1, ?, ?, 400, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [])`,
		at, at.Add(400*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM epoch_asset_counts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The probe that asks whether a database is empty does not fill asset
// counts; the system's open fills them once its thread count and memory
// limit are set, so that systems upgrading at once fill within their
// shares.
func TestAssetCountsFilledAfterTheLimits(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	cfg.DuckDBMemory = 300 << 20
	for _, s := range cfg.ResolvedSystems() {
		storeWithoutCounts(t, s.DataDir)
	}
	sys := cfg.ResolvedSystems()[0]
	if empty, err := isEmpty(ctx, filepath.Join(sys.DataDir, store.FileName), sys.ID); err != nil || empty {
		t.Fatalf("isEmpty = %v, %v", empty, err)
	}
	probe, err := store.OpenPath(ctx, filepath.Join(sys.DataDir, store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, probe); n != 0 {
		t.Errorf("opening the store filled %d epochs' counts", n)
	}
	probe.Close()

	type settings struct {
		threads int
		memory  string
	}
	var mu sync.Mutex
	var during []settings
	orig := fillAssetCounts
	t.Cleanup(func() { fillAssetCounts = orig })
	fillAssetCounts = func(st *store.Store, ctx context.Context) (int64, error) {
		var s settings
		if err := st.DB().QueryRow(`SELECT current_setting('threads'), current_setting('memory_limit')`).Scan(&s.threads, &s.memory); err != nil {
			return 0, err
		}
		mu.Lock()
		during = append(during, s)
		mu.Unlock()
		return orig(st, ctx)
	}
	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSystems(rts, time.Time{})
	if len(during) != 2 {
		t.Fatalf("filled %d stores, want 2", len(during))
	}
	for i, rt := range rts {
		var after settings
		if err := rt.st.DB().QueryRow(`SELECT current_setting('threads'), current_setting('memory_limit')`).Scan(&after.threads, &after.memory); err != nil {
			t.Fatal(err)
		}
		if after.threads != threadsPerStore(runtimeProcs(), 2) {
			t.Fatalf("threads after open %d", after.threads)
		}
		found := false
		for _, d := range during {
			found = found || d == after
		}
		if !found {
			t.Errorf("system %d: settings during the fills %v, after the open %v", i, during, after)
		}
		if n := countRows(t, rt.st); n != 1 {
			t.Errorf("system %d: %d epochs with counts after the open, want 1", i, n)
		}
	}
}
