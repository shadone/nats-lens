package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

// writeUnreplayableDatabase leaves at path what a crash before DuckDB's
// first checkpoint leaves: a database file that was never checkpointed and
// a write-ahead log that alters a table with a default expression, as
// migration 0010 alters migrations. DuckDB 1.5 cannot replay it ("no
// default database set").
func writeUnreplayableDatabase(t *testing.T, path string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src.duckdb")
	db, err := sql.Open("duckdb", src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE t (a INTEGER, ts TIMESTAMP DEFAULT current_timestamp); ALTER TABLE t ADD COLUMN b VARCHAR`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{"", ".wal"} {
		b, err := os.ReadFile(src + ext)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+ext, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.OpenPath(context.Background(), path); err == nil {
		t.Fatal("setup: the crashed database opened")
	}
}

// A database whose WAL cannot be replayed is moved aside, both files kept,
// and rebuilt from the archive; the store records the recovery as an event.
func TestUnreplayableWALIsMovedAsideAndRebuilt(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t.TempDir())
	writeArchive(t, cfg)
	dbPath := filepath.Join(cfg.DataDir, store.FileName)
	writeUnreplayableDatabase(t, dbPath)

	st, _, err := openStore(ctx, cfg, quietLog())
	if err != nil {
		t.Fatalf("open after crash: %v", err)
	}
	defer st.Close()
	if n := countEvents(t, st); n != 3 {
		t.Fatalf("events = %d, want the 2 archived plus the recovery event", n)
	}
	var kind, summary string
	if err := st.DB().QueryRow(`SELECT kind, summary FROM events WHERE source = 'collector'`).Scan(&kind, &summary); err != nil {
		t.Fatal(err)
	}
	if kind != "collector.database_recovered" || !strings.Contains(summary, "corrupt-") {
		t.Fatalf("recovery event = %s %q", kind, summary)
	}
	entries, err := os.ReadDir(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	var aside []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			aside = append(aside, e.Name())
		}
	}
	if len(aside) != 2 {
		t.Fatalf("files kept aside = %v, want the database and its WAL", aside)
	}
}
