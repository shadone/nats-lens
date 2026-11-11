package storeprov_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// sampleValue returns the value of the sample line that starts with
// series, or fails the test.
func sampleValue(t *testing.T, body, series string) float64 {
	t.Helper()
	for l := range strings.Lines(body) {
		if rest, ok := strings.CutPrefix(l, series+" "); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			if err != nil {
				t.Fatalf("%s: %v", l, err)
			}
			return v
		}
	}
	t.Errorf("missing %s", series)
	return 0
}

// Every timing family appears in /metrics once its work has run: the API
// call with its queries, the store's lock by operation, the queries by
// source and the background jobs a test can run without NATS. The runner's
// own jobs are checked in the runner's tests.
func TestTimingMetricsExported(t *testing.T) {
	st := emptyStore(t)
	const sys = `system="timing-scrape"`
	st.SetSystem("timing-scrape")
	for i := range 3 {
		if _, err := st.WriteSnapshot(ctx, snapshot(t, i)); err != nil {
			t.Fatal(err)
		}
	}
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	eng := &checks.Engine{Set: set, Log: apitest.Log(t), Interval: 30 * time.Second, Budget: time.Minute}
	if _, err := eng.Run(ctx, st, 3); err != nil {
		t.Fatal(err)
	}
	clock := t0.Add(2 * time.Hour)
	arch := &archive.Archiver{Store: st, Dest: archive.LocalDir{Dir: t.TempDir()}, Retention: 14 * day, Log: apitest.Log(t)}
	if err := arch.ExportHour(ctx, t0.Truncate(time.Hour), true, clock); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sweep(ctx, clock, 14*day); err != nil {
		t.Fatal(err)
	}
	srv, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set,
		Clock: func() time.Time { return clock }})
	must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{})))
	must(c.ListSystems(ctx, connect.NewRequest(&v1.ListSystemsRequest{})))
	if _, err := c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: "no-such-system"})); err == nil {
		t.Fatal("an unknown system answered")
	}
	body := scrape(t, srv.URL+"/metrics")

	for _, typ := range []string{
		"nats_lens_api_request_duration_seconds histogram",
		"nats_lens_api_request_queries histogram",
		"nats_lens_api_request_query_seconds histogram",
		"nats_lens_api_requests_in_flight gauge",
		"nats_lens_db_query_duration_seconds histogram",
		"nats_lens_store_lock_wait_seconds histogram",
		"nats_lens_store_lock_held_seconds histogram",
		"nats_lens_store_lock_waiters gauge",
		"nats_lens_job_duration_seconds histogram",
	} {
		if !strings.Contains(body, "# TYPE "+typ+"\n") {
			t.Errorf("missing # TYPE %s", typ)
		}
	}
	positive := []string{
		`nats_lens_api_request_duration_seconds_count{system="default",procedure="GetOverview",code="ok"}`,
		`nats_lens_api_request_queries_count{system="default",procedure="GetOverview"}`,
		`nats_lens_api_request_duration_seconds_count{system="all",procedure="ListSystems",code="ok"}`,
		`nats_lens_api_request_duration_seconds_count{system="unknown",procedure="GetOverview",code="invalid_argument"}`,
		`nats_lens_api_request_queries_sum{system="default",procedure="GetOverview"}`,
		`nats_lens_api_request_query_seconds_sum{system="default",procedure="GetOverview"}`,
		`nats_lens_db_query_duration_seconds_count{` + sys + `,source="api"}`,
		`nats_lens_db_query_duration_seconds_count{` + sys + `,source="metrics"}`,
		`nats_lens_db_query_duration_seconds_count{` + sys + `,source="archive"}`,
		`nats_lens_job_duration_seconds_count{` + sys + `,job="check_evaluation"}`,
		`nats_lens_job_duration_seconds_count{` + sys + `,job="archive_export"}`,
	}
	for _, op := range []string{"epoch", "check_apply", "loopback", "archive_record", "retention_sweep"} {
		positive = append(positive,
			`nats_lens_store_lock_wait_seconds_count{`+sys+`,op="`+op+`"}`,
			`nats_lens_store_lock_held_seconds_count{`+sys+`,op="`+op+`"}`)
	}
	for _, s := range positive {
		if v := sampleValue(t, body, s); v <= 0 {
			t.Errorf("%s is %g", s, v)
		}
	}
	if v := sampleValue(t, body, `nats_lens_store_lock_waiters{`+sys+`}`); v != 0 {
		t.Errorf("lock waiters %g after every write returned", v)
	}
	if v := sampleValue(t, body, `nats_lens_api_requests_in_flight{system="default",procedure="GetOverview"}`); v != 0 {
		t.Errorf("GetOverview in flight %g after it returned", v)
	}
}
