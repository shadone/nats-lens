package store

import (
	"context"
	"database/sql"
	"errors"
)

// SnapshotCopy copies the source catalog and rows using one source read transaction.
// inspect reads capture provenance in that same transaction. path must be a new,
// caller-owned private destination, never the source database.
func (s *Store) SnapshotCopy(ctx context.Context, path string, inspect func(*sql.Tx) error) error {
	return errors.New("consistent dataset export not implemented")
}
