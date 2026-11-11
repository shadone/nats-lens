package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
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

// leaveWAL runs qs on the database at path and closes it without a
// checkpoint, as a process that crashed or was killed after committing
// them leaves it: the changes are only in the write-ahead log.
func leaveWAL(t *testing.T, path string, qs ...string) {
	t.Helper()
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, q := range append([]string{`PRAGMA disable_checkpoint_on_shutdown`, `SET wal_autocheckpoint = '1TB'`}, qs...) {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".wal"); err != nil {
		t.Fatalf("no write-ahead log left: %v", err)
	}
}

// A database written by a later binary, whose migrations table names a
// migration this binary does not have, is refused at startup with an error
// that says so, and the refusal writes nothing: the database file and its
// write-ahead log, when the later binary left one, are byte for byte what
// they were, and the later binary's catalog (here a table comment) is
// untouched.
func TestNewerDatabaseIsRefusedBeforeAnyWrite(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	newest := all[len(all)-1].version
	for _, withWAL := range []bool{false, true} {
		t.Run(map[bool]string{false: "checkpointed", true: "with_wal"}[withWAL], func(t *testing.T) {
			path := newerDatabase(t, newest+1)
			if withWAL {
				leaveWAL(t, path, fmt.Sprintf(`INSERT INTO migrations (version, name, sha256) VALUES (%d, 'later_still', repeat('1', 64))`, newest+2))
			}
			before, beforeWAL := fileSum(t, path), fileSum(t, path+".wal")

			_, err := OpenPath(ctx, path)
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
		})
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

// A later binary that crashed right after its migration committed leaves
// the newer migration only in the write-ahead log: the file itself records
// this binary's schema. The read-only check replays the log, sees the
// newer migration and refuses, and the file and the log stay as they were.
func TestNewerMigrationOnlyInTheLogIsRefused(t *testing.T) {
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`CHECKPOINT`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	leaveWAL(t, path, fmt.Sprintf(`INSERT INTO migrations (version, name, sha256) VALUES (%d, 'from_a_later_binary', repeat('0', 64))`,
		all[len(all)-1].version+1))
	before, beforeWAL := fileSum(t, path), fileSum(t, path+".wal")
	if _, err := OpenPath(ctx, path); err == nil || !strings.Contains(err.Error(), "newer than this binary") {
		t.Fatalf("open with a newer migration in the log: %v, want a refusal naming a newer schema", err)
	}
	if after := fileSum(t, path); after != before {
		t.Errorf("database file changed by the refusal")
	}
	if after := fileSum(t, path+".wal"); after != beforeWAL {
		t.Errorf("write-ahead log after the refusal: %s, was %s", after, beforeWAL)
	}
}

// A database with an edited migration is refused before startup fills in
// any missing checksum: the refusal leaves the database file, its
// write-ahead log when there is one, and the migrations table as they were.
func TestEditedMigrationRefusedBeforeBackfill(t *testing.T) {
	ctx := context.Background()
	for _, withWAL := range []bool{false, true} {
		t.Run(map[bool]string{false: "checkpointed", true: "with_wal"}[withWAL], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db.duckdb")
			s, err := OpenPath(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			edits := []string{
				`UPDATE migrations SET sha256 = NULL WHERE version = 1`,
				`UPDATE migrations SET sha256 = 'edited' WHERE version = 3`,
			}
			if !withWAL {
				for _, q := range append(edits, `CHECKPOINT`) {
					if _, err := s.DB().Exec(q); err != nil {
						t.Fatalf("%s: %v", q, err)
					}
				}
			}
			s.Close()
			if withWAL {
				leaveWAL(t, path, edits...)
			}
			before, beforeWAL := fileSum(t, path), fileSum(t, path+".wal")
			if _, err := OpenPath(ctx, path); err == nil || !strings.Contains(err.Error(), "0003") {
				t.Fatalf("open with an edited migration: %v, want a refusal naming 0003", err)
			}
			if after := fileSum(t, path); after != before {
				t.Errorf("database file changed by the refusal")
			}
			if after := fileSum(t, path+".wal"); after != beforeWAL {
				t.Errorf("write-ahead log after the refusal: %s, was %s", after, beforeWAL)
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
		})
	}
}
