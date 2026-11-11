package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileSum returns the SHA-256 of a file, or "absent" when it does not exist.
func fileSum(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newerDatabase writes a database as a later binary would leave it: every
// migration of this binary, then a migration this binary does not have,
// and a comment on the migrations table that differs from this binary's.
// It returns the database's path, checkpointed and closed.
func newerDatabase(t *testing.T, extra int) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "newer.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO migrations (version, name, sha256) VALUES (?, 'from_a_later_binary', repeat('0', 64))`,
		`COMMENT ON TABLE migrations IS 'Written by a later binary.'`,
		`CHECKPOINT`,
	} {
		var err error
		if strings.Contains(q, "?") {
			_, err = s.DB().Exec(q, extra)
		} else {
			_, err = s.DB().Exec(q)
		}
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// A database written by a later binary, whose migrations table names a
// migration this binary does not have, is refused at startup with an error
// that says so, and the refusal writes nothing: the database file is
// byte for byte what it was, no write-ahead log is left, and the later
// binary's catalog (here a table comment) is untouched.
func TestNewerDatabaseIsRefusedBeforeAnyWrite(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	path := newerDatabase(t, all[len(all)-1].version+1)
	before, beforeWAL := fileSum(t, path), fileSum(t, path+".wal")

	_, err = OpenPath(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "newer than this binary") {
		t.Fatalf("open a newer database: %v, want a refusal naming a newer schema", err)
	}
	if after := fileSum(t, path); after != before {
		t.Errorf("database file changed by the refusal: sha256 %s, was %s", after, before)
	}
	if after := fileSum(t, path+".wal"); after != beforeWAL {
		t.Errorf("write-ahead log after the refusal: %s, was %s", after, beforeWAL)
	}
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var comment string
	if err := db.QueryRow(`SELECT comment FROM duckdb_tables() WHERE table_name = 'migrations'`).Scan(&comment); err != nil {
		t.Fatal(err)
	}
	if comment != "Written by a later binary." {
		t.Errorf("migrations table comment after the refusal = %q, want the later binary's", comment)
	}
}

// The read-only schema check names a newer database as newer, rather than
// suggesting a migration this binary cannot do.
func TestCheckSchemaNamesANewerDatabase(t *testing.T) {
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	path := newerDatabase(t, all[len(all)-1].version+1)
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = CheckSchema(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "newer than this binary") || strings.Contains(err.Error(), "to migrate") {
		t.Fatalf("CheckSchema on a newer database: %v, want it named newer without advice to migrate", err)
	}
}

// openReadOnly opens path read-only without any schema check, for
// tests that inspect a database this binary refuses.
func openReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// A database with an edited migration is refused before startup fills in
// any missing checksum: the refusal leaves the migrations table as it was.
func TestEditedMigrationRefusedBeforeBackfill(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE migrations SET sha256 = NULL WHERE version = 1`,
		`UPDATE migrations SET sha256 = 'edited' WHERE version = 3`,
		`CHECKPOINT`,
	} {
		if _, err := s.DB().Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	before := fileSum(t, path)
	if _, err := OpenPath(ctx, path); err == nil || !strings.Contains(err.Error(), "0003") {
		t.Fatalf("open with an edited migration: %v, want a refusal naming 0003", err)
	}
	if after := fileSum(t, path); after != before {
		t.Errorf("database file changed by the refusal")
	}
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sum sql.NullString
	if err := db.QueryRow(`SELECT sha256 FROM migrations WHERE version = 1`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum.Valid {
		t.Errorf("migration 0001's checksum was filled in before the refusal: %s", sum.String)
	}
}
