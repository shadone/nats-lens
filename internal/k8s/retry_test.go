package k8s

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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
		t.Run(map[bool]string{false: "resume", true: "relist"}[relist], func(t *testing.T) {
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
			if relist && lists < 2 {
				t.Error("no relists")
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
