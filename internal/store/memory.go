package store

import (
	"context"
	"fmt"
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
