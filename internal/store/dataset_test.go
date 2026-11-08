package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

func TestDatasetCopyActiveBoundary(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "data.duckdb")
	var captured int64
	err = s.SnapshotCopy(ctx, dest, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT max(epoch) FROM epochs").Scan(&captured); err != nil {
			return err
		}
		// A separate SQL connection must commit while the read snapshot remains open.
		done := make(chan error, 1)
		go func() { _, e := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Minute))); done <- e }()
		select {
		case e := <-done:
			return e
		case <-time.After(15 * time.Second):
			t.Error("collection stalled by export")
			return context.DeadlineExceeded
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := duckdb.NewConnector(dest+"?access_mode=read_only", nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(c)
	defer db.Close()
	var newest int64
	if err = db.QueryRow("SELECT max(epoch) FROM epochs").Scan(&newest); err != nil {
		t.Fatal(err)
	}
	if newest != captured || captured != 1 {
		t.Fatalf("capture %d export %d", captured, newest)
	}
	if err = CheckSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT count(*) FROM duckdb_indexes()", "SELECT count(*) FROM duckdb_sequences()", "SELECT count(*) FROM duckdb_columns() WHERE comment IS NOT NULL"} {
		var a, b int
		if err = s.db.QueryRow(q).Scan(&a); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(q).Scan(&b); err != nil {
			t.Fatal(err)
		}
		if a != b || a == 0 {
			t.Errorf("catalog count %s: source %d export %d", q, a, b)
		}
	}
}
