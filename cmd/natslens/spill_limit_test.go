package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/config"
)

// stubFree stubs the free disk for a test; the returned func restores it.
func stubFree(t *testing.T, free int64) {
	t.Helper()
	orig := freeDisk
	t.Cleanup(func() { freeDisk = orig })
	freeDisk = func(string) (int64, error) { return free, nil }
}

// Each system's database gets its spill bound when it opens: its own
// duckdb_max_temp_size, else its share of the memory budget times the
// reads its query budget admits plus one: twice the share at 2 threads.
func TestOpenSystemsBoundTheSpill(t *testing.T) {
	stubFree(t, 1<<40)
	procs := runtimeProcs
	t.Cleanup(func() { runtimeProcs = procs })
	runtimeProcs = func() int { return 4 }
	ctx := context.Background()
	cfg := listConfig(t, t.TempDir(), "shop-prod", "shop-staging")
	cfg.DuckDBMemory = 600 << 20
	cfg.Systems[0].DuckDBMaxTemp = 100 << 20
	rts, err := openSystems(ctx, cfg, cfg.ResolvedSystems(), quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSystems(rts, time.Time{})
	want := map[string]string{"shop-prod": "100.0 MiB", "shop-staging": "600.0 MiB"}
	for _, rt := range rts {
		var got string
		if err := rt.st.DB().QueryRow(`SELECT current_setting('max_temp_directory_size')`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want[rt.sys.ID] {
			t.Errorf("%s: max_temp_directory_size = %q, want %q", rt.sys.ID, got, want[rt.sys.ID])
		}
	}
}

// The derived bound grows with the reads the budget admits and is lowered
// to 90% of the free disk; a configured bound is kept as it is.
func TestStoreLimits(t *testing.T) {
	cfg := config.Config{DuckDBMemory: 1 << 30}
	stubFree(t, 1<<40)
	if got := storeLimits(cfg, 12, t.TempDir()); got.memory != 1<<30 || got.spill != 5<<30 {
		t.Errorf("12 threads: %+v, want 1 GiB and 5 GiB", got)
	}
	stubFree(t, 2<<30)
	if got := storeLimits(cfg, 12, t.TempDir()).spill; got != (2<<30)*9/10 {
		t.Errorf("2 GiB free: spill %d, want 90%% of it", got)
	}
	cfg.DuckDBMaxTemp = 3 << 30
	if got := storeLimits(cfg, 12, t.TempDir()).spill; got != 3<<30 {
		t.Errorf("configured: spill %d, want 3 GiB unlowered", got)
	}
	freeDisk = func(string) (int64, error) { return 0, errors.New("no statfs") }
	cfg.DuckDBMaxTemp = 0
	if got := storeLimits(cfg, 2, t.TempDir()).spill; got != 2<<30 {
		t.Errorf("free disk unknown: spill %d, want 2 GiB", got)
	}
}

// A rebuild from the archive loads into a store with the system's memory
// limit and spill bound.
func TestRebuildIsBounded(t *testing.T) {
	ctx := context.Background()
	orig := rebuildFrom
	t.Cleanup(func() { rebuildFrom = orig })
	var mem, spill string
	rebuildFrom = func(ctx context.Context, a *archive.Archiver) error {
		if err := a.Store.DB().QueryRowContext(ctx, `SELECT current_setting('memory_limit'), current_setting('max_temp_directory_size')`).Scan(&mem, &spill); err != nil {
			return err
		}
		return archive.ErrEmptyArchive
	}
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = dir
	err := rebuild(ctx, cfg, archive.LocalDir{Dir: filepath.Join(dir, "archive")}, filepath.Join(dir, "natslens.duckdb"), limits{memory: 300 << 20, spill: 700 << 20}, quietLog())
	if !errors.Is(err, archive.ErrEmptyArchive) {
		t.Fatalf("rebuild: %v", err)
	}
	if mem != "300.0 MiB" || spill != "700.0 MiB" {
		t.Errorf("rebuild store: memory_limit %q, max_temp_directory_size %q; want 300.0 MiB and 700.0 MiB", mem, spill)
	}
}
