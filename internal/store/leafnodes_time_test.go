package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// leafzView is a hub's view with n LEAFZ leaf node connections, spokes and
// hubs over a few accounts.
func leafzView(id string, n int) *collect.ServerView {
	v := &collect.ServerView{
		Info:  server.ServerInfo{ID: id, Name: "nats-" + id},
		Leafz: &server.Leafz{ID: id, NumLeafs: n},
	}
	for i := range n {
		v.Leafz.Leafs = append(v.Leafz.Leafs, &server.LeafInfo{
			ID: uint64(i + 1), Name: fmt.Sprintf("shop-%d", i), IsSpoke: i%5 != 0, Account: fmt.Sprintf("ACC%d", i%8),
			IP: fmt.Sprintf("10.1.%d.%d", i/256%256, i%256), Port: 7422 + i%1000, RTT: "3.4ms",
			InMsgs: int64(i) * 1000, OutMsgs: int64(i) * 2000, InBytes: int64(i) << 20, OutBytes: int64(i) << 21,
			NumSubs: uint32(i % 300), Compression: "s2_auto",
		})
	}
	return v
}

// TestLeafnodeWriteTime logs how long one epoch's leaf node rows take to
// write: 1,000 and 5,001 leaf nodes on one server (5,001 is one row more
// than a batch), inside the epoch's transaction of a file store, as
// WriteSnapshot writes them.
func TestLeafnodeWriteTime(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	ctx := context.Background()
	for _, n := range []int{1000, 5001} {
		s, err := Open(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		for round := range 3 {
			v := leafzView("NHUB", n)
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			w := newWriter(ctx, tx, int64(round+1), collect.NewSnapshot(t0))
			began := time.Now()
			w.writeLeafnodes(v)
			w.closeStatements()
			took := time.Since(began)
			if w.err != nil {
				t.Fatal(w.err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			t.Logf("round %d: %d leaf nodes written in %v", round, n, took)
		}
		if got := count(t, s.DB(), `SELECT count(*) FROM leafnodes`); got != 3*n {
			t.Fatalf("rows = %d, want %d", got, 3*n)
		}
		_ = s.Close()
	}
}
