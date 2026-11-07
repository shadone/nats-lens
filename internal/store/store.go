// Package store keeps epochs in an embedded DuckDB database: one transaction
// per epoch, versioned migrations and a retention sweep.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// FileName is the database file inside the data directory.
const FileName = "natslens.duckdb"

// Store is the natslens database. Every write holds mu, so that their
// transactions never run at the same time and conflict: epochs, events,
// mutes, the retention sweep, recording an exported hour and the rebuild.
// Exporting an hour's rows to Parquet only reads and runs without mu, next
// to the writes.
type Store struct {
	db    *sql.DB
	guard *engineGuard
	mu    sync.Mutex
	log   *slog.Logger
	// warned holds the asset warnings already logged; written under mu by
	// WriteSnapshot.
	warned map[warning]bool
}

// SetLogger sets where the store reports data it skipped; the default is
// slog.Default().
func (s *Store) SetLogger(l *slog.Logger) { s.log = l }

func (s *Store) logger() *slog.Logger {
	if s.log == nil {
		return slog.Default()
	}
	return s.log
}

// Open opens or creates the database file in dataDir and applies migrations.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return OpenPath(ctx, filepath.Join(dataDir, FileName))
}

// ErrWALReplay marks a database whose write-ahead log DuckDB cannot replay.
// Such a file never opens again; the caller moves it aside and rebuilds.
var ErrWALReplay = errors.New("write-ahead log cannot be replayed")

// openError wraps an error from opening path, marking a failed WAL replay.
func openError(path string, err error) error {
	err = SafeError("open", err)
	if !IsFatal(err) && strings.Contains(err.Error(), "replaying WAL") {
		return fmt.Errorf("open duckdb %s: %w: %w", path, ErrWALReplay, err)
	}
	return fmt.Errorf("open duckdb %s: %w", path, err)
}

// OpenPath opens the database at path; an empty path is an in-memory database.
func OpenPath(ctx context.Context, path string) (*Store, error) {
	connector, err := duckdb.NewConnector(path, nil)
	if err != nil {
		return nil, openError(path, err)
	}
	guard := newEngineGuard()
	db := sql.OpenDB(&guardedConnector{Connector: connector, g: guard})
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, openError(path, err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Write the migrated schema into the database file now. Until the
	// first checkpoint it lives only in the write-ahead log, and DuckDB
	// cannot replay a log that creates the schema of a database file that
	// was never checkpointed: a crash then leaves a file that never opens.
	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("checkpoint %s: %w", path, err)
	}
	return &Store{db: db, guard: guard}, nil
}

// DB exposes the handle for queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// LastSeenServers returns each server name with the last time it answered,
// for seeding the collector's expected servers at start.
func (s *Store) LastSeenServers(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server_name, max(last_seen_at) FROM servers GROUP BY server_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var name string
		var at time.Time
		if err := rows.Scan(&name, &at); err != nil {
			return nil, err
		}
		out[name] = at
	}
	return out, rows.Err()
}
