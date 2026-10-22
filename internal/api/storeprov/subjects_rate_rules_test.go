package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// epochPt is one server's report of an account's API total in an epoch.
type epochPt struct {
	e    int64
	s, a string
	v    int64
}

// apiRate30 builds a store of 30-second epochs 1..n ending at 14:00, the
// reports, and the start times in server_stats and servers by server ID,
// and reads the account's API row for rng (nil: the last day).
func apiRate30(t *testing.T, n int64, pts []epochPt, ssStart, svStart map[string]time.Time, rng *v1.TimeRange, acct string) *v1.SubjectRow {
	t.Helper()
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	db := st.DB()
	first := last.Add(-time.Duration(n-1) * 30 * time.Second)
	ex := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	ex(`INSERT INTO epochs SELECT i, ?::TIMESTAMP + to_seconds(30 * (i - 1)), ?::TIMESTAMP + to_seconds(30 * (i - 1)) + INTERVAL 1 SECOND,
  0, 9, 9, [], [], [], 0, false, [], NULL FROM range(1, ? + 1) t(i)`, first.Format(time.DateTime), first.Format(time.DateTime), n)
	seen := map[string]bool{}
	for _, p := range pts {
		ex(`INSERT INTO account_stats (epoch, server_id, account, js_streams, js_api_total, js_api_errors) VALUES (?, ?, ?, 1, ?, 0)`, p.e, p.s, p.a, p.v)
		if at, ok := ssStart[p.s]; ok {
			ex(`INSERT OR IGNORE INTO server_stats (epoch, server_id, start_time) VALUES (?, ?, ?)`, p.e, p.s, at)
		}
		if at, ok := svStart[p.s]; ok && !seen[p.s] {
			seen[p.s] = true
			ex(`INSERT INTO servers (server_id, server_name, start_time, first_epoch, last_epoch, first_seen_at, last_seen_at) VALUES (?, ?, ?, 1, ?, now(), now())`, p.s, p.s, at, n)
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return last.Add(10 * time.Second) }, Retention: 14 * day}
	_, c := apitest.Serve(t, p)
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{Range: rng})))
	return subjRow(res, acct, api.JetStreamAPIPattern)
}

// epochAt is when epoch e of n started.
func epochAt(n, e int64) time.Time {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	return last.Add(-time.Duration(n-e) * 30 * time.Second)
}

// steady30 is S0 reporting APP's total growing 3 a second over n epochs.
func steady30(n int64) []epochPt {
	var pts []epochPt
	for e := int64(1); e <= n; e++ {
		pts = append(pts, epochPt{e, "S0", "APP", 1_000_000 + 90*e})
	}
	return pts
}

// A server restarted 2 s before epoch 98 reports 0 there and still 0 at
// 99, 32 s after its start, then the total: the settle rule leaves out the
// second 0 (the first is its first report).
func TestSubjectsAPIRateSettle(t *testing.T) {
	const n = 100
	pts := append(steady30(n), epochPt{98, "S2", "APP", 0}, epochPt{99, "S2", "APP", 0}, epochPt{100, "S2", "APP", 1_000_000 + 90*100})
	js := apiRate30(t, n, pts, map[string]time.Time{"S2": epochAt(n, 98).Add(-2 * time.Second)}, nil, nil, "APP")
	if js.GetApiPerSecond() != 3 {
		t.Errorf("settle via server_stats: rate %v", js.GetApiPerSecond())
	}
}

// The same with no start time in server_stats: servers.start_time
// stands in.
func TestSubjectsAPIRateStartFallback(t *testing.T) {
	const n = 100
	pts := append(steady30(n), epochPt{98, "S2", "APP", 0}, epochPt{99, "S2", "APP", 0}, epochPt{100, "S2", "APP", 1_000_000 + 90*100})
	js := apiRate30(t, n, pts, nil, map[string]time.Time{"S2": epochAt(n, 98).Add(-2 * time.Second)}, nil, "APP")
	if js.GetApiPerSecond() != 3 {
		t.Errorf("settle via servers fallback: rate %v", js.GetApiPerSecond())
	}
}

// The store's history begins inside the rate window (retention cut the
// rest): the first kept report has none before it and is left out, and
// the rest give the rate; with two epochs kept one report is left and
// there is no rate.
func TestSubjectsAPIRateRetentionEdge(t *testing.T) {
	const n = 20
	if js := apiRate30(t, n, steady30(n), nil, nil, nil, "APP"); js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v", js.GetApiPerSecond())
	}
	if js := apiRate30(t, 2, steady30(2), nil, nil, nil, "APP"); js.ApiPerSecond != nil {
		t.Errorf("2 epochs kept: rate %v, want none", js.GetApiPerSecond())
	}
}

// A new account on long-lived servers: each server's first report holds
// only the requests it answered (7), the next the cluster's total, which
// then grows 1 a second. The first report is left out, so the jump is
// not read as requests.
func TestSubjectsAPIRateNewAccount(t *testing.T) {
	const n = 100
	pts := steady30(n)
	for e := int64(95); e <= n; e++ {
		v := 50_000 + 30*(e-95)
		if e == 95 {
			v = 7
		}
		pts = append(pts, epochPt{e, "S0", "NEW", v}, epochPt{e, "S1", "NEW", v})
		pts = append(pts, epochPt{e, "S1", "APP", 1_000_000 + 90*e})
	}
	js := apiRate30(t, n, pts, nil, nil, nil, "NEW")
	if js.GetApiPerSecond() != 1 {
		t.Errorf("new account rate %v, want 1", js.GetApiPerSecond())
	}
}
