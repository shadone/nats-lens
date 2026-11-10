package storeprov_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The catch-up fixture: epochs every 30 seconds from rT (epoch 1) to
// epoch 32, without epochs 22 to 25. nats-0 leads ORDERS and KV_cfg and
// the durable worker; nats-1 restarts (its old process answers up to
// epoch 5, its new one from epoch 7) and leads PAY. A is epoch 3, B epoch
// 30.
//
//   - nats-1's ORDERS replica: offline at 6, behind at 7 to 9, current
//     at 10: caught up.
//   - nats-1's worker replica: behind at 7 only, the first collection
//     of its new process: kept; current at 8.
//   - nats-2's ORDERS replica: behind at 12 and 13, not reporting
//     ORDERS itself at 14, current at 15: the end is hidden. Behind
//     again at 20 alone, current at 21: a burst.
//   - nats-2's PAY replica: behind at 19 to 21, current at 26 after the
//     time without collections: the end is hidden.
//   - nats-2's KV_cfg replica: behind at 28 to 30, still at B.
func catchUpStore(t *testing.T) *storeprov.Provider {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	proc := func(id, name string, first, last int) {
		r.add("servers", map[string]any{"server_id": id, "server_name": name, "version": "2.12.2", "start_time": rEpoch(first).Add(-10 * time.Second),
			"first_epoch": first, "last_epoch": last, "first_seen_at": rEpoch(first), "last_seen_at": rEpoch(last)})
	}
	proc("S0", "nats-0", 1, 32)
	proc("S1OLD", "nats-1", 1, 5)
	proc("S1NEW", "nats-1", 7, 32)
	proc("S2", "nats-2", 1, 32)
	answering := func(n int) []string {
		ids := []string{"S0", "S2"}
		switch {
		case n <= 5:
			ids = append(ids, "S1OLD")
		case n >= 7:
			ids = append(ids, "S1NEW")
		}
		return ids
	}
	for n := 1; n <= 32; n++ {
		if n >= 22 && n <= 25 {
			continue
		}
		ids := answering(n)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 3, ?, ['nats-0', 'nats-1', 'nats-2'], ?, [], 0, false, [], NULL)`,
			n, rEpoch(n), rEpoch(n), len(ids), []string{"nats-0", "nats-1", "nats-2"}); err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			r.add("epoch_endpoints", map[string]any{"epoch": n, "server_id": id, "endpoint": "JSZ", "state": "ok"})
		}
		nats1 := "S1OLD"
		if n >= 7 {
			nats1 = "S1NEW"
		}
		up1 := n != 6
		// Each replica's report as its leader gives it: "" in sync,
		// "b" behind, "o" offline, "-" not held by the replica's server.
		type rep struct {
			stream, consumer, leader string
			peers                    map[string]string
			lag                      map[string]int64
		}
		state := func(m map[int]string, d string) string {
			if s, ok := m[n]; ok {
				return s
			}
			return d
		}
		reps := []rep{
			{stream: "ORDERS", leader: "S0", peers: map[string]string{
				"nats-1": state(map[int]string{6: "o", 7: "b", 8: "b", 9: "b"}, ""),
				"nats-2": state(map[int]string{12: "b", 13: "b", 14: "-", 20: "b"}, "")},
				lag: map[string]int64{"nats-1": int64(1000 - 100*n), "nats-2": 2500}},
			{stream: "ORDERS", consumer: "worker", leader: "S0", peers: map[string]string{"nats-1": state(map[int]string{6: "o", 7: "b"}, "")},
				lag: map[string]int64{"nats-1": 40}},
			{stream: "PAY", leader: nats1, peers: map[string]string{"nats-2": state(map[int]string{19: "b", 20: "b", 21: "b"}, "")},
				lag: map[string]int64{"nats-2": 7000}},
			{stream: "KV_cfg", leader: "S0", peers: map[string]string{"nats-2": state(map[int]string{28: "b", 29: "b", 30: "b"}, "")},
				lag: map[string]int64{"nats-2": int64(18400 + 10*(n-28))}},
		}
		idOf := map[string]string{"nats-0": "S0", "nats-1": nats1, "nats-2": "S2"}
		for _, x := range reps {
			if x.leader == nats1 && !up1 {
				continue
			}
			holders := []string{x.leader}
			for peer, s := range x.peers {
				if s != "-" && (peer != "nats-1" || up1) {
					holders = append(holders, idOf[peer])
				}
				row := map[string]any{"epoch": n, "server_id": x.leader, "account": rAcc, "stream": x.stream, "peer_name": peer,
					"current": s != "b", "offline": s == "o"}
				if s == "b" {
					row["lag"] = x.lag[peer]
				} else {
					row["lag"] = 0
				}
				table := "stream_replicas"
				if x.consumer != "" {
					table, row["consumer"] = "consumer_replicas", x.consumer
				}
				r.add(table, row)
			}
			for _, h := range holders {
				if x.consumer == "" {
					r.add("stream_stats", map[string]any{"epoch": n, "server_id": h, "account": rAcc, "stream": x.stream, "is_leader": h == x.leader})
				} else {
					r.add("consumer_stats", map[string]any{"epoch": n, "server_id": h, "account": rAcc, "stream": x.stream, "consumer": x.consumer,
						"is_leader": h == x.leader})
				}
			}
		}
	}
	return &storeprov.Provider{St: st, Interval: 30 * time.Second, Retention: 14 * 24 * time.Hour, Live: true,
		Clock: func() time.Time { return rT.Add(2 * time.Hour) }}
}

func TestReplayCatchUpsFromStore(t *testing.T) {
	p := catchUpStore(t)
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(30)))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range f.CatchUps {
		lag := func(v *int64) string {
			if v == nil {
				return "-"
			}
			return fmt.Sprint(*v)
		}
		got = append(got, fmt.Sprintf("%s %s %d-%d n%d peak %s last %s next %d current %d", c.Server, c.Entity.GetKey(), c.FromEpoch, c.LastEpoch,
			c.Behind, lag(c.PeakLag), lag(c.LastLag), c.NextEpoch, c.CurrentEpoch))
		if !c.From.Equal(rEpoch(int(c.FromEpoch))) || !c.Last.Equal(rEpoch(int(c.LastEpoch))) ||
			(c.CurrentEpoch != 0 && !c.Current.Equal(rEpoch(int(c.CurrentEpoch)))) {
			t.Errorf("%s %s times %v %v %v", c.Server, c.Entity.GetKey(), c.From, c.Last, c.Current)
		}
	}
	slices.Sort(got)
	want := []string{
		"nats-1 AAPP/ORDERS 7-9 n3 peak 300 last 100 next 10 current 10",
		"nats-1 AAPP/ORDERS/worker 7-7 n1 peak 40 last 40 next 8 current 8",
		"nats-2 AAPP/KV_cfg 28-30 n3 peak 18420 last 18420 next 0 current 0",
		"nats-2 AAPP/ORDERS 12-13 n2 peak 2500 last 2500 next 14 current 15",
		"nats-2 AAPP/ORDERS 20-20 n1 peak 2500 last 2500 next 21 current 21",
		"nats-2 AAPP/PAY 19-21 n3 peak 7000 last 7000 next 26 current 26",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("catch-ups:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if f.CatchUpsTruncated {
		t.Error("truncated")
	}

	res := api.FinishReplay(f)
	var steps []string
	for _, s := range res.GetSteps() {
		if s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_CATCH_UP {
			steps = append(steps, s.GetTitle()+" | "+s.GetBody())
		}
	}
	wantSteps := []string{
		"nats-1 catches up 1 stream and 1 consumer | Replicas of ORDERS and worker on ORDERS. Current again by 09:04:30. " +
			"The slowest, ORDERS, took 2 min and was at most 300 operations behind.",
		"nats-2 still catching up at B: KV_cfg 18,420 operations behind | Replicas of ORDERS, PAY and KV_cfg. " +
			"2 streams were current by 09:12:30, but when they caught up was not observed: " +
			"the leader did not report the replica from 09:06:30 to 09:07:00. Behind at B: KV_cfg.",
	}
	if strings.Join(steps, "\n") != strings.Join(wantSteps, "\n") {
		t.Errorf("steps:\n%s\nwant\n%s", strings.Join(steps, "\n"), strings.Join(wantSteps, "\n"))
	}
}

func TestReplayCatchUpsReadLimit(t *testing.T) {
	p := catchUpStore(t)
	defer storeprov.SetReplayRowLimit(2)()
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(30)))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.CatchUps) != 2 || !f.CatchUpsTruncated || f.CatchUps[0].FromEpoch != 7 {
		t.Errorf("catch-ups %d, truncated %v", len(f.CatchUps), f.CatchUpsTruncated)
	}
}
