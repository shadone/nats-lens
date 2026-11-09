package dataset

import (
	"context"
	"database/sql"
)

// InspectDatabase reads the canonical capture boundaries from a closed, stable
// database already validated and opened read-only by OpenDatabase or Open. It
// borrows the handle: no Close, migration, checkpoint, or boundary reinterpretation.
func InspectDatabase(ctx context.Context, db *sql.DB) (Boundaries, error) {
	if err := ctx.Err(); err != nil {
		return Boundaries{}, err
	}
	if db == nil {
		return Boundaries{}, ErrInvalid
	}
	return readBoundaries(ctx, db)
}
