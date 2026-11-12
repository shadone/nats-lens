package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	"github.com/shadone/nats-lens/internal/store"
)

// rebuildSuffix names the file a rebuild writes before it replaces the
// database, so that a failed rebuild never leaves a database behind.
const rebuildSuffix = ".rebuild"

// freeDisk reads the free bytes of the disk holding a directory; a
// variable so that tests can set it.
var freeDisk = statFree

// limits are the memory and spill bounds a store of the system opens with.
type limits struct{ memory, spill int64 }

// storeLimits returns the system's DuckDB memory limit and spill bound
// for a database with threads. A derived spill bound is lowered to 90% of
// the free space of dir's disk, DuckDB's own bound, when that is less; a
// configured duckdb_max_temp_size is kept as it is.
func storeLimits(cfg config.Config, threads int, dir string) limits {
	l := limits{memory: cfg.DuckDBMemoryLimit(), spill: cfg.DuckDBMaxTempSizeFor(threads)}
	if cfg.DuckDBMaxTemp > 0 {
		return l
	}
	if free, err := freeDisk(dir); err == nil && free*9/10 >= 1<<20 {
		l.spill = min(l.spill, free*9/10)
	}
	return l
}

// apply sets the bounds on a store.
func (l limits) apply(ctx context.Context, st *store.Store) error {
	if err := st.SetMemoryLimit(ctx, l.memory); err != nil {
		return err
	}
	return st.SetMaxTempSize(ctx, l.spill)
}

// openStore opens the database. When the file is missing, or holds no
// epoch and no event, and the archive has data inside the retention
// window, the database is first rebuilt from the archive into a separate
// file that replaces it only once the rebuild succeeded.
//
// The database, and a rebuild's, gets the system's memory limit and the
// spill bound of a database with threads.
func openStore(ctx context.Context, cfg config.Config, threads int, log *slog.Logger) (*store.Store, *archive.Archiver, error) {
	dest := archive.LocalDir{Dir: cfg.ResolvedArchiveDir()}
	dbPath := filepath.Join(cfg.DataDir, store.FileName)
	lim := storeLimits(cfg, threads, cfg.DataDir)
	empty, err := isEmpty(ctx, dbPath, cfg.SystemID)
	var recovery *events.Event
	if errors.Is(err, store.ErrWALReplay) {
		// The file will never open again. Keep it for inspection and start
		// over from the archive.
		aside, merr := moveAside(dbPath, time.Now())
		if merr != nil {
			return nil, nil, fmt.Errorf("move aside a database that cannot be opened: %w (after %v)", merr, err)
		}
		log.Error("database cannot be opened: its write-ahead log cannot be replayed; moved it aside and rebuilding from the archive",
			"err", err, "moved_to", aside)
		now := time.Now().UTC()
		recovery = &events.Event{Time: now, ReceivedAt: now, Source: events.SourceCollector, Kind: "collector.database_recovered",
			Reason: err.Error(), Raw: json.RawMessage(`{}`),
			Summary: "database could not be opened (write-ahead log replay failed); kept as " + strings.Join(aside, ", ") +
				" and rebuilt from the archive"}
		empty, err = true, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if empty {
		switch err := rebuild(ctx, cfg, dest, dbPath, lim, log); {
		case errors.Is(err, archive.ErrEmptyArchive):
			log.Info("new database", "archive_dir", cfg.ResolvedArchiveDir())
		case err != nil:
			return nil, nil, fmt.Errorf("rebuild from archive %s: %w", cfg.ResolvedArchiveDir(), err)
		}
	}
	st, err := store.Open(ctx, cfg.DataDir, store.WithSystem(cfg.SystemID))
	if err != nil {
		return nil, nil, err
	}
	if err := lim.apply(ctx, st); err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	log.Info("duckdb memory limit", "bytes", lim.memory)
	log.Info("duckdb spill limit", "bytes", lim.spill)
	st.SetLogger(log)
	if recovery != nil {
		if err := st.WriteEvents(ctx, []events.Event{*recovery}); err != nil {
			log.Error("recovery event not stored", "err", err)
		}
	}
	return st, newArchiver(cfg, st, dest, log), nil
}

// moveAside renames a database file and its write-ahead log to
// <name>.corrupt-<UTC time>, never deleting either, and returns the new
// names of the files that existed.
func moveAside(path string, now time.Time) ([]string, error) {
	suffix := ".corrupt-" + now.UTC().Format("20060102T150405Z")
	var moved []string
	// The log goes first: a log left beside a new database would be
	// replayed into it.
	for _, p := range []string{path + ".wal", path} {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Rename(p, p+suffix); err != nil {
			return moved, err
		}
		moved = append(moved, p+suffix)
	}
	return moved, nil
}

func newArchiver(cfg config.Config, st *store.Store, dest archive.Destination, log *slog.Logger) *archive.Archiver {
	return &archive.Archiver{
		Store: st, Dest: dest, Retention: cfg.Retention.D(), Grace: archiveGrace(cfg),
		ProvisionalEvery: cfg.ArchiveProvisionalEvery.D(), MaxHoursPerTick: archive.DefaultMaxHoursPerTick,
		MinCleanupAge: archive.DefaultMinCleanupAge, ArchiveRetention: cfg.ArchiveRetention.D(), Log: log,
	}
}

// archiveGrace is how long an hour stays open after it ends: two epoch
// intervals, so that its last epoch is written, two event flushes, so that
// its last event batch is written, and at least a minute.
func archiveGrace(cfg config.Config) time.Duration {
	return max(2*cfg.Interval.D(), 2*cfg.Events.FlushInterval.D(), time.Minute)
}

// isEmpty reports whether the database file is missing or holds no epoch
// and no event.
func isEmpty(ctx context.Context, path, system string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	st, err := store.OpenPath(ctx, path, store.WithSystem(system))
	if err != nil {
		return false, err
	}
	defer st.Close()
	var n int64
	if err := st.DB().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM epochs) + (SELECT count(*) FROM events)`).Scan(&n); err != nil {
		return false, fmt.Errorf("count stored rows: %w", err)
	}
	return n == 0, nil
}

// rebuild loads the archive into path+rebuildSuffix and renames it over
// path when the load succeeded. On failure the side file is removed and
// path is left as it was, so the next start tries again.
func rebuild(ctx context.Context, cfg config.Config, dest archive.Destination, path string, lim limits, log *slog.Logger) error {
	tmp := path + rebuildSuffix
	removeDB(tmp)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	st, err := store.OpenPath(ctx, tmp, store.WithSystem(cfg.SystemID))
	if err != nil {
		return err
	}
	if err := lim.apply(ctx, st); err != nil {
		_ = st.Close()
		removeDB(tmp)
		return err
	}
	err = rebuildFrom(ctx, newArchiver(cfg, st, dest, log))
	if cerr := st.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		removeDB(tmp)
		return err
	}
	removeDB(path)
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace database with the rebuilt one: %w", err)
	}
	return nil
}

// rebuildFrom loads the archive into the archiver's store; a variable so
// that tests can see the store it loads into.
var rebuildFrom = func(ctx context.Context, a *archive.Archiver) error { return a.Rebuild(ctx, time.Now()) }

// removeDB removes a database file and its write-ahead log.
func removeDB(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + ".wal")
}

// within reports whether dir is inside or equal to parent.
func within(dir, parent string) bool {
	d, err1 := filepath.Abs(dir)
	p, err2 := filepath.Abs(parent)
	if err1 != nil || err2 != nil {
		return false
	}
	return d == p || strings.HasPrefix(d, p+string(filepath.Separator))
}
