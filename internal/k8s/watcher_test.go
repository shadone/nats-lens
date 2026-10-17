package k8s

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestWatcherWithFakeClientset(t *testing.T) {
	cs := fake.NewClientset(natsPod("nats-0", "n0", true), natsPod("nats-1", "n1", true), node("n0", "pool-1", "v1.33.4"),
		node("n1", "pool-1", "v1.33.4"),
		kevent("old", "example", "Pod", "nats-1", "Unhealthy", "Warning", "readiness probe failed", 1, t0.Add(-10*time.Minute)),
		kevent("seen", "example", "Pod", "nats-1", "Killing", "Normal", "Stopping container nats", 1, t0.Add(-20*time.Minute)))
	watches := map[string]chan *watch.FakeWatcher{}
	var listSelectors sync.Map
	for _, r := range []string{"pods", "nodes", "events"} {
		ch := make(chan *watch.FakeWatcher, 8)
		watches[r] = ch
		cs.PrependWatchReactor(r, func(a k8stesting.Action) (bool, watch.Interface, error) {
			w := watch.NewFake()
			ch <- w
			return true, w, nil
		})
		cs.PrependReactor("list", r, func(a k8stesting.Action) (bool, runtime.Object, error) {
			l := a.(k8stesting.ListActionImpl)
			listSelectors.Store(r+" "+l.Namespace, l.ListOptions.LabelSelector+"|"+l.ListOptions.FieldSelector)
			return false, nil, nil
		})
	}
	rec := &sink{}
	c := &clock{t: t0}
	w := New(Options{Client: cs, Namespaces: []string{"example"}, NATSSelector: "app.kubernetes.io/name=nats", Nodes: true,
		Sink: rec, Seen: map[string]int64{"seen": 1}, Log: quietLog(), Now: c.now})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	get := func(r string) *watch.FakeWatcher {
		t.Helper()
		select {
		case fw := <-watches[r]:
			return fw
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s watch", r)
		}
		return nil
	}
	pods, nodes := get("pods"), get("nodes")
	// One Event watch in example, one for node Events in default; which
	// comes first is not fixed, and the pod Events are sent on both.
	evA, evB := get("events"), get("events")
	waitFor := func(kinds ...string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if got := rec.kinds(); len(got) >= len(kinds) {
				if !slices.Equal(got, kinds) {
					t.Fatalf("kinds %v, want %v", got, kinds)
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("kinds %v, want %v", rec.kinds(), kinds)
	}
	// The listed Unhealthy Event of a NATS pod is stored; the one already
	// in the store is not.
	waitFor(KindPodUnhealthy)
	if e := rec.all()[0]; e.ServerName != "nats-1" {
		t.Fatalf("listed event %+v", e)
	}
	rec.reset()

	n := node("n0", "pool-1", "v1.33.4")
	n.Spec.Unschedulable = true
	nodes.Modify(n)
	waitFor(KindNodeCordoned)
	p := natsPod("nats-0", "n0", true)
	p.Status.Conditions = append(p.Status.Conditions, corev1.PodCondition{Type: corev1.DisruptionTarget, Status: corev1.ConditionTrue,
		Reason: "EvictionByEvictionAPI"})
	pods.Modify(p)
	waitFor(KindNodeCordoned, KindPodEvicted, KindNodeDrained)
	k1 := kevent("k1", "example", "Pod", "nats-0", "Killing", "Normal", "Stopping container nats", 1, t0)
	evA.Add(k1)
	evB.Add(k1)
	waitFor(KindNodeCordoned, KindPodEvicted, KindNodeDrained, KindPodKilling)
	pods.Delete(p)
	waitFor(KindNodeCordoned, KindPodEvicted, KindNodeDrained, KindPodKilling, KindPodDeleted)
	nodes.Delete(n)
	waitFor(KindNodeCordoned, KindPodEvicted, KindNodeDrained, KindPodKilling, KindPodDeleted, KindNodeDeleted)
	for _, e := range rec.all() {
		if e.Kind == KindPodEvicted || e.Kind == KindPodKilling || e.Kind == KindPodDeleted {
			if e.ServerName != "nats-0" {
				t.Fatalf("%s server %q", e.Kind, e.ServerName)
			}
		}
	}

	// The pods are listed with the selector, node Events with a field
	// selector in the node namespace.
	if v, _ := listSelectors.Load("pods example"); v != "app.kubernetes.io/name=nats|" {
		t.Fatalf("pod list selectors %v", v)
	}
	if v, _ := listSelectors.Load("events default"); v != "|regarding.kind=Node" {
		t.Fatalf("node event list selectors %v", v)
	}
}
