package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// apiPt is one server's report of APP's JetStream API counters.
type apiPt struct {
	epoch         int64
	server        string
	total, errors int64
}

// apiRate replaces the subjects fixture's API counters with pts, records
// the given process start times (server ID to time) in server_stats, and
// reads APP's JetStream API row.
func apiRate(t *testing.T, pts []apiPt, starts map[string]time.Time) *v1.SubjectRow {
	t.Helper()
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	p := subjectsProvider(t, last)
	db := p.St.DB()
	if _, err := db.Exec(`DELETE FROM account_stats`); err != nil {
		t.Fatal(err)
	}
	for _, x := range pts {
		if _, err := db.Exec(`INSERT INTO account_stats (epoch, server_id, account, js_streams, js_api_total, js_api_errors) VALUES (?, ?, 'APP', 9, ?, ?)`,
			x.epoch, x.server, x.total, x.errors); err != nil {
			t.Fatal(err)
		}
		if at, ok := starts[x.server]; ok {
			if _, err := db.Exec(`INSERT INTO server_stats (epoch, server_id, start_time) VALUES (?, ?, ?)`, x.epoch, x.server, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, c := apitest.Serve(t, p)
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
	return subjRow(res, "APP", api.JetStreamAPIPattern)
}

// A server restarted just before epoch 12 reports 0 before the others'
// usage updates reach it, then the cluster's running total: its report
// within 60 seconds of its start is left out, so its one-interval jump is
// no rate.
func TestSubjectsAPIRateRestartedServer(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	var pts []apiPt
	for e := int64(10); e <= 13; e++ {
		for _, s := range []string{"S0", "S1"} {
			pts = append(pts, apiPt{e, s, 1_000_000 + 900*e, 9 * e})
		}
	}
	pts = append(pts, apiPt{12, "S2b", 0, 0}, apiPt{13, "S2b", 1_000_000 + 900*13, 9 * 13})
	epoch12 := last.Add(-subjStep)
	js := apiRate(t, pts, map[string]time.Time{"S2b": epoch12.Add(-2 * time.Second)})
	if js.GetApiPerSecond() != 3 || js.GetApiErrorShare() != 0.01 {
		t.Errorf("rate %v share %v, want 3 and 0.01", js.GetApiPerSecond(), js.GetApiErrorShare())
	}
}

// Equal rates over different spans: the total and the errors come from
// one server, the first by ID.
func TestSubjectsAPIRateTie(t *testing.T) {
	for range 10 {
		// S0's first report (10) lies before the window and S1's (11) in it:
		// both keep two or more reports and read 3 a second.
		pts := []apiPt{{10, "S0", 0, 0}, {11, "S0", 900, 9}, {12, "S0", 1800, 18}, {13, "S0", 2700, 27},
			{11, "S1", 0, 0}, {12, "S1", 900, 0}, {13, "S1", 1800, 90}}
		if js := apiRate(t, pts, nil); js.GetApiPerSecond() != 3 || js.GetApiErrorShare() != 0.01 {
			t.Fatalf("rate %v share %v, want 3 and S0's 0.01", js.GetApiPerSecond(), js.GetApiErrorShare())
		}
	}
}

// Before the account stores data anywhere, each server reports only the
// requests it answered, mostly the meta leader's: the highest server's
// rate is the best lower bound (a median would read 0 here).
func TestSubjectsAPIRateLocalOnly(t *testing.T) {
	var pts []apiPt
	for e := int64(11); e <= 13; e++ {
		pts = append(pts, apiPt{e, "S0", 0, 0}, apiPt{e, "S1", 900 * e, 0}, apiPt{e, "S2", 0, 0})
	}
	if js := apiRate(t, pts, nil); js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v, want the meta leader's 3", js.GetApiPerSecond())
	}
}
