package store

import (
	"context"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/collect"
)

// TestWriteJSONBoundaries counts the rows and statements writeJSON
// makes around the batch size, and that an error stops the loop.
func TestWriteJSONBoundaries(t *testing.T) {
	type row struct {
		X int64 `json:"x"`
	}
	s := openMem(t)
	ctx := context.Background()
	for _, n := range []int{0, 1, connectionBatch - 1, connectionBatch, connectionBatch + 1, 2*connectionBatch + 1} {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		w := newWriter(ctx, tx, 1, collect.NewSnapshot(t0))
		w.exec(`CREATE TEMP TABLE review_t (x BIGINT, part BIGINT)`)
		rows := make([]row, n)
		for i := range rows {
			rows[i].X = int64(i)
		}
		writeJSON(w, "review_t", `INSERT INTO review_t SELECT x, (SELECT count(DISTINCT part) FROM review_t) `+fromJSON(`[{"x":"BIGINT"}]`), rows, connectionBatch)
		var got, distinct, parts int
		if err := tx.QueryRowContext(ctx, `SELECT count(*), count(DISTINCT x), count(DISTINCT part) FROM review_t`).Scan(&got, &distinct, &parts); err != nil {
			t.Fatal(err)
		}
		want := (n + connectionBatch - 1) / connectionBatch
		if w.err != nil || got != n || distinct != n || parts != want {
			t.Errorf("n=%d: rows %d, distinct %d, statements %d (want %d), err %v", n, got, distinct, parts, want, w.err)
		}
		_ = tx.Rollback()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	w := newWriter(ctx, tx, 1, collect.NewSnapshot(t0))
	w.exec(`CREATE TEMP TABLE review_u (x TINYINT)`)
	rows := make([]row, 3*connectionBatch)
	rows[connectionBatch+2].X = 1000 // out of range in the second batch
	writeJSON(w, "review_u", `INSERT INTO review_u SELECT x `+fromJSON(`[{"x":"BIGINT"}]`), rows, connectionBatch)
	if w.err == nil || !strings.Contains(w.err.Error(), "INSERT INTO review_u") {
		t.Errorf("err %v; want an error naming the statement", w.err)
	}
}
