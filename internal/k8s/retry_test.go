package k8s

import (
	"bytes"
	"context"
	"errors"
	"io"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/events"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	clocktesting "k8s.io/utils/clock/testing"
)

// timedWatch advances only the stream clock, after construction, without sleeping.
type timedWatch struct {
	ch    chan watch.Event
	once  bool
	life  time.Duration
	clock *clocktesting.FakeClock
}

func (w *timedWatch) Stop() {}
func (w *timedWatch) ResultChan() <-chan watch.Event {
	if !w.once {
		w.once = true
		w.clock.Step(w.life)
	}
	return w.ch
}
func closedWatch(fc *clocktesting.FakeClock, life time.Duration, evs ...watch.Event) watch.Interface {
	ch := make(chan watch.Event, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return &timedWatch{ch: ch, clock: fc, life: life}
}

func TestRetryShortClosuresAndRelists(t *testing.T) {
	for _, relist := range []bool{false, true} {
		t.Run(map[bool]string{false: "short", true: "short and gone"}[relist], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fc := clocktesting.NewFakeClock(t0)
			rec := &sink{}
			var waits []time.Duration
			calls, lists := 0, 0
			src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
				lists++
				return nil, "100", "", nil
			}}
			src.watch = func(_ context.Context, opts metav1.ListOptions) (watch.Interface, error) {
				calls++
				if opts.ResourceVersion != "100" {
					t.Errorf("RV %q", opts.ResourceVersion)
				}
				if relist && calls%2 == 0 {
					return nil, apierrors.NewResourceExpired("expired")
				}
				return closedWatch(fc, 10*time.Millisecond), nil
			}
			l := &loop{src: src, out: make(chan update, 32), record: rec.Record, gapAfter: 10 * time.Second, now: fc.Now, watchClock: fc, log: quietLog()}
			l.sleep = func(_ context.Context, d time.Duration) {
				waits = append(waits, d)
				fc.Step(d)
				if len(waits) == 9 {
					cancel()
				}
			}
			l.run(ctx)
			want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute, time.Minute, time.Minute}
			if len(waits) != len(want) {
				t.Fatalf("waits %v", waits)
			}
			for i := range want {
				if waits[i] != want[i] {
					t.Errorf("retry %d = %s, want %s", i, waits[i], want[i])
				}
			}
			if l.failingSince.IsZero() || len(rec.all()) != 1 || rec.all()[0].Kind != KindNotWatching {
				t.Errorf("outage lost: since=%v events=%v", l.failingSince, rec.kinds())
			}
			// Every short closure relists before the next watch; so does a 410.
			if lists < len(waits) {
				t.Errorf("lists = %d after %d short closures", lists, len(waits))
			}
		})
	}
}

func TestRetryAuthLoggingAndRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := clocktesting.NewFakeClock(t0)
	rec := &sink{}
	var logs bytes.Buffer
	calls := 0
	src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		return nil, "100", "", nil
	}}
	src.watch = func(context.Context, metav1.ListOptions) (watch.Interface, error) {
		calls++
		if calls <= 200 {
			switch calls % 3 {
			case 0:
				return nil, apierrors.NewUnauthorized("SECRET\n" + strings.Repeat("x", 10000))
			case 1:
				return nil, apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", errors.New("SECRET"))
			default:
				return nil, errors.New("SECRET")
			}
		}
		return closedWatch(fc, 0, watch.Event{Type: watch.Bookmark, Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "101"}}}), nil
	}
	l := &loop{src: src, out: make(chan update, 4), record: rec.Record, gapAfter: 10 * time.Second, now: fc.Now, watchClock: fc, log: slog.New(slog.NewTextHandler(&logs, nil))}
	// Advance less than retry duration to exercise the logging bound independently
	// of backoff; a bookmark recovers, then its short closure starts a new outage.
	l.sleep = func(_ context.Context, d time.Duration) {
		fc.Step(10 * time.Millisecond)
		if calls > 200 {
			cancel()
		}
	}
	l.run(ctx)
	text := logs.String()
	if strings.Contains(text, "SECRET") || len(text) > 2500 {
		t.Errorf("unbounded or sensitive logs: bytes=%d secret=%v", len(text), strings.Contains(text, "SECRET"))
	}
	if n := strings.Count(text, "level=WARN"); n != 2 {
		t.Errorf("warnings=%d want 2 (one per outage)", n)
	}
	if !strings.Contains(text, "suppressed=199") || !strings.Contains(text, "retries=200") {
		t.Errorf("missing recovery counts: %.1000s", text)
	}
	if l.backoff != 2*time.Second {
		t.Errorf("bookmark did not reset retry: %s", l.backoff)
	}
}

func TestRetryQuietWatchRecoversWhileOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := clocktesting.NewFakeClock(t0)
	recorded := make(chan events.Event, 8)
	entered := make(chan struct{})
	calls := 0
	src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		return nil, "100", "", nil
	}}
	src.watch = func(context.Context, metav1.ListOptions) (watch.Interface, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("unavailable")
		}
		return &enteredWatch{ch: make(chan watch.Event), entered: entered}, nil
	}
	l := &loop{src: src, out: make(chan update, 4), record: func(e events.Event) { recorded <- e }, gapAfter: time.Second, now: fc.Now, watchClock: fc, log: quietLog(), sleep: func(context.Context, time.Duration) { fc.Step(2 * time.Second) }}
	done := make(chan struct{})
	go func() { defer close(done); l.run(ctx) }()
	defer func() { cancel(); <-done }()
	<-entered
	select {
	case e := <-recorded:
		t.Errorf("construction recovered prematurely: %s", e.Kind)
	default:
	}
	fc.Step(minWatchLife)
	select {
	case e := <-recorded:
		if e.Kind != KindGap || rawOf(t, e)["seconds"] != float64(3) {
			t.Errorf("gap %+v", e)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("quiet open watch did not recover")
	}
}

type enteredWatch struct {
	ch      chan watch.Event
	entered chan struct{}
	once    bool
}

func (w *enteredWatch) Stop() {}
func (w *enteredWatch) ResultChan() <-chan watch.Event {
	if !w.once {
		w.once = true
		close(w.entered)
	}
	return w.ch
}

// A GKE API server closes a watch from a resource version it no longer has
// at once and cleanly, without 410 Gone. The loop must list again and watch
// from the fresh version, not retry the stale one forever.
func TestRetryShortClosureFromStaleVersionRelists(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := clocktesting.NewFakeClock(t0)
	rec := &sink{}
	var waits []time.Duration
	var rvs []string
	lists := 0
	src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		lists++
		return nil, map[bool]string{true: "100", false: "200"}[lists == 1], "", nil
	}}
	src.watch = func(_ context.Context, opts metav1.ListOptions) (watch.Interface, error) {
		rvs = append(rvs, opts.ResourceVersion)
		switch {
		case len(rvs) > 10 || len(rvs) > 1 && rvs[len(rvs)-2] == "200":
			cancel()
			return nil, context.Canceled
		case opts.ResourceVersion == "200":
			return closedWatch(fc, 2*time.Second), nil
		default:
			return closedWatch(fc, 10*time.Millisecond), nil
		}
	}
	l := &loop{src: src, out: make(chan update, 32), record: rec.Record, gapAfter: 10 * time.Second, now: fc.Now, watchClock: fc, log: quietLog()}
	l.sleep = func(_ context.Context, d time.Duration) {
		waits = append(waits, d)
		fc.Step(d)
	}
	l.run(ctx)
	if lists != 2 || strings.Join(rvs, ",") != "100,200,200" {
		t.Errorf("lists=%d watched from %v, want a relist and a watch from 200", lists, rvs)
	}
	if len(waits) != 1 || waits[0] != time.Second {
		t.Errorf("waits %v", waits)
	}
	if !l.failingSince.IsZero() || l.lastErr != nil || len(rec.all()) != 0 {
		t.Errorf("not recovered: since=%v err=%v events=%v", l.failingSince, l.lastErr, rec.kinds())
	}
}

func TestRetrySuccessfulListDoesNotRecoverWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := clocktesting.NewFakeClock(t0)
	lists := 0
	var waits []time.Duration
	src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		lists++
		if lists <= 2 {
			return nil, "", "", errors.New("list failed")
		}
		return nil, "100", "", nil
	}, watch: func(context.Context, metav1.ListOptions) (watch.Interface, error) {
		return nil, errors.New("watch failed")
	}}
	l := &loop{src: src, out: make(chan update, 4), record: func(events.Event) {}, gapAfter: time.Minute, now: fc.Now, watchClock: fc, log: quietLog(), sleep: func(_ context.Context, d time.Duration) {
		waits = append(waits, d)
		fc.Step(d)
		if len(waits) == 3 {
			cancel()
		}
	}}
	l.run(ctx)
	if len(waits) != 3 || waits[2] != 4*time.Second || !l.failingSince.Equal(t0) {
		t.Errorf("list reset outage: waits=%v since=%v", waits, l.failingSince)
	}
}

func TestRetryWarningSummariesArePerSource(t *testing.T) {
	fc := clocktesting.NewFakeClock(t0)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	l := &loop{src: &source{name: "events in example"}, record: func(events.Event) {}, gapAfter: time.Hour, now: fc.Now, log: logger, backoff: minBackoff, sleep: func(context.Context, time.Duration) { fc.Step(time.Second) }}
	for i := 0; i < 200; i++ {
		l.fail(context.Background(), "watch", errors.New("SECRET"))
	}
	l.recovered()
	other := &loop{src: &source{name: "pods in example"}, record: func(events.Event) {}, gapAfter: time.Hour, now: fc.Now, log: logger, backoff: minBackoff, sleep: func(context.Context, time.Duration) {}}
	other.fail(context.Background(), "list", errors.New("SECRET"))
	text := logs.String()
	if n := strings.Count(text, "level=WARN"); n != 5 {
		t.Errorf("warnings %d want 5", n)
	}
	if !strings.Contains(text, "suppressed=59") || !strings.Contains(text, "suppressed=19") || !strings.Contains(text, "retries=200") {
		t.Errorf("missing summaries: %.1200s", text)
	}
	if strings.Contains(text, "SECRET") {
		t.Error("error leaked")
	}
}

// These replace the wall-clock review regressions with deterministic lifetimes.
func TestReviewFalseGapAfterShortFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := clocktesting.NewFakeClock(t0)
	timestamps := &clock{t: t0}
	rec := &sink{}
	calls := 0
	var waits []time.Duration
	src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		return nil, "100", "", nil
	}}
	src.watch = func(context.Context, metav1.ListOptions) (watch.Interface, error) {
		calls++
		switch calls {
		case 1:
			return nil, errors.New("temporary")
		case 2:
			return closedWatch(fc, 8*time.Minute), nil
		default:
			cancel()
			return nil, context.Canceled
		}
	}
	l := &loop{src: src, out: make(chan update, 4), record: rec.Record, gapAfter: 30 * time.Second, now: timestamps.now, watchClock: fc, log: quietLog(), sleep: func(_ context.Context, d time.Duration) { waits = append(waits, d); timestamps.add(d); fc.Step(d) }}
	l.run(ctx)
	if len(waits) != 1 || waits[0] != time.Second || len(rec.all()) != 0 || !l.failingSince.IsZero() {
		t.Errorf("normal timeout: waits=%v events=%v since=%v", waits, rec.kinds(), l.failingSince)
	}
}

func TestRetryPaginationAndCancellation(t *testing.T) {
	fc := clocktesting.NewFakeClock(t0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	src := &source{name: "pods in example", list: func(_ context.Context, opts metav1.ListOptions) ([]runtime.Object, string, string, error) {
		calls++
		if opts.Limit != listPage {
			t.Errorf("limit %d", opts.Limit)
		}
		if calls == 1 {
			return []runtime.Object{&metav1.PartialObjectMetadata{}}, "100", "next", nil
		}
		if opts.Continue != "next" {
			t.Errorf("continue %q", opts.Continue)
		}
		return nil, "100", "", nil
	}, watch: func(_ context.Context, opts metav1.ListOptions) (watch.Interface, error) {
		if opts.ResourceVersion != "100" || !opts.AllowWatchBookmarks || opts.TimeoutSeconds == nil || *opts.TimeoutSeconds < 300 || *opts.TimeoutSeconds >= 600 {
			t.Errorf("watch options %+v", opts)
		}
		cancel()
		return closedWatch(fc, 0), nil
	}}
	var logs bytes.Buffer
	rec := &sink{}
	l := &loop{src: src, out: make(chan update, 4), record: rec.Record, gapAfter: time.Second, now: fc.Now, watchClock: fc, log: slog.New(slog.NewTextHandler(&logs, nil)), sleep: func(context.Context, time.Duration) { t.Error("canceled watch retried") }}
	l.run(ctx)
	if calls != 2 || len(rec.all()) != 0 || logs.Len() != 0 {
		t.Errorf("cancel: pages=%d events=%v logs=%s", calls, rec.kinds(), logs.String())
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRetryClientEmptyHTTPWatch(t *testing.T) {
	calls := 0
	client, err := kubernetes.NewForConfigAndClient(&rest.Config{Host: "https://example.com"}, &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("watch") != "true" || r.URL.Query().Get("resourceVersion") != "100" {
			t.Errorf("watch URL %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := clocktesting.NewFakeClock(t0)
	var waits []time.Duration
	src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		return nil, "100", "", nil
	}, watch: client.EventsV1().Events("example").Watch}
	l := &loop{src: src, out: make(chan update, 4), record: func(events.Event) {}, gapAfter: time.Minute, now: fc.Now, watchClock: fc, log: quietLog(), sleep: func(_ context.Context, d time.Duration) {
		waits = append(waits, d)
		fc.Step(d)
		if len(waits) == 3 {
			cancel()
		}
	}}
	l.run(ctx)
	if calls != 3 || len(waits) != 3 || waits[0] != time.Second || waits[1] != 2*time.Second || waits[2] != 4*time.Second {
		t.Errorf("HTTP empty streams: calls=%d waits=%v", calls, waits)
	}
}

func TestRetryClientAuthHTTPRecovery(t *testing.T) {
	calls := 0
	client, err := kubernetes.NewForConfigAndClient(&rest.Config{Host: "https://example.com"}, &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		code, body := http.StatusUnauthorized, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Unauthorized","code":401,"message":"SECRET"}`
		if calls%2 == 0 {
			code, body = http.StatusForbidden, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403,"message":"SECRET"}`
		}
		if calls > 200 {
			code, body = http.StatusOK, `{"type":"BOOKMARK","object":{"kind":"Event","apiVersion":"events.k8s.io/v1","metadata":{"resourceVersion":"101"}}}`+"\n"
		}
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := clocktesting.NewFakeClock(t0)
	rec := &sink{}
	var logs bytes.Buffer
	var waits []time.Duration
	src := &source{name: "events in example", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		return nil, "100", "", nil
	}, watch: client.EventsV1().Events("example").Watch}
	l := &loop{src: src, out: make(chan update, 4), record: rec.Record, gapAfter: 10 * time.Second, now: fc.Now, watchClock: fc, log: slog.New(slog.NewTextHandler(&logs, nil)), sleep: func(_ context.Context, d time.Duration) {
		waits = append(waits, d)
		fc.Step(d)
		if calls > 200 {
			cancel()
		}
	}}
	l.run(ctx)
	if calls != 201 || len(waits) != 201 || waits[199] != time.Minute || waits[200] != time.Second {
		t.Errorf("auth retries: calls=%d waits=%v", calls, waits)
	}
	evs := rec.all()
	if len(evs) != 2 || evs[0].Kind != KindNotWatching || evs[1].Kind != KindGap {
		t.Fatalf("auth recovery events %v", rec.kinds())
	}
	if !evs[1].Time.Equal(t0) || !evs[1].ReceivedAt.Equal(fc.Now().Add(-time.Second)) {
		t.Errorf("gap interval %+v", evs[1])
	}
	for _, e := range evs {
		if strings.Contains(string(e.Raw), "SECRET") || strings.Contains(e.Summary, "SECRET") {
			t.Error("event error leaked")
		}
	}
	if strings.Contains(logs.String(), "SECRET") || !strings.Contains(logs.String(), "retries=200") {
		t.Error("unsafe or missing auth recovery log")
	}
}
