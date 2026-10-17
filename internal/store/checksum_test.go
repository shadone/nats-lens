package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationChecksumsRecorded(t *testing.T) {
	s := openMem(t)
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		var got string
		if err := s.DB().QueryRow(`SELECT sha256 FROM migrations WHERE version = ?`, m.version).Scan(&got); err != nil {
			t.Fatalf("version %d: %v", m.version, err)
		}
		if got != m.sha256 {
			t.Errorf("version %d: recorded %q, file %q", m.version, got, m.sha256)
		}
	}
}

// A database whose recorded checksum differs from the embedded file, as
// when an applied migration was edited, must not start.
func TestEditedMigrationFailsStart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE migrations SET sha256 = 'edited' WHERE version = 3`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	_, err = OpenPath(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "0003") {
		t.Fatalf("expected a checksum error naming migration 0003, got %v", err)
	}
}

// Rows applied before the checksum column existed are filled on start.
func TestMissingChecksumsBackfilled(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE migrations SET sha256 = NULL`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if n := count(t, s.DB(), `SELECT count(*) FROM migrations WHERE sha256 IS NULL`); n != 0 {
		t.Fatalf("%d rows without checksum", n)
	}
}

// openAllButLast creates a database file with every migration applied
// except the newest, which is returned as the pending one.
func openAllButLast(t *testing.T) (*sql.DB, []migration) {
	t.Helper()
	ctx := context.Background()
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE migrations (
    version INTEGER PRIMARY KEY, name VARCHAR NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT current_timestamp)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateWith(ctx, db, all[:len(all)-1], 0); err != nil {
		t.Fatal(err)
	}
	return db, all
}

func versionApplied(t *testing.T, db *sql.DB, v int) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM migrations WHERE version = ?`, v).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// An edited migration is reported before any pending migration runs on top
// of it.
func TestEditedMigrationStopsPendingOnes(t *testing.T) {
	db, all := openAllButLast(t)
	if _, err := db.Exec(`UPDATE migrations SET sha256 = 'edited' WHERE version = 3`); err != nil {
		t.Fatal(err)
	}
	last := all[len(all)-1]
	err := migrateWith(context.Background(), db, all, last.version-1)
	if err == nil || !strings.Contains(err.Error(), "0003") {
		t.Fatalf("expected a checksum error naming 0003, got %v", err)
	}
	if versionApplied(t, db, last.version) {
		t.Fatalf("migration %04d was applied on top of an edited migration", last.version)
	}
}

// A migration's checksum is written with it, not filled in afterwards.
func TestChecksumRecordedWithMigration(t *testing.T) {
	db, all := openAllButLast(t)
	m := all[len(all)-1]
	if err := applyMigration(context.Background(), db, m); err != nil {
		t.Fatal(err)
	}
	var sum sql.NullString
	if err := db.QueryRow(`SELECT sha256 FROM migrations WHERE version = ?`, m.version).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if !sum.Valid || sum.String != m.sha256 {
		t.Fatalf("sha256 = %v, want %s", sum, m.sha256)
	}
}
