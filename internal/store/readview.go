package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// NewReadView borrows an engine-enforced read-only database for existing store
// reads. It does not open, migrate, checkpoint, or own the handle. Close is a
// no-op on the returned view. The owner must keep DB open while reads run.
// ASVS 2.2.2: verify engine access mode at this boundary rather than relying on
// the caller's name or intent. It supplies no collector/runtime health signal.
func NewReadView(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("read view requires a read-only database")
	}
	var mode string
	if err := db.QueryRowContext(ctx, "SELECT current_setting('access_mode')").Scan(&mode); err != nil || !strings.EqualFold(mode, "read_only") {
		return nil, errors.New("read view requires engine-enforced read_only access")
	}
	return &Store{db: db, borrowed: true}, nil
}
