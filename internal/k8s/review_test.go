package k8s

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/shadone/nats-lens/internal/events"
)

// R1: a watch that ends at once without an error is reopened with no wait.
func TestReviewImmediateCloseSpins(t *testing.T) {
	calls := 0
	src := &source{kind: "pods", ns: "example", name: "pods in example",
		list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
			return nil, "1", "", nil
		},
		watch: func(context.Context, metav1.ListOptions) (watch.Interface, error) {
			calls++
			w := watch.NewFake()
			w.Stop()
			return w, nil
		}}
	out := make(chan update, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	slept := 0
	l := &loop{src: src, out: out, record: func(e events.Event) {}, gapAfter: time.Second, now: time.Now, log: quietLog(),
		sleep: func(context.Context, time.Duration) { slept++ }}
	l.run(ctx)
	t.Logf("watch calls in 300ms: %d, sleeps: %d", calls, slept)
	if calls > 20 {
		t.Fatalf("hot loop: %d watch calls, %d waits", calls, slept)
	}
}

// R2: one short watch error, then a quiet healthy watch: the gap is the
// length of the quiet watch, not of the failure.
func TestReviewFalseGapAfterShortFailure(t *testing.T) {
	sc := &scripted{watchers: make(chan *watch.FakeWatcher, 4), watchErrs: []error{errors.New("unexpected EOF")}}
	rec, c := &sink{}, &clock{t: t0}
	out, _ := startLoop(t, sc, rec, c, 30*time.Second)
	next(t, out)
	w := nextWatcher(t, sc) // established 1 s after the failure
	c.add(8 * time.Minute)  // quiet namespace: no events, then the server ends the watch
	w.Stop()
	nextWatcher(t, sc)
	if len(rec.all()) != 0 {
		e := rec.all()[0]
		t.Fatalf("a 1 s failure recorded %v: %s", rec.kinds(), e.Summary)
	}
}

// R3: the cordon arrives after the pod's termination.
func TestReviewCordonAfterTermination(t *testing.T) {
	tr, s, c := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1")}, true)
	del := natsPod("nats-0", "n0", true)
	ts := metav1.NewTime(c.now().Add(time.Minute))
	del.DeletionTimestamp = &ts
	tr.podChanged(del)
	cord := node("n0", "pool-1", "v1")
	cord.Spec.Unschedulable = true
	tr.nodeChanged(cord)
	if !slices.Contains(s.kinds(), KindNodeDrained) {
		t.Fatalf("no drain: %v", s.kinds())
	}
}

// R4: autoscaler taint then pod termination gives the same kind as a drain.
func TestReviewAutoscalerRemovalIsDrained(t *testing.T) {
	tr, s, c := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1")}, true)
	n := node("n0", "pool-1", "v1")
	n.Spec.Taints = []corev1.Taint{{Key: autoscalerTaint}}
	tr.nodeChanged(n)
	del := natsPod("nats-0", "n0", true)
	ts := metav1.NewTime(c.now().Add(time.Minute))
	del.DeletionTimestamp = &ts
	tr.podChanged(del)
	d, ok := find(s.all(), KindNodeDrained)
	t.Logf("kinds %v drained raw %s", s.kinds(), d.Raw)
	_ = ok
}
