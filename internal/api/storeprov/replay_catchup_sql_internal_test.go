package storeprov

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/store"
)

// Leader moves from S0 to S2 mid-run, the replica's server nats-1 changes
// process mid-run, and the same stream name lives in two accounts.
func TestReplayCatchUpSQLCases(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	at := func(e int) time.Time { return t0.Add(time.Duration(e-1) * 30 * time.Second) }
	ex := func(q string, a ...any) {
		t.Helper()
		if _, err := st.DB().ExecContext(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range [][2]string{{"S0", "nats-0"}, {"S1a", "nats-1"}, {"S1b", "nats-1"}, {"S2", "nats-2"}} {
		ex(`INSERT INTO servers (server_id, server_name, version, start_time, first_epoch, last_epoch, first_seen_at, last_seen_at) VALUES (?, ?, '2.12', ?, 1, 10, ?, ?)`,
			s[0], s[1], t0, t0, t0)
	}
	n1 := func(e int) string {
		if e <= 5 {
			return "S1a"
		}
		return "S1b"
	}
	for e := 1; e <= 10; e++ {
		ex(`INSERT INTO epochs VALUES (?, ?, ?, 0, 3, 3, [], [], [], 0, false, [], NULL)`, e, at(e), at(e))
		// ACC1/ORDERS: leader S0 to 4, S2 from 5; nats-1 behind 3..6.
		lead, other := "S0", "S2"
		if e >= 5 {
			lead, other = "S2", "S0"
		}
		for _, h := range []string{lead, other, n1(e)} {
			ex(`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader) VALUES (?, ?, 'ACC1', 'ORDERS', ?)`, e, h, h == lead)
		}
		beh := e >= 3 && e <= 6
		ex(`INSERT INTO stream_replicas (epoch, server_id, account, stream, peer_name, current, offline, lag) VALUES (?, ?, 'ACC1', 'ORDERS', 'nats-1', ?, false, ?)`,
			e, lead, !beh, map[bool]int{true: 100 * e, false: 0}[beh])
		// ACC2/ORDERS: leader S0; nats-1 behind 8..9.
		for _, h := range []string{"S0", "S2", n1(e)} {
			ex(`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader) VALUES (?, ?, 'ACC2', 'ORDERS', ?)`, e, h, h == "S0")
		}
		beh2 := e == 8 || e == 9
		ex(`INSERT INTO stream_replicas (epoch, server_id, account, stream, peer_name, current, offline, lag) VALUES (?, ?, 'ACC2', 'ORDERS', 'nats-1', ?, false, 700)`,
			e, "S0", !beh2)
	}
	p := &Provider{St: st, Interval: 30 * time.Second}
	f := &api.ReplayFacts{A: at(1), B: at(10)}
	if err := p.replayCatchUps(ctx, f, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range f.CatchUps {
		got = append(got, fmt.Sprintf("%s %s %d-%d n%d next %d cur %d from %s cur %s", c.Server, c.Entity.GetKey(), c.FromEpoch, c.LastEpoch, c.Behind,
			c.NextEpoch, c.CurrentEpoch, c.From.Format(time.RFC3339), c.Current.Format(time.RFC3339)))
		if !c.From.Equal(at(int(c.FromEpoch))) || !c.Current.Equal(at(int(c.CurrentEpoch))) || !c.Next.Equal(at(int(c.NextEpoch))) {
			t.Errorf("time shifted: %v", c)
		}
	}
	want := "nats-1 ACC1/ORDERS 3-6 n4 next 7 cur 7 from 2026-10-17T09:01:00Z cur 2026-10-17T09:03:00Z\n" +
		"nats-1 ACC2/ORDERS 8-9 n2 next 10 cur 10 from 2026-10-17T09:03:30Z cur 2026-10-17T09:04:30Z"
	if strings.Join(got, "\n") != want {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}
