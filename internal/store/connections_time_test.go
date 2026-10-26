package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// connzView is a server's view with n CONNZ connections spread over a
// few accounts and applications, a third of them with a user JWT.
func connzView(id string, n int, start time.Time) *collect.ServerView {
	v := &collect.ServerView{
		Info:     server.ServerInfo{ID: id, Name: "nats-" + id},
		Connz:    &server.Connz{ID: id, NumConns: n, Total: n},
		UserJWTs: map[uint64]collect.UserJWT{},
	}
	for i := range n {
		cid := uint64(i + 1)
		v.Connz.Conns = append(v.Connz.Conns, &server.ConnInfo{
			Cid: cid, Kind: "Client", Type: "nats", Name: fmt.Sprintf("app-%d@pod-%d", i%40, i),
			Lang: "go", Version: "1.37.0", Account: fmt.Sprintf("ACC%d", i%8), AuthorizedUser: "u",
			IP: "10.0.0.1", Port: 40000 + i%20000, Start: start.Add(-time.Duration(i) * time.Second),
			LastActivity: start.Add(-time.Duration(i%60) * time.Millisecond), RTT: "1.2ms",
			Pending: i % 7, InMsgs: int64(i) * 1000, OutMsgs: int64(i) * 2000, InBytes: int64(i) << 20,
			OutBytes: int64(i) << 21, Stalls: int64(i % 3), NumSubs: uint32(i % 50), TLSVersion: "1.3",
			NameTag: fmt.Sprintf("acc-%d", i%8),
		})
		if i%3 == 0 {
			v.UserJWTs[cid] = collect.UserJWT{Decoded: true, Expires: start.Add(time.Duration(i) * time.Hour)}
		}
	}
	return v
}

// TestConnectionWriteTime logs how long one epoch's connection rows take
// to write: 4,096 connections on one server, inside the epoch's
// transaction of a file store, as WriteSnapshot writes them.
func TestConnectionWriteTime(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const n = 4096
	for round := range 3 {
		v := connzView("NBIGSERVER", n, t0)
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		w := newWriter(ctx, tx, int64(round+1), collect.NewSnapshot(t0))
		began := time.Now()
		w.writeConnections(v)
		w.closeStatements()
		took := time.Since(began)
		if w.err != nil {
			t.Fatal(w.err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		t.Logf("round %d: %d connections written in %v", round, n, took)
	}
	if got := count(t, s.DB(), `SELECT count(*) FROM connections`); got != 3*n {
		t.Fatalf("rows = %d, want %d", got, 3*n)
	}
}
