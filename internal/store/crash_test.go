package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func copyFile(t *testing.T, from, to string) {
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A process killed right after OpenPath must leave a database that opens:
// the migrations are checkpointed into the file, not left in the WAL.
// Adopted from the review.
func TestReopenAfterCrashAfterOpen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenPath(ctx, filepath.Join(dir, "a.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	crash := t.TempDir()
	copyFile(t, filepath.Join(dir, "a.duckdb"), filepath.Join(crash, "a.duckdb"))
	if _, err := os.Stat(filepath.Join(dir, "a.duckdb.wal")); err == nil {
		copyFile(t, filepath.Join(dir, "a.duckdb.wal"), filepath.Join(crash, "a.duckdb.wal"))
	}
	s2, err := OpenPath(ctx, filepath.Join(crash, "a.duckdb"))
	if err != nil {
		t.Fatalf("reopen after crash: %.300v", err)
	}
	s2.Close()
}

// An unreplayable WAL is reported as ErrWALReplay, so that the caller can
// move the files aside instead of failing at every start.
func TestUnreplayableWALIsErrWALReplay(t *testing.T) {
	src := filepath.Join(t.TempDir(), "s.duckdb")
	db, err := sql.Open("duckdb", src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (a INTEGER, ts TIMESTAMP DEFAULT current_timestamp); ALTER TABLE t ADD COLUMN b VARCHAR`); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "d.duckdb")
	copyFile(t, src, dst)
	copyFile(t, src+".wal", dst+".wal")
	_, err = OpenPath(context.Background(), dst)
	if !errors.Is(err, ErrWALReplay) {
		t.Fatalf("err = %v, want ErrWALReplay", err)
	}
}
