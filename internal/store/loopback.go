package store

import (
	"context"
	"fmt"
)

// SetLoopbackTooling stores the loopback guard's decision with the epoch,
// unless it has one already.
func (s *Store) SetLoopbackTooling(ctx context.Context, epoch int64, on bool) error {
	s.mu.Lock(opLoopback)
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `UPDATE epochs SET loopback_tooling = ? WHERE epoch = ? AND loopback_tooling IS NULL`,
		on, epoch); err != nil {
		return fmt.Errorf("store loopback decision: %w", err)
	}
	return nil
}
