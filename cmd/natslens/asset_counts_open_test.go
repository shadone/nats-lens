package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
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
	// 13 threads per store: a share no machine's default is likely to be,
	// so the settings during the fill tell the configured ones apart.
	procs := runtimeProcs
	t.Cleanup(func() { runtimeProcs = procs })
	runtimeProcs = func() int { return 26 }
	if runtime.NumCPU() == 13 {
		t.Skip("13 CPUs: DuckDB's default thread count equals the share")
	}
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
		if after.threads != 13 {
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

// A fill that fails leaves the store open: the count series read the
// reports until a later start fills them, and the other systems open as
// usual.
func TestAssetCountsFillFailureKeepsTheSystem(t *testing.T) {
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	for _, s := range cfg.ResolvedSystems() {
		storeWithoutCounts(t, s.DataDir)
	}
	failing := cfg.ResolvedSystems()[0].ID
	orig := fillAssetCounts
	t.Cleanup(func() { fillAssetCounts = orig })
	fillAssetCounts = func(st *store.Store, ctx context.Context) (int64, error) {
		if st.System() == failing {
			return 0, errors.New("disk full")
		}
		return orig(st, ctx)
	}
	var logs bytes.Buffer
	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer closeSystems(rts, time.Time{})
	for _, rt := range rts {
		want := 1
		if rt.sys.ID == failing {
			want = 0
		}
		if n := countRows(t, rt.st); n != want {
			t.Errorf("%s: %d epochs with counts, want %d", rt.sys.ID, n, want)
		}
	}
	if !strings.Contains(logs.String(), "disk full") {
		t.Errorf("the failure was not logged: %s", logs.String())
	}
}
