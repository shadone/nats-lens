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

// DuckDB 2 can replay this schema WAL, which older engines could not replay.
func TestReopenAfterSchemaWALReplay(t *testing.T) {
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
	s, err := OpenPath(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.DB().QueryRow(`SELECT count(*) FROM duckdb_columns() WHERE table_name = 't' AND column_name IN ('a', 'ts', 'b')`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("replayed columns = %d, %v", n, err)
	}
}

// A WAL with a bad entry checksum is reported as ErrWALReplay, so that the caller
// can preserve the files and rebuild. Use only a disposable synthetic file.
func TestUnreplayableWALIsErrWALReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.duckdb")
	s, err := OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`CREATE TABLE synthetic_wal(i INTEGER); INSERT INTO synthetic_wal VALUES (1); PRAGMA disable_checkpoint_on_shutdown`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path + ".wal")
	if err != nil || len(b) == 0 {
		t.Fatalf("read synthetic WAL: %v (%d bytes)", err, len(b))
	}
	// Flip an entry byte, leaving its stored checksum unchanged. Unlike a torn
	// tail, which DuckDB can discard, a checksum mismatch must refuse replay.
	b[len(b)-1] ^= 0xff
	if err := os.WriteFile(path+".wal", b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = OpenPath(context.Background(), path)
	if !errors.Is(err, ErrWALReplay) {
		t.Fatalf("err = %v, want ErrWALReplay", err)
	}
}
