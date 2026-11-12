package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// SetMemoryLimit bounds the memory the database may use, in bytes. The
// setting is the database's, so one statement covers every connection.
func (s *Store) SetMemoryLimit(ctx context.Context, bytes int64) error {
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("SET memory_limit = '%dB'", bytes)); err != nil {
		return fmt.Errorf("set memory_limit: %w", err)
	}
	return nil
}

// SetThreads sets how many threads the database's queries may use, at
// least one. Several databases in one process each get a share of the
// CPUs instead of DuckDB's default of every core.
func (s *Store) SetThreads(ctx context.Context, n int) error {
	if n < 1 {
		return fmt.Errorf("set threads: %d is not a thread count", n)
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("SET threads = %d", n)); err != nil {
		return fmt.Errorf("set threads: %w", err)
	}
	return nil
}

// SetMaxTempSize bounds the disk DuckDB may use for data that does not fit
// in its memory limit, in bytes: the temporary files in its temp
// directory, by default <database>.tmp beside the database. A query that
// would pass the bound fails with an Out of Memory error, which leaves the
// engine usable. DuckDB's own default is 90% of the free space of that
// disk.
func (s *Store) SetMaxTempSize(ctx context.Context, bytes int64) error {
	if bytes < 1 {
		return fmt.Errorf("set max_temp_directory_size: %d is not a size", bytes)
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("SET max_temp_directory_size = '%dB'", bytes)); err != nil {
		return fmt.Errorf("set max_temp_directory_size: %w", err)
	}
	s.maxTemp.Store(bytes)
	return nil
}

// MaxTempSize is the bound SetMaxTempSize set, or 0 when it was not
// called and DuckDB's default applies.
func (s *Store) MaxTempSize() int64 { return s.maxTemp.Load() }

// TempBytes returns the bytes DuckDB's temporary files hold now: what the
// database's queries spilled to disk and have not released yet.
func (s *Store) TempBytes(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(size), 0)::BIGINT FROM duckdb_temporary_files()`).Scan(&n); err != nil {
		return 0, fmt.Errorf("read temporary files: %w", err)
	}
	return n, nil
}

// IsOutOfMemory reports whether err is DuckDB running out of memory or of
// spill space (max_temp_directory_size, see SetMaxTempSize). The statement
// failed and its transaction rolled back; the engine stays usable, and the
// same statement can succeed once other queries release their memory and
// temporary files. A commit that cannot spill fails with a transaction
// error that names the block it could not write. A fatal error is never
// one, whatever its message says.
func IsOutOfMemory(err error) bool {
	var de *duckdb.Error
	if !errors.As(err, &de) {
		return false
	}
	return de.Type != duckdb.ErrorTypeFatal && (de.Type == duckdb.ErrorTypeOutOfMemory || strings.Contains(de.Msg, "failed to offload data block"))
}
