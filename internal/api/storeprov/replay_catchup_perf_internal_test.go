package storeprov

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/store"
)

// A day of 30-second collections, 9 servers, 600 streams and 600
// consumers at R3 (about 17 million rows), each follower not current in
// one report of a hundred: the read of a day and of an hour, timed in the
// log. design.md records the times.
func TestReplayCatchUpReadTime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a day of replica rows")
	}
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	n := 600
	stmts := []string{
		`INSERT INTO servers (server_id, server_name, version, start_time, first_epoch, last_epoch, first_seen_at, last_seen_at)
		 SELECT 'S'||i, 'nats-'||i, '2.12', TIMESTAMP '2026-10-16', 1, 2880, TIMESTAMP '2026-10-17', TIMESTAMP '2026-10-18' FROM range(9) t(i)`,
		`INSERT INTO epochs SELECT e, TIMESTAMP '2026-10-17' + to_seconds((e-1)*30), TIMESTAMP '2026-10-17' + to_seconds((e-1)*30), 0, 9, 9, [], [], [], 0, false, [], NULL FROM range(1, 2881) t(e)`,
		`CREATE TEMP TABLE asn AS SELECT s, r, (s + r*3) % 9 AS srv FROM range(?) a(s), range(3) b(r)`,
		`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader) SELECT e, 'S'||srv, 'ACC', 'st'||s, r = 0 FROM range(1, 2881) t(e), asn`,
		`INSERT INTO stream_replicas (epoch, server_id, account, stream, peer_name, current, offline, lag)
		 SELECT e, 'S'||l.srv, 'ACC', 'st'||l.s, 'nats-'||p.srv, hash(e, l.s, p.r) % 100 <> 0, false, CASE WHEN hash(e, l.s, p.r) % 100 = 0 THEN 500 ELSE 0 END
		 FROM range(1, 2881) t(e), asn l JOIN asn p ON p.s = l.s AND p.r > 0 WHERE l.r = 0`,
		`INSERT INTO consumer_stats (epoch, server_id, account, stream, consumer, is_leader) SELECT e, 'S'||srv, 'ACC', 'st'||s, 'c', r = 0 FROM range(1, 2881) t(e), asn`,
		`INSERT INTO consumer_replicas (epoch, server_id, account, stream, consumer, peer_name, current, offline, lag)
		 SELECT e, 'S'||l.srv, 'ACC', 'st'||l.s, 'c', 'nats-'||p.srv, hash(e, l.s, p.r, 1) % 100 <> 0, false, 0
		 FROM range(1, 2881) t(e), asn l JOIN asn p ON p.s = l.s AND p.r > 0 WHERE l.r = 0`,
	}
	for i, q := range stmts {
		var args []any
		if i == 2 {
			args = []any{n}
		}
		if _, err := st.DB().ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	p := &Provider{St: st, Interval: 30 * time.Second}
	start := time.Now()
	f := &api.ReplayFacts{A: t0, B: t0.Add(24*time.Hour - time.Minute)}
	if err := p.replayCatchUps(ctx, f, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("24h: runs %d truncated %v in %v", len(f.CatchUps), f.CatchUpsTruncated, time.Since(start))
	if f.CatchUpsTruncated {
		t.Error("the bursts used up the read limit")
	}
	start = time.Now()
	f = &api.ReplayFacts{A: t0.Add(10 * time.Hour), B: t0.Add(11 * time.Hour)}
	if err := p.replayCatchUps(ctx, f, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	t.Logf("1h: runs %d in %v", len(f.CatchUps), time.Since(start))
}
