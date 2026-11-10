package storeprov_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// SQL and Go keep the same runs at the 100-operation and 3-interval
// boundaries, and neither counts a restart before A.
func TestReplayCatchUpParity(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	proc := func(id, name string, first, last int) {
		r.add("servers", map[string]any{"server_id": id, "server_name": name, "version": "2.12.2", "start_time": rEpoch(first).Add(-10 * time.Second),
			"first_epoch": first, "last_epoch": last, "first_seen_at": rEpoch(first), "last_seen_at": rEpoch(last)})
	}
	proc("S0", "nats-0", 1, 14)
	proc("S1OLD", "nats-1", 1, 4)
	proc("S1NEW", "nats-1", 5, 14)
	// nats-2 restarted before A: no restart of Replay's.
	proc("S2OLD", "nats-2", 1, 1)
	proc("S2", "nats-2", 2, 14)
	type spec struct {
		stream, peer string
		behind       []int
		lag          int64
	}
	specs := []spec{
		{"SA", "nats-1", []int{8}, 10},       // restart's first +3 intervals: kept
		{"SB", "nats-1", []int{9}, 10},       // +4 intervals: dropped
		{"SC", "nats-2", []int{10, 11}, 100}, // n2, lag 100: kept
		{"SD", "nats-2", []int{10, 11}, 99},  // n2, lag 99: dropped
		{"SE", "nats-2", []int{10}, 5000},    // n1, no restart: dropped
		{"SF", "nats-2", []int{3}, 10},       // n1 after a restart before A: dropped
	}
	for n := 1; n <= 14; n++ {
		n1 := "S1OLD"
		if n >= 5 {
			n1 = "S1NEW"
		}
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 3, 3, ['nats-0', 'nats-1', 'nats-2'], ['nats-0', 'nats-1', 'nats-2'], [], 0, false, [], NULL)`,
			n, rEpoch(n), rEpoch(n)); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"S0", n1, "S2"} {
			r.add("epoch_endpoints", map[string]any{"epoch": n, "server_id": id, "endpoint": "JSZ", "state": "ok"})
		}
		for _, s := range specs {
			for _, peer := range []string{"nats-1", "nats-2"} {
				b := peer == s.peer && slices.Contains(s.behind, n)
				lag := int64(0)
				if b {
					lag = s.lag
				}
				r.add("stream_replicas", map[string]any{"epoch": n, "server_id": "S0", "account": rAcc, "stream": s.stream, "peer_name": peer,
					"current": !b, "offline": false, "lag": lag})
			}
			for _, h := range []string{"S0", n1, "S2"} {
				r.add("stream_stats", map[string]any{"epoch": n, "server_id": h, "account": rAcc, "stream": s.stream, "is_leader": h == "S0"})
			}
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Retention: 14 * 24 * time.Hour, Live: true,
		Clock: func() time.Time { return rT.Add(2 * time.Hour) }}
	f, err := p.Replay(ctx, replayReq(rEpoch(2), rEpoch(14)))
	if err != nil {
		t.Fatal(err)
	}
	var sqlGot []string
	for _, c := range f.CatchUps {
		sqlGot = append(sqlGot, c.Server+" "+c.Entity.GetName())
	}
	if got := strings.Join(sqlGot, ","); got != "nats-1 SA,nats-2 SC" {
		t.Errorf("SQL runs = %s (restarts %d)", got, len(f.Restarts))
	}
	// Go keeps every run SQL returns, and drops the ones SQL drops when
	// handed them.
	res := api.FinishReplay(f)
	var goGot []string
	for _, s := range res.GetSteps() {
		if s.GetKind() == v1.ReplayStepKind_REPLAY_STEP_KIND_CATCH_UP {
			for _, e := range s.GetEntities()[1:] {
				goGot = append(goGot, s.GetEntities()[0].GetKey()+" "+e.GetName())
			}
		}
	}
	if got := strings.Join(goGot, ","); got != "nats-1 SA,nats-2 SC" {
		t.Errorf("Go steps = %s", got)
	}
}
