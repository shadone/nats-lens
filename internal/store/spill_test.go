package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// fillSpill holds data in a temporary table on its own connection until
// the store's spill bound refuses more; the returned func drops it.
func fillSpill(t *testing.T, s *Store) func() {
	t.Helper()
	ctx := context.Background()
	conn, err := s.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE filler (i BIGINT, m VARCHAR)`); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		_, err := conn.ExecContext(ctx, `INSERT INTO filler SELECT i, md5(i::VARCHAR) || md5((i + 1)::VARCHAR) FROM range(100000) t(i)`)
		if err == nil {
			continue
		}
		// The insert's commit is what cannot spill.
		if !IsOutOfMemory(err) || IsFatal(err) {
			t.Fatalf("filling the spill: %v", err)
		}
		return func() {
			if _, err := conn.ExecContext(ctx, `DROP TABLE filler`); err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
		}
	}
	t.Fatal("the spill bound never refused the filler")
	return nil
}

// A query that needs more spill than the bound fails with an Out of
// Memory error and leaves the engine usable. While other work holds the
// spill at the bound, the epoch write fails the same way, and the next
// epoch is written once the spill is released.
func TestSpillPastTheBoundFailsCleanly(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, start)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMemoryLimit(ctx, 64<<20); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMaxTempSize(ctx, 32<<20); err != nil {
		t.Fatal(err)
	}

	var n int64
	err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM (SELECT i, md5(i::VARCHAR) m FROM range(5000000) t(i) ORDER BY m)`).Scan(&n)
	// The bound, not the memory limit, refused it: DuckDB could not
	// write a block to its temporary files.
	if !IsOutOfMemory(err) || IsFatal(err) || err == nil || !strings.Contains(err.Error(), "failed to offload data block") {
		t.Fatalf("spilling query: %v, want an Out of Memory error of the spill bound that is not fatal", err)
	}
	if s.Failure() != nil {
		t.Fatalf("store failed: %v", s.Failure())
	}
	// The failed query's temporary files are released; DuckDB may keep a
	// block or two.
	if got, err := s.TempBytes(ctx); err != nil || got > 1<<20 {
		t.Fatalf("spill after the failed query = %d, %v; want under 1 MiB", got, err)
	}

	release := fillSpill(t, s)
	if got, _ := s.TempBytes(ctx); got < 16<<20 {
		t.Fatalf("filler spilled %d bytes", got)
	}
	_, err = s.WriteSnapshot(ctx, fixtureSnapshot(t, start.Add(30*time.Second)))
	if !IsOutOfMemory(err) || IsFatal(err) {
		t.Fatalf("epoch write with the spill full: %v, want an Out of Memory error that is not fatal", err)
	}
	if s.Failure() != nil {
		t.Fatalf("store failed: %v", s.Failure())
	}
	release()
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, start.Add(time.Minute))); err != nil {
		t.Fatalf("epoch write after the spill was released: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM epochs`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("epochs = %d, %v; want 2", n, err)
	}
}

// IsOutOfMemory takes DuckDB's out of memory errors and a commit that
// could not spill, never a fatal error that mentions a block it could not
// write: that one stops the engine and must be reported as fatal.
func TestIsOutOfMemory(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&duckdb.Error{Type: duckdb.ErrorTypeOutOfMemory, Msg: "Out of Memory Error: failed to pin block"}, true},
		{fmt.Errorf("commit: %w", &duckdb.Error{Type: duckdb.ErrorTypeTransaction, Msg: "TransactionContext Error: Failed to commit: failed to offload data block of size 256.0 KiB"}), true},
		{&duckdb.Error{Type: duckdb.ErrorTypeFatal, Msg: "FATAL Error: failed to offload data block of size 256.0 KiB"}, false},
		{&duckdb.Error{Type: duckdb.ErrorTypeConstraint, Msg: "Constraint Error: duplicate key"}, false},
		{errors.New("Out of Memory Error: not a driver error"), false},
	} {
		if got := IsOutOfMemory(c.err); got != c.want {
			t.Errorf("IsOutOfMemory(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
