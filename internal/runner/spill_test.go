package runner

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

// An epoch write that fails because other work holds the database's
// memory and spill is logged with that cause, the store stays usable, and
// the next epoch is written once the spill is released.
func TestEpochWriteOutOfSpillIsReported(t *testing.T) {
	ctx := context.Background()
	r := newRunner(t)
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r.Store = st
	var logs bytes.Buffer
	r.Log = slog.New(slog.NewTextHandler(&logs, nil))
	if _, err := r.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMemoryLimit(ctx, 64<<20); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMaxTempSize(ctx, 32<<20); err != nil {
		t.Fatal(err)
	}
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE filler (i BIGINT, m VARCHAR)`); err != nil {
		t.Fatal(err)
	}
	full := false
	for range 200 {
		if _, err := conn.ExecContext(ctx, `INSERT INTO filler SELECT i, md5(i::VARCHAR) || md5((i + 1)::VARCHAR) FROM range(100000) t(i)`); err != nil {
			full = true
			break
		}
	}
	if !full {
		t.Fatal("the spill bound never refused the filler")
	}
	logs.Reset()
	_, err = r.Once(ctx)
	if !store.IsOutOfMemory(err) {
		t.Fatalf("Once with the spill full: %v, want an Out of Memory error", err)
	}
	if !strings.Contains(logs.String(), "epoch not stored") || !strings.Contains(logs.String(), "duckdb_max_temp_size") {
		t.Errorf("log does not name the cause:\n%s", logs.String())
	}
	if st.Failure() != nil {
		t.Fatalf("store failed: %v", st.Failure())
	}
	if _, err := conn.ExecContext(ctx, `DROP TABLE filler`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Once(ctx); err != nil {
		t.Fatalf("epoch after the spill was released: %v", err)
	}
	if n := epochs(t, r); n != 2 {
		t.Errorf("epochs = %d, want 2", n)
	}
}
