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
