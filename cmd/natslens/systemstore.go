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
	"time"

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
	if err := writeSynced(tmp, append(b, '\n')); err != nil {
		return fmt.Errorf("system %s: write %s: %w", sys.ID, markerFile, err)
	}
	return os.Rename(tmp, path)
}

// writeSynced writes a file and syncs it, so that a rename after it never
// leaves an empty file behind a crash.
func writeSynced(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// threadsPerStore is each database's thread count when n databases share
// procs CPUs: an equal share, at least two.
func threadsPerStore(procs, n int) int {
	return max(2, procs/max(n, 1))
}

// openSystems opens every system's store in parallel, each with its own
// rebuild, and loads its inventory. A store that fails to open fails the
// start: the first failure cancels the other opens, so that a refusal
// does not wait for another system's long rebuild, the stores already open
// are closed again, and the failures other than the cancellations are
// returned.
func openSystems(ctx context.Context, cfg config.Config, systems []config.System, log *slog.Logger) ([]*systemRuntime, error) {
	threads := threadsPerStore(runtimeProcs(), len(systems))
	octx, cancel := context.WithCancel(ctx)
	defer cancel()
	rts := make([]*systemRuntime, len(systems))
	errs := make([]error, len(systems))
	var wg sync.WaitGroup
	for i, sys := range systems {
		rt := newSystemRuntime(cfg, sys, log)
		rts[i] = rt
		wg.Go(func() {
			if errs[i] = openRuntime(rt, octx, threads); errs[i] != nil {
				cancel()
			}
		})
	}
	wg.Wait()
	var failed []error
	for _, err := range errs {
		if err != nil && (ctx.Err() != nil || !errors.Is(err, context.Canceled)) {
			failed = append(failed, err)
		}
	}
	if len(failed) == 0 && octx.Err() != nil && ctx.Err() == nil {
		// Every failure was a cancellation; report them rather than none.
		failed = errs
	}
	if err := errors.Join(failed...); err != nil {
		closeSystems(rts, time.Time{})
		return nil, err
	}
	return rts, nil
}

// open opens the system's store and archive and loads its inventory.
func (rt *systemRuntime) open(ctx context.Context, threads int) error {
	if err := checkMarker(rt.sys); err != nil {
		return err
	}
	// The default archive directory is inside the data directory by
	// design; only a directory set there on purpose is warned about.
	if rt.archiveDirSet && within(rt.sys.ArchiveDir, rt.sys.DataDir) {
		rt.log.Warn("archive_dir is inside data_dir: losing the data volume loses the archive too",
			"archive_dir", rt.sys.ArchiveDir, "data_dir", rt.sys.DataDir)
	}
	// The marker goes first: a database without one would be adopted by
	// the next start under any id.
	if err := writeMarker(rt.sys); err != nil {
		return err
	}
	st, arch, err := openStore(ctx, rt.cfg, rt.log)
	if err != nil {
		return fmt.Errorf("system %s: %w", rt.sys.ID, err)
	}
	rt.st, rt.arch = st, arch
	st.SetSystem(rt.sys.ID)
	if err := st.SetThreads(ctx, threads); err != nil {
		return fmt.Errorf("system %s: %w", rt.sys.ID, err)
	}
	budget := rt.cfg.QueryBudgetFor(threads)
	logged := any("off")
	if budget > 0 {
		st.SetQueryBudget(budget)
		logged = budget
	}
	rt.log.Info("duckdb threads", "threads", threads, "query_budget", logged)
	// Epochs stored without asset counts get them now, within the
	// store's thread count and memory limit. A failed fill leaves the
	// count series reading the reports, slower but correct, so it stops
	// the start only when the start was cancelled or the engine failed.
	if _, err := fillAssetCounts(st, ctx); err != nil {
		if ctx.Err() != nil || st.Failure() != nil {
			return fmt.Errorf("system %s: %w", rt.sys.ID, err)
		}
		rt.log.Warn("asset counts not filled; the count series read the reports until a later start fills them", "err", err)
	}
	if err := loadInventory(ctx, rt.cfg, st, rt.log); err != nil {
		return fmt.Errorf("system %s: %w", rt.sys.ID, err)
	}
	return nil
}

// fillAssetCounts fills the asset counts a store lacks; a variable so that
// tests can see when it runs.
var fillAssetCounts = (*store.Store).FillAssetCounts

// openRuntime opens one system; a variable so that tests can slow it.
var openRuntime = (*systemRuntime).open

// closeStore closes one store; a variable so that tests can slow it.
var closeStore = (*store.Store).Close

// closeSystems closes every open store, all at once, and waits for them
// until by; a zero by waits for every close. A close still running at by
// is abandoned: the process is about to exit, and the next start replays
// the database's write-ahead log.
func closeSystems(rts []*systemRuntime, by time.Time) {
	var wg sync.WaitGroup
	for _, rt := range rts {
		if rt == nil || rt.st == nil {
			continue
		}
		st, log := rt.st, rt.log
		rt.st = nil
		wg.Go(func() {
			if err := closeStore(st); err != nil {
				log.Error("database not closed cleanly", "err", err)
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	if by.IsZero() {
		<-done
		return
	}
	if !waitUntil(done, by) {
		for _, rt := range rts {
			if rt != nil {
				rt.log.Warn("database still closing at the shutdown deadline; abandoned")
				break
			}
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
