package k8s

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// scripted is a source whose lists and watches follow a script.
type scripted struct {
	mu      sync.Mutex
	lists   int
	listRVs []string
	// watchErrs are returned by the next watch calls, nil for a watch.
	watchErrs []error
	watchRVs  []string
	watchers  chan *watch.FakeWatcher
}

func (s *scripted) source() *source {
	return &source{kind: "pods", ns: "example", name: "pods in example",
		list: func(ctx context.Context, opts metav1.ListOptions) ([]runtime.Object, string, string, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.lists++
			return []runtime.Object{natsPod("nats-0", "n0", true)}, "100", "", nil
		},
		watch: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			s.mu.Lock()
			s.watchRVs = append(s.watchRVs, opts.ResourceVersion)
			var err error
			if len(s.watchErrs) > 0 {
				err, s.watchErrs = s.watchErrs[0], s.watchErrs[1:]
			}
			s.mu.Unlock()
			if err != nil {
				return nil, err
			}
			w := watch.NewFake()
			s.watchers <- w
			return w, nil
		},
	}
}

func (s *scripted) state() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lists, append([]string(nil), s.watchRVs...)
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func startLoop(t *testing.T, sc *scripted, rec *sink, c *clock, gapAfter time.Duration) (chan update, context.CancelFunc) {
	t.Helper()
	out := make(chan update, 64)
	ctx, cancel := context.WithCancel(context.Background())
	l := &loop{src: sc.source(), out: out, record: rec.Record, gapAfter: gapAfter, now: c.now, log: quietLog(),
		sleep: func(_ context.Context, d time.Duration) { c.add(d) }}
	done := make(chan struct{})
	go func() { l.run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return out, cancel
}

func next(t *testing.T, out chan update) update {
	t.Helper()
	select {
	case u := <-out:
		return u
	case <-time.After(5 * time.Second):
		t.Fatal("no update")
	}
	return update{}
}

func nextWatcher(t *testing.T, sc *scripted) *watch.FakeWatcher {
	t.Helper()
	select {
	case w := <-sc.watchers:
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("no watch")
	}
	return nil
}

func TestLoopResumesFromLastResourceVersion(t *testing.T) {
	sc := &scripted{watchers: make(chan *watch.FakeWatcher, 4)}
	rec, c := &sink{}, &clock{t: t0}
	out, _ := startLoop(t, sc, rec, c, 30*time.Second)
	if u := next(t, out); !u.isList || !u.initial || len(u.listed) != 1 {
		t.Fatalf("first update %+v", u)
	}
	w := nextWatcher(t, sc)
	p := natsPod("nats-0", "n0", false)
	p.ResourceVersion = "105"
	w.Modify(p)
	if u := next(t, out); u.ev.Type != watch.Modified {
		t.Fatalf("update %+v", u)
	}
	bm := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "110"}}
	w.Action(watch.Bookmark, bm)
	// The server ends the watch; the next one resumes from the bookmark,
	// without a list or a gap.
	w.Stop()
	nextWatcher(t, sc)
	lists, rvs := sc.state()
	if lists != 1 || len(rvs) != 2 || rvs[0] != "100" || rvs[1] != "110" {
		t.Fatalf("lists %d, watch resource versions %v", lists, rvs)
	}
	if len(rec.all()) != 0 {
		t.Fatalf("recorded %v", rec.kinds())
	}
}

func TestLoopRelistsWhenGone(t *testing.T) {
	sc := &scripted{watchers: make(chan *watch.FakeWatcher, 4)}
	rec, c := &sink{}, &clock{t: t0}
	out, _ := startLoop(t, sc, rec, c, 30*time.Second)
	next(t, out)
	w := nextWatcher(t, sc)
	w.Error(&apierrors.NewResourceExpired("too old").ErrStatus)
	u := next(t, out)
	if !u.isList || u.initial {
		t.Fatalf("after 410 want a relist that is not the first, got %+v", u)
	}
	nextWatcher(t, sc)
	if lists, _ := sc.state(); lists != 2 {
		t.Fatalf("lists %d", lists)
	}
	if len(rec.all()) != 0 {
		t.Fatalf("an expired watch is not a gap: %v", rec.kinds())
	}
}

func TestLoopRecordsAGap(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("rbac"))
	sc := &scripted{watchers: make(chan *watch.FakeWatcher, 4),
		watchErrs: []error{forbidden, forbidden, forbidden, forbidden, forbidden, forbidden}}
	rec, c := &sink{}, &clock{t: t0}
	out, _ := startLoop(t, sc, rec, c, 10*time.Second)
	next(t, out)
	// Six failures wait 1+2+4+8+16+32 s of fake time; the gap is recorded as not
	// watching once it lasted 10 s, then as a gap once a watch works.
	w := nextWatcher(t, sc)
	p := natsPod("nats-0", "n0", true)
	p.ResourceVersion = "120"
	w.Modify(p)
	next(t, out)
	evs := rec.all()
	if len(evs) != 2 || evs[0].Kind != KindNotWatching || evs[1].Kind != KindGap {
		t.Fatalf("recorded %v", rec.kinds())
	}
	gap := rawOf(t, evs[1])
	if evs[1].Source != "collector" || !evs[1].Time.Equal(t0) || gap["seconds"].(float64) != 63 || gap["source"] != "pods in example" {
		t.Fatalf("gap %+v raw %v", evs[1], gap)
	}
	if nw := rawOf(t, evs[0]); nw["status"].(float64) != 403 {
		t.Fatalf("not watching raw %v", nw)
	}
	// A failure shorter than gap_after records nothing.
	rec.reset()
	sc.mu.Lock()
	sc.watchErrs = []error{forbidden}
	sc.mu.Unlock()
	w.Stop()
	w2 := nextWatcher(t, sc)
	w2.Modify(p)
	next(t, out)
	if len(rec.all()) != 0 {
		t.Fatalf("short failure recorded %v", rec.kinds())
	}
}
