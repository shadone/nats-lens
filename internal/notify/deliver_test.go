package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

func queueDelivery(t *testing.T, st *store.Store, at time.Time, epoch int64, n int) {
	t.Helper()
	var rows []store.OutboxRow
	for i := 0; i < n; i++ {
		m := BaselineMessage("example-prod", i, epoch)
		b, _ := m.Encode()
		rows = append(rows, store.OutboxRow{Channel: "ops", CreatedAt: at, Epoch: epoch, Kind: "baseline", DedupKey: m.DedupKey + string(rune('a'+i)), Message: b})
	}
	if err := st.ApplyNotify(context.Background(), store.NotifyChange{Outbox: rows}); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryRetriesDurableBatch(t *testing.T) {
	e := newEnv(t)
	now := r0
	var keys, bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		bodies = append(bodies, string(b))
		if len(keys) < 4 {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, e.st, now, 1, 1)
	d := &Delivery{Store: e.st, SystemID: "example-prod", Channels: []config.Channel{ch}, Clock: func() time.Time { return now }}
	for i, wait := range []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 0} {
		if _, err := d.Step(context.Background(), ch); err != nil {
			t.Fatal(err)
		}
		rows, _ := e.st.OutboxRows(context.Background(), "ops")
		if rows[0].Attempts != i+1 {
			t.Fatalf("attempts %+v", rows)
		}
		if wait > 0 {
			if rows[0].NextAttemptAt.Sub(now) != wait {
				t.Fatalf("wait %s", rows[0].NextAttemptAt.Sub(now))
			}
			queueDelivery(t, e.st, now, int64(i+2), 1)
			now = now.Add(wait)
			d = &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return now }}
		}
	}
	for i := range keys {
		if keys[i] != keys[0] || bodies[i] != bodies[0] {
			t.Fatal("retry batch changed")
		}
	}
	rows, _ := e.st.OutboxRows(context.Background(), "ops")
	if rows[0].LastError != "" {
		t.Fatalf("delivered row kept an error: %q", rows[0].LastError)
	}
	if rows[0].State != "delivered" || rows[1].Attempts != 0 {
		t.Fatalf("order %+v", rows)
	}
}

func TestDeliveryOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		age    time.Duration
		state  string
		wait   time.Duration
	}{{"refused", 400, 0, "refused", 0}, {"retry-after", 429, 0, "pending", 30 * time.Second}, {"capped", 503, 0, "pending", 10 * time.Minute}, {"give-up", 500, 25 * time.Hour, "failed", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "capped" {
					w.Header().Set("Retry-After", "999999")
				} else {
					w.Header().Set("Retry-After", "30")
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			ch := channel("ops")
			ch.URL = srv.URL
			queueDelivery(t, e.st, r0.Add(-tc.age), 1, 1)
			d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return r0 }}
			if _, err := d.Step(context.Background(), ch); err != nil {
				t.Fatal(err)
			}
			rows, _ := e.st.OutboxRows(context.Background(), "ops")
			if rows[0].State != tc.state {
				t.Fatalf("%+v", rows)
			}
			if tc.wait > 0 && rows[0].NextAttemptAt.Sub(r0) != tc.wait {
				t.Fatalf("%+v", rows)
			}
		})
	}
}

func TestDeliveryCrashAfterSuccess(t *testing.T) {
	e := newEnv(t)
	var keys, bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		bodies = append(bodies, string(b))
		w.WriteHeader(204)
	}))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, e.st, r0, 1, 1)
	rows, at, err := e.st.DeliveryBatch(context.Background(), "ops", r0, 100)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := Decode(rows[0].Message)
	b := Batch{SystemID: "example-prod", SentAt: at, Key: fmt.Sprintf("example-prod:ops:%d:%d", rows[0].ID, rows[0].ID), Messages: []Message{m}}
	s := NewSender(ch, "")
	defer s.Close()
	if r := s.Send(context.Background(), b); r.Outcome != Delivered {
		t.Fatal(r)
	}
	// Simulate a crash by omitting FinishDelivery after the successful response.
	queueDelivery(t, e.st, r0.Add(time.Second), 2, 1)
	d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return r0.Add(time.Minute) }}
	if _, err := d.Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if keys[0] != keys[1] || bodies[0] != bodies[1] {
		t.Fatal("crash changed batch identity or body")
	}
}

func TestDeliveryCancellationAndStoreLock(t *testing.T) {
	e := newEnv(t)
	started := make(chan struct{})
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, e.st, r0, 1, 1)
	d := &Delivery{Store: e.st, SystemID: "example-prod", Channels: []config.Channel{ch}}
	stop, cancelStop := context.WithCancel(context.Background())
	defer cancelStop()
	requests, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	done := make(chan struct{})
	go func() { d.Run(stop, requests); close(done) }()
	<-started
	cancelStop()
	select {
	case <-done:
		t.Fatal("in-flight request did not get shutdown grace")
	case <-time.After(20 * time.Millisecond):
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), time.Second)
	defer cancelLock()
	if err := e.st.ApplyNotify(lockCtx, store.NotifyChange{}); err != nil {
		t.Fatalf("HTTP held write lock: %v", err)
	}
	cancelRequest()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
	rows, _ := e.st.OutboxRows(context.Background(), "ops")
	if rows[0].State != "pending" || rows[0].Attempts != 0 {
		t.Fatalf("cancelled batch changed: %+v", rows)
	}
	// Restart delivers the same pending rows to the now-working receiver.
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer good.Close()
	ch.URL = good.URL
	if _, err := d.Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	rows, _ = e.st.OutboxRows(context.Background(), "ops")
	if rows[0].State != "delivered" {
		t.Fatal(rows)
	}
}

func TestDeliveryRateSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	now := r0
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, e.st, r0, 1, 101)
	makeDelivery := func() *Delivery {
		return &Delivery{Store: e.st, SystemID: "example-prod", Interval: time.Minute, Clock: func() time.Time { return now }}
	}
	if _, err := makeDelivery().Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.st.OutboxRows(context.Background(), "ops")
	if rows[99].State != "delivered" || rows[100].State != "pending" {
		t.Fatal("batch must stop at 100")
	}
	if wait, err := makeDelivery().Step(context.Background(), ch); err != nil || wait != time.Minute || calls != 1 {
		t.Fatalf("restart bypassed rate: wait=%s calls=%d err=%v", wait, calls, err)
	}
	now = now.Add(time.Minute)
	if _, err := makeDelivery().Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestDeliveryRetryAfterDoesNotShortenBackoff(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "1"); w.WriteHeader(503) }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, e.st, r0, 1, 1)
	d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return r0 }}
	if wait, err := d.Step(context.Background(), ch); err != nil || wait != 10*time.Second {
		t.Fatalf("wait=%s err=%v", wait, err)
	}
}

func TestDeliveryWaitsForNextEpoch(t *testing.T) {
	e := newEnv(t)
	e.apply(0)
	now := r0
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, e.st, r0, 1, 101)
	d := &Delivery{Store: e.st, SystemID: "example-prod", Interval: time.Minute, Clock: func() time.Time { return now }}
	if _, err := d.Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := d.Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("second successful batch in the same epoch")
	}
	e.apply(2 * time.Minute)
	if _, err := d.Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("new epoch did not release next batch")
	}
}

func TestDeliveryBackoffCapAndNotDue(t *testing.T) {
	e := newEnv(t)
	now := r0
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, e.st, r0, 1, 1)
	d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return now }}
	for i, seconds := range []int{10, 20, 40, 80, 160, 320, 600, 600} {
		wait, err := d.Step(context.Background(), ch)
		if err != nil || wait != time.Duration(seconds)*time.Second {
			t.Fatalf("attempt %d wait=%s err=%v", i, wait, err)
		}
		if _, err := d.Step(context.Background(), ch); err != nil {
			t.Fatal(err)
		}
		if calls != i+1 {
			t.Fatal("retried before due")
		}
		now = now.Add(wait)
	}
}

func TestDeliverySecretsNotPersistedOrLogged(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, "receiver-private-body")
	}))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL + "/private-path?key=private-query"
	ch.BearerTokenEnv = "DELIVERY_TEST_TOKEN"
	t.Setenv(ch.BearerTokenEnv, "private-bearer")
	var logs bytes.Buffer
	queueDelivery(t, e.st, r0, 1, 1)
	d := &Delivery{Store: e.st, SystemID: "example-prod", Clock: func() time.Time { return r0 }, Log: slog.New(slog.NewTextHandler(&logs, nil))}
	if _, err := d.Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	rows, err := e.st.OutboxRows(context.Background(), "ops")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-path", "private-query", "private-bearer", "receiver-private-body"} {
		if strings.Contains(fmt.Sprint(rows)+logs.String(), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	// An unreadable rotated secret is a retry, not a fatal start or refusal.
	ch.URL = ""
	ch.URLFile = filepath.Join(t.TempDir(), "missing-url")
	d.Clock = func() time.Time { return r0.Add(10 * time.Second) }
	if _, err := d.Step(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	rows, _ = e.st.OutboxRows(context.Background(), "ops")
	if rows[0].State != "pending" || rows[0].Attempts != 2 || rows[0].LastStatus != 0 {
		t.Fatal(rows)
	}
}

func TestDeliverySurvivesDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "outbox.duckdb")
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if calls == 1 {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	ch := channel("ops")
	ch.URL = srv.URL
	queueDelivery(t, st, r0, 1, 1)
	d := &Delivery{Store: st, SystemID: "example-prod", Clock: func() time.Time { return r0 }}
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d = &Delivery{Store: st, SystemID: "example-prod", Clock: func() time.Time { return r0.Add(5 * time.Second) }}
	if wait, err := d.Step(ctx, ch); err != nil || wait != 5*time.Second || calls != 1 {
		t.Fatalf("reopened retry wait=%s calls=%d err=%v", wait, calls, err)
	}
	d.Clock = func() time.Time { return r0.Add(10 * time.Second) }
	if _, err := d.Step(ctx, ch); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.OutboxRows(ctx, "ops")
	if rows[0].State != "delivered" || rows[0].Attempts != 2 || keys[0] != keys[1] {
		t.Fatal(rows, keys)
	}
}

func TestDeliveryChannelsIndependent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	slowStarted := make(chan struct{})
	healthySent := make(chan struct{})
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(slowStarted); <-release }))
	defer slow.Close()
	defer close(release)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(healthySent); w.WriteHeader(204) }))
	defer good.Close()
	queueDelivery(t, e.st, r0, 1, 1)
	if _, err := e.st.DB().ExecContext(ctx, `UPDATE notify_outbox SET channel='healthy'`); err != nil {
		t.Fatal(err)
	}
	queueDelivery(t, e.st, r0, 2, 1)
	ch := channel("ops")
	ch.URL = slow.URL
	other := channel("healthy")
	other.URL = good.URL
	stop, cancel := context.WithCancel(ctx)
	defer cancel()
	requests, cancelRequests := context.WithCancel(ctx)
	defer cancelRequests()
	d := &Delivery{Store: e.st, SystemID: "example-prod", Channels: []config.Channel{ch, other}}
	done := make(chan struct{})
	go func() { d.Run(stop, requests); close(done) }()
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow channel did not start")
	}
	select {
	case <-healthySent:
	case <-time.After(time.Second):
		t.Fatal("slow channel blocked healthy channel")
	}
	cancel()
	cancelRequests()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers did not stop")
	}
}
