package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReviewRecentOutcomeSurvivesSweep(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	queueDelivery(t, e.st, r0.Add(-8*24*time.Hour), 1, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return r0 }}
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	before, _ := e.st.OutboxRows(ctx, "ops")
	if len(before) != 1 || before[0].State != "failed" {
		t.Fatal(before)
	}
	res, err := e.st.SweepNotifications(ctx, r0.Add(time.Second), 14*24*time.Hour, []string{"ops"})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := e.st.OutboxRows(ctx, "ops")
	if len(after) != 1 {
		t.Fatalf("one-second-old give-up erased: sweep=%+v remaining=%d", res, len(after))
	}
}
func TestReviewRebuiltBaselineHasNewBatchKey(t *testing.T) {
	e := newEnv(t, channel("ops"))
	ctx := context.Background()
	e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		w.WriteHeader(204)
	}))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return e.at }}
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	rb := rebuild(t, e.st, e.at)
	r := envOn(t, rb, "example-prod", channel("ops"))
	r.epoch, r.at = e.epoch, e.at
	r.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "warning"))
	d = &Delivery{Store: rb, SystemID: "example-prod", Clock: func() time.Time { return r.at }}
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatal(keys)
	}
	if keys[0] == keys[1] {
		t.Fatalf("different baselines before/after archive rebuild reuse Idempotency-Key %q", keys[0])
	}
}

func TestReviewSweepPreservesEpochRate(t *testing.T) {
	e := newEnv(t)
	e.apply(0)
	ctx := context.Background()
	queueDelivery(t, e.st, r0.Add(-8*24*time.Hour), 1, 101)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	d := &Delivery{Store: e.st, SystemID: "example-prod", Interval: time.Minute, Clock: func() time.Time { return r0 }}
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.SweepNotifications(ctx, r0.Add(time.Second), 14*24*time.Hour, []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("sweep released another successful batch in same epoch: calls=%d", calls)
	}
}

// Watermarks must outlive all retained outbox rows, including when collection
// is slower than retention and a sweep removes the last stored epoch.
func TestDeliveryRateOutlivesSweptOutcomes(t *testing.T) {
	for _, status := range []int{204, 400} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			e := newEnv(t)
			e.apply(0)
			ctx := context.Background()
			queueDelivery(t, e.st, r0, 1, 101)
			now := r0
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(status) }))
			defer srv.Close()
			ch := channel("ops")
			ch.URL = srv.URL
			step := func() {
				t.Helper()
				d := &Delivery{Store: e.st, SystemID: "example-prod", Interval: 30 * 24 * time.Hour, Clock: func() time.Time { return now }}
				if _, err := d.Step(ctx, ch); err != nil {
					t.Fatal(err)
				}
			}
			step()
			now = r0.Add(8 * 24 * time.Hour)
			res, err := e.st.SweepNotifications(ctx, now, 14*24*time.Hour, []string{"ops"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Outbox != 100 {
				t.Fatalf("did not exercise swept history: %+v", res)
			}
			step()
			if calls != 1 {
				t.Fatal("sweep bypassed long interval", calls)
			}
			// A new epoch still cannot bypass the interval watermark.
			e.apply(8 * 24 * time.Hour)
			step()
			if calls != 1 {
				t.Fatal("new epoch bypassed long interval", calls)
			}
			now = r0.Add(31 * 24 * time.Hour)
			if status == 204 {
				if _, err := e.st.DB().Exec(`DELETE FROM epochs`); err != nil {
					t.Fatal(err)
				}
				step()
				if calls != 1 {
					t.Fatal("missing epochs bypassed success watermark", calls)
				}
			}
			e.apply(31 * 24 * time.Hour)
			step()
			if calls != 2 {
				t.Fatal("new epoch after interval did not release batch", calls)
			}
		})
	}
}

func TestDeliveryRecentOutcomeMinimumReportingWindow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	queueDelivery(t, e.st, r0.Add(-8*24*time.Hour), 1, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return r0 }}
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	for _, age := range []time.Duration{time.Second, 30 * time.Minute, time.Hour} {
		if _, err := e.st.SweepNotifications(ctx, r0.Add(age), time.Second, []string{"ops"}); err != nil {
			t.Fatal(err)
		}
		rows, err := e.st.OutboxRows(ctx, "ops")
		if err != nil || len(rows) != 1 {
			t.Fatalf("reporting window at %s: %+v, %v", age, rows, err)
		}
	}
	res, err := e.st.SweepNotifications(ctx, r0.Add(time.Hour+time.Second), time.Second, []string{"ops"})
	if err != nil || res.Outbox != 1 {
		t.Fatalf("expired outcome: %+v, %v", res, err)
	}
}
