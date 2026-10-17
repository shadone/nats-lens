package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Migrations 0022 and 0023 apply to a database written by 0001 to 0021
// that holds a finding; the new column is NULL for it.
func TestMigrationsFrom21OnAPopulatedDatabase(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE migrations (version INTEGER PRIMARY KEY, name VARCHAR NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all[:21], 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO findings (id, check_code, entity_type, entity_key, severity, state, last_transition, since, since_epoch, transitioned_at, last_seen_epoch, updated_epoch, detail, transitions_last_hour, flapping)
VALUES ('a','c','e','k','warning','firing','firing', now()::TIMESTAMP, 1, now()::TIMESTAMP, 1, 1, 'd', 0, false)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all, 21); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM findings WHERE condition_since IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
