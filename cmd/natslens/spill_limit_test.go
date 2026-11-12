package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
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
	if got := storeLimits(cfg, 12, t.TempDir(), quietLog()); got.memory != 1<<30 || got.spill != 5<<30 {
		t.Errorf("12 threads: %+v, want 1 GiB and 5 GiB", got)
	}
	stubFree(t, 2<<30)
	if got := storeLimits(cfg, 12, t.TempDir(), quietLog()).spill; got != (2<<30)*9/10 {
		t.Errorf("2 GiB free: spill %d, want 90%% of it", got)
	}
	cfg.DuckDBMaxTemp = 3 << 30
	if got := storeLimits(cfg, 12, t.TempDir(), quietLog()).spill; got != 3<<30 {
		t.Errorf("configured: spill %d, want 3 GiB unlowered", got)
	}
	freeDisk = func(string) (int64, error) { return 0, errors.New("no statfs") }
	cfg.DuckDBMaxTemp = 0
	if got := storeLimits(cfg, 2, t.TempDir(), quietLog()).spill; got != 2<<30 {
		t.Errorf("free disk unknown: spill %d, want 2 GiB", got)
	}
}

// On a nearly full disk the derived bound is lowered to 1 MiB, never left
// at its full size; the lowering is logged with the free bytes.
func TestStoreLimitsNearlyFullDisk(t *testing.T) {
	cfg := config.Config{DuckDBMemory: 1 << 30}
	for _, free := range []int64{0, 512 << 10, 1 << 20} {
		stubFree(t, free)
		var logs bytes.Buffer
		if got := storeLimits(cfg, 2, t.TempDir(), slog.New(slog.NewTextHandler(&logs, nil))).spill; got != 1<<20 {
			t.Errorf("%d bytes free: spill %d, want 1 MiB", free, got)
		}
		if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "free_bytes="+strconv.FormatInt(free, 10)) ||
			!strings.Contains(logs.String(), "derived_bytes=2147483648") {
			t.Errorf("%d bytes free: log %q", free, logs.String())
		}
	}
	stubFree(t, 1<<40)
	var logs bytes.Buffer
	_ = storeLimits(cfg, 2, t.TempDir(), slog.New(slog.NewTextHandler(&logs, nil)))
	if logs.Len() != 0 {
		t.Errorf("logged without lowering: %q", logs.String())
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

// After a rebuild the final store's bound reads the free disk again: the
// rebuilt database took space the first read counted.
func TestBoundReadAgainAfterRebuild(t *testing.T) {
	ctx := context.Background()
	orig, origFree := rebuildFrom, freeDisk
	t.Cleanup(func() { rebuildFrom, freeDisk = orig, origFree })
	reads := 0
	freeDisk = func(string) (int64, error) {
		reads++
		if reads == 1 {
			return 1 << 40, nil
		}
		return 100 << 20, nil
	}
	rebuildFrom = func(context.Context, *archive.Archiver) error { return nil }
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DuckDBMemory = 256 << 20
	st, _, err := openStore(ctx, cfg, 2, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if want := int64(100<<20) * 9 / 10; st.MaxTempSize() != want {
		t.Errorf("final store's bound %d, want %d, 90%% of the free disk after the rebuild", st.MaxTempSize(), want)
	}
}
