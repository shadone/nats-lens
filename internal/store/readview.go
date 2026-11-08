package store

import (
	"context"
	"database/sql"
)

// NewReadView borrows a database for the existing store read methods.
func NewReadView(ctx context.Context, db *sql.DB) (*Store, error) {
	return &Store{db: db}, nil
}
