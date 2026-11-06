package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/shadone/nats-lens/internal/store"
)

func TestArchiveSourceHealth(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	snap := store.FixtureSnapshot(t, h0.Add(time.Minute))
	// Add sources to an existing fixture stream so export exercises a real
	// batch, rather than a hand-inserted row.
	v := snap.SortedViews()[0]
	s := &v.Jsz.AccountDetails[0].Streams[0]
	s.Config.Mirror = nil
	s.Config.Sources = []*server.StreamSource{{Name: "orders", FilterSubject: "orders.eu.>"}, {Name: "orders", FilterSubject: "orders.us.>"}}
	s.Cluster = nil
	s.Sources = []*server.StreamSourceInfo{{Name: "orders", FilterSubject: "orders.eu.>", Lag: ^uint64(0), Active: 9007199254740993, Error: &server.ApiError{Code: 404, ErrCode: 10059, Description: "origin 'orders' missing\x00detail"}}}
	if _, err := st.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d", n)
	}
	rb := rebuilt(t, dir, now)
	for _, q := range []string{
		`SELECT count(*) FROM stream_source_stats WHERE reported AND lag=18446744073709551615::UBIGINT AND active_ns=9007199254740993 AND error_id=10059`,
		`SELECT count(*) FROM stream_source_stats WHERE NOT reported AND lag IS NULL AND active_ns IS NULL AND error IS NULL`,
	} {
		if n := scalar[int](t, rb, q); n != 1 {
			t.Errorf("%s: %d", q, n)
		}
	}
	// Retention must include the new table, not leave orphan rows behind.
	if _, err := rb.Sweep(context.Background(), now.Add(48*time.Hour), time.Hour); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM stream_source_stats`); n != 0 {
		t.Errorf("after sweep: %d", n)
	}
}

func TestArchiveBeforeSourceHealth(t *testing.T) {
	st, dir := openMem(t), t.TempDir()
	if _, err := st.WriteSnapshot(context.Background(), store.FixtureSnapshot(t, h0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if n := exported(t, newArchiver(st, dir), now); n != 1 {
		t.Fatalf("exported %d", n)
	}
	stripColumns(t, st, hourFile(t, dir, "stream_stats"), "source_config_known")
	rb := rebuilt(t, dir, now)
	if n := scalar[int](t, rb, `SELECT count(*) FROM stream_stats WHERE source_config_known IS NOT FALSE`); n != 0 {
		t.Errorf("old archive invented source membership: %d rows", n)
	}
	if n := scalar[int](t, rb, `SELECT count(*) FROM stream_source_stats`); n != 0 {
		t.Errorf("old archive invented %d source reports", n)
	}
}
