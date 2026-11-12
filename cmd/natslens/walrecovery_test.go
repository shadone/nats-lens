package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

// writeUnreplayableDatabase damages only a disposable synthetic WAL's entry
// checksum. Schema WAL replay succeeds in DuckDB 2 and is not corruption.
func writeUnreplayableDatabase(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE synthetic_wal(i INTEGER); INSERT INTO synthetic_wal VALUES (1); PRAGMA disable_checkpoint_on_shutdown`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path + ".wal")
	if err != nil || len(b) == 0 {
		t.Fatalf("read synthetic WAL: %v (%d bytes)", err, len(b))
	}
	b[len(b)-1] ^= 0xff
	if err := os.WriteFile(path+".wal", b, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.OpenPath(context.Background(), path)
	if st != nil {
		st.Close()
	}
	if !errors.Is(err, store.ErrWALReplay) {
		t.Fatalf("setup: got %v, want ErrWALReplay", err)
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

	st, _, err := openStore(ctx, cfg, 2, quietLog())
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
