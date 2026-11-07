package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// microIdleStore: orders/get on O1, epochs every 5 minutes from 08:00 to 14:00.
// Fast (2 ms) from 08:00 to 10:00, idle (counters unchanged) until 13:00,
// slow (40 ms) from the 13:05 collection on. The hourly rollup holds 30
// hours of 2 ms before, hour 8 and 9 fast, hours 13 and 14 slow.
// lastStats is the state of the 14:00 STATS request; subjects are
// listed for the endpoint per epoch by subj.
func microIdleStore(t *testing.T, lastStats string, subj func(e int64) []string) (*store.Store, time.Time) {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	first := last.Add(-6 * time.Hour)
	started := first.Add(-time.Hour)
	var req, ns int64
	n := int64(6 * 12)
	for e := int64(0); e <= n; e++ {
		at := first.Add(time.Duration(e) * 5 * time.Minute)
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, [], [], [], 0, false, [], NULL)`, e+1, at, at.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		state := "ok"
		if e == n {
			state = lastStats
		}
		r.add("micro_requests", map[string]any{"epoch": e + 1, "label": "app", "account": "APP", "request": "STATS", "state": state, "replies": 1})
		r.add("micro_requests", map[string]any{"epoch": e + 1, "label": "app", "account": "APP", "request": "INFO", "state": "ok", "replies": 1})
		if state != "ok" {
			continue
		}
		switch {
		case at.Before(first.Add(2 * time.Hour)):
			req, ns = req+100, ns+100*2_000_000
		case at.Hour() >= 13:
			if at.After(last.Add(-time.Hour)) {
				req, ns = req+100, ns+100*40_000_000
			}
		}
		r.add("micro_stats", map[string]any{"epoch": e + 1, "account": "APP", "service": "orders", "instance_id": "O1", "started": started})
		r.add("micro_instances", map[string]any{"epoch": e + 1, "account": "APP", "service": "orders", "instance_id": "O1", "version": "1.0.0",
			"endpoints": `[]`})
		for _, s := range subj(e) {
			cols := map[string]any{"epoch": e + 1, "account": "APP", "service": "orders", "instance_id": "O1", "endpoint": "get",
				"num_requests": req, "num_errors": 0, "processing_time_ns": ns, "average_processing_time_ns": 0}
			if s != "" {
				cols["subject"] = s
			}
			r.add("micro_endpoints", cols)
		}
	}
	add := func(h time.Time, ms float64) {
		r.add("micro_endpoint_hours", map[string]any{"hour": h, "account": "APP", "service": "orders", "endpoint": "get",
			"requests": 1000, "errors": 0, "processing_time_ns": int64(ms * 1000 * 1e6), "epochs": 12})
	}
	for h := 40; h > 10; h-- {
		add(last.Add(-time.Duration(h)*time.Hour), 2)
	}
	add(last.Add(-6*time.Hour), 2)
	add(last.Add(-5*time.Hour), 2)
	add(last.Add(-time.Hour), 40)
	add(last, 40)
	return st, last
}

func microIdleGet(t *testing.T, st *store.Store, last time.Time) *v1.MicroService {
	t.Helper()
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 5 * time.Minute, Live: true, Clock: func() time.Time { return clock },
		Retention: 14 * day})
	res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Msg.GetServices() {
		if s.GetName() == "orders" {
			return s
		}
	}
	t.Fatalf("no orders: %v", res.Msg)
	return nil
}

// Idle hours between the fast hour and the slow one: the start is the
// first slow collection after the idle hours.
func TestMicroStoreSlowAfterIdleHours(t *testing.T) {
	st, last := microIdleStore(t, "ok", func(int64) []string { return []string{"orders.get"} })
	s := microIdleGet(t, st, last)
	e := s.GetEndpoints()[0]
	want := last.Add(-55 * time.Minute)
	if !e.GetSlow() || e.GetSlowSince() == nil || !e.GetSlowSince().AsTime().Equal(want) {
		t.Errorf("slow %v since %v, want %v (avg %v level %v)", e.GetSlow(), e.GetSlowSince().AsTime(), want, e.GetAverageMs(), e.GetLevelMs())
	}
	if s.GetInstanceSource() != v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_ANSWERED {
		t.Errorf("source %v", s.GetInstanceSource())
	}
}

// Subjects: two subjects of one name listed in order, subject the first;
// a subject-less row in the same epoch is not listed.
func TestMicroStoreSubjects(t *testing.T) {
	st, last := microIdleStore(t, "ok", func(int64) []string { return []string{"orders.get.v2", "orders.get", ""} })
	s := microIdleGet(t, st, last)
	e := s.GetEndpoints()[0]
	if !slices.Equal(e.GetSubjects(), []string{"orders.get", "orders.get.v2"}) || e.GetSubject() != "orders.get" {
		t.Errorf("subjects %v subject %q", e.GetSubjects(), e.GetSubject())
	}
	t.Logf("three series of one counter: %.2f req/s", e.GetRequestsPerSecond())
}

// The last STATS timed out and no subscription collection: the instances
// that last answered, by source LAST_ANSWERED.
func TestMicroStoreLastAnswered(t *testing.T) {
	st, last := microIdleStore(t, "timeout", func(int64) []string { return []string{"orders.get"} })
	s := microIdleGet(t, st, last)
	if s.GetInstanceSource() != v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_LAST_ANSWERED || s.GetInstances() != 1 || s.GetAnswered() {
		t.Errorf("after a timeout: source %v instances %d answered %v note %v", s.GetInstanceSource(), s.GetInstances(), s.GetAnswered(), s.GetNote())
	}
}
