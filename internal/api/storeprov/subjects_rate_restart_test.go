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

// The cases of a server whose reports may hold its own count before the
// others' totals arrive. apiSteady: S0 and S1 report a shared total growing 900 per 5-minute epoch
// (3 a second) in epochs 10 to 13.
func apiSteady() []apiPt {
	var pts []apiPt
	for e := int64(10); e <= 13; e++ {
		for _, s := range []string{"S0", "S1"} {
			pts = append(pts, apiPt{e, s, 1_000_000 + 900*e, 9 * e})
		}
	}
	return pts
}

var apiLast = time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)

// Restarted twice in the window: S2a (epoch 11) and S2b (epoch 12), each
// collected 2 s after start with 0.
func TestSubjectsAPIRateRestartTwice(t *testing.T) {
	pts := append(apiSteady(),
		apiPt{11, "S2a", 0, 0}, apiPt{12, "S2a", 1_000_000 + 900*12, 0},
		apiPt{12, "S2b", 0, 0}, apiPt{13, "S2b", 1_000_000 + 900*13, 0})
	e11, e12 := apiLast.Add(-2*subjStep), apiLast.Add(-subjStep)
	js := apiRate(t, pts, map[string]time.Time{"S2a": e11.Add(-2 * time.Second), "S2b": e12.Add(-2 * time.Second)})
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v, want 3", js.GetApiPerSecond())
	}
}

// The restarted server's clock is 60 s behind the collector's: its
// start_time reads 60 s early, so a report 2 s after the real start looks
// 62 s old and is kept.
func TestSubjectsAPIRateClockBehind(t *testing.T) {
	pts := append(apiSteady(), apiPt{12, "S2b", 0, 0}, apiPt{13, "S2b", 1_000_000 + 900*13, 0})
	e12 := apiLast.Add(-subjStep)
	js := apiRate(t, pts, map[string]time.Time{"S2b": e12.Add(-62 * time.Second)})
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v, want 3 (server clock 60 s behind)", js.GetApiPerSecond())
	}
}

// The restarted server's clock is 60 s ahead: start_time reads after the
// collection; the report is left out, as it should be.
func TestSubjectsAPIRateClockAhead(t *testing.T) {
	pts := append(apiSteady(), apiPt{12, "S2b", 0, 0}, apiPt{13, "S2b", 1_000_000 + 900*13, 0})
	e12 := apiLast.Add(-subjStep)
	js := apiRate(t, pts, map[string]time.Time{"S2b": e12.Add(58 * time.Second)})
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v, want 3 (server clock 60 s ahead)", js.GetApiPerSecond())
	}
}

// The 0 is still there 90 s after the start (routes or the account came up
// late), and the total arrives by the next collection.
func TestSubjectsAPIRateLateJump(t *testing.T) {
	pts := append(apiSteady(), apiPt{12, "S2b", 0, 0}, apiPt{13, "S2b", 1_000_000 + 900*13, 0})
	e12 := apiLast.Add(-subjStep)
	js := apiRate(t, pts, map[string]time.Time{"S2b": e12.Add(-90 * time.Second)})
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v, want 3 (0 still held 90 s after start)", js.GetApiPerSecond())
	}
}

// No start time for the restarted server (VARZ and STATSZ failed): kept.
func TestSubjectsAPIRateNoStart(t *testing.T) {
	pts := append(apiSteady(), apiPt{12, "S2b", 0, 0}, apiPt{13, "S2b", 1_000_000 + 900*13, 0})
	js := apiRate(t, pts, nil)
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v, want 3 (no start time)", js.GetApiPerSecond())
	}
}

// The rate reads the window and the hour before it, bounded by epoch, of
// 14 days of epochs 30 seconds apart on 9 servers with 20 accounts (7.3
// million rows), well inside the call's time.
func TestSubjectsAPIRateLongHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	db := st.DB()
	const epochs = 14 * 24 * 120
	first := last.Add(-time.Duration(epochs-1) * 30 * time.Second)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO epochs SELECT i, ?::TIMESTAMP + to_seconds(30 * (i - 1)), ?::TIMESTAMP + to_seconds(30 * (i - 1)) + INTERVAL 1 SECOND,
  0, 9, 9, [], [], [], 0, false, [], NULL FROM range(1, ? + 1) t(i)`, first.Format(time.DateTime), first.Format(time.DateTime), epochs)
	exec(`INSERT INTO account_stats (epoch, server_id, account, js_streams, js_api_total, js_api_errors)
SELECT e, 'S' || s, 'A' || a, 1, 1000 + 90 * e, e FROM range(1, ? + 1) x(e), range(9) y(s), range(20) z(a)`, epochs)
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return last.Add(10 * time.Second) },
		Retention: 14 * day}
	_, c := apitest.Serve(t, p)
	start := time.Now()
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	took := time.Since(start)
	js := subjRow(res, "A0", api.JetStreamAPIPattern)
	t.Logf("7.3 million account reports: %s, A0 at %v/s", took, js.GetApiPerSecond())
	if js.GetApiPerSecond() != 3 || took > 10*time.Second {
		t.Errorf("rate %v in %s", js.GetApiPerSecond(), took)
	}
}
