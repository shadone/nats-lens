package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// markerFile names the file in a system's data directory that records
// which system the directory belongs to.
const markerFile = "system.json"

type marker struct {
	ID string `json:"id"`
}

// checkMarker refuses a data directory marked for another system. A
// directory without a marker, as a single-system nats-lens left it, is
// adopted.
func checkMarker(sys config.System) error {
	b, err := os.ReadFile(filepath.Join(sys.DataDir, markerFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("system %s: read %s: %w", sys.ID, markerFile, err)
	}
	var m marker
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("system %s: %s in %s: %w", sys.ID, markerFile, sys.DataDir, err)
	}
	if m.ID != sys.ID {
		return fmt.Errorf("system %s: data directory %s belongs to system %q (its %s); give the system that id, "+
			"or another data_dir", sys.ID, sys.DataDir, m.ID, markerFile)
	}
	return nil
}

// writeMarker records the system's id in its data directory once.
func writeMarker(sys config.System) error {
	path := filepath.Join(sys.DataDir, markerFile)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	b, err := json.Marshal(marker{ID: sys.ID})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sys.DataDir, 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("system %s: write %s: %w", sys.ID, markerFile, err)
	}
	return os.Rename(tmp, path)
}

// threadsPerStore is each database's thread count when n databases share
// procs CPUs: an equal share, at least two.
func threadsPerStore(procs, n int) int {
	return max(2, procs/max(n, 1))
}

// openSystems opens every system's store in parallel, each with its own
// rebuild, and loads its inventory. A store that fails to open fails the
// start: the others are closed again and the errors are returned.
func openSystems(ctx context.Context, cfg config.Config, systems []config.System, log *slog.Logger) ([]*systemRuntime, error) {
	threads := threadsPerStore(runtimeProcs(), len(systems))
	rts := make([]*systemRuntime, len(systems))
	errs := make([]error, len(systems))
	var wg sync.WaitGroup
	for i, sys := range systems {
		rt := newSystemRuntime(cfg, sys, log)
		rts[i] = rt
		wg.Go(func() { errs[i] = rt.open(ctx, threads) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		closeSystems(rts)
		return nil, err
	}
	return rts, nil
}

// open opens the system's store and archive and loads its inventory.
func (rt *systemRuntime) open(ctx context.Context, threads int) error {
	if err := checkMarker(rt.sys); err != nil {
		return err
	}
	st, arch, err := openStore(ctx, rt.cfg, rt.log)
	if err != nil {
		return fmt.Errorf("system %s: %w", rt.sys.ID, err)
	}
	rt.st, rt.arch = st, arch
	if err := st.SetThreads(ctx, threads); err != nil {
		return fmt.Errorf("system %s: %w", rt.sys.ID, err)
	}
	rt.log.Info("duckdb threads", "threads", threads)
	if err := writeMarker(rt.sys); err != nil {
		return err
	}
	if err := loadInventory(ctx, rt.cfg, st, rt.log); err != nil {
		return fmt.Errorf("system %s: %w", rt.sys.ID, err)
	}
	return nil
}

// closeSystems closes every store that is open.
func closeSystems(rts []*systemRuntime) {
	for _, rt := range rts {
		if rt != nil && rt.st != nil {
			if err := rt.st.Close(); err != nil {
				rt.log.Error("database not closed cleanly", "err", err)
			}
			rt.st = nil
		}
	}
}

// warnUnusedDatabase reports a single-system database in the root data_dir
// that none of the listed systems uses: its data is no longer read.
func warnUnusedDatabase(cfg config.Config, log *slog.Logger) {
	if len(cfg.Systems) == 0 {
		return
	}
	path := filepath.Join(cfg.DataDir, store.FileName)
	if _, err := os.Stat(path); err != nil {
		return
	}
	for _, s := range cfg.ResolvedSystems() {
		if sameDir(s.DataDir, cfg.DataDir) {
			return
		}
	}
	log.Warn("a single-system database is in data_dir and no system uses it; set a system's data_dir to "+
		"this directory (and archive_dir to its archive) to keep its data", "database", path)
}

// sameDir reports whether two paths name the same directory.
func sameDir(a, b string) bool {
	return within(a, b) && within(b, a)
}
