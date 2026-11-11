package k8s

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// clientPod is a running pod of namespace ns with address ip, owned by
// the controller kind/name when kind is set.
func clientPod(ns, name, ip, kind, owner string, labels map[string]string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: types.UID("uid-" + name), Labels: labels},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &metav1.Time{Time: t0.Add(-time.Hour)}},
	}
	if ip != "" {
		p.Status.PodIP = ip
		p.Status.PodIPs = []corev1.PodIP{{IP: ip}}
	}
	if kind != "" {
		yes := true
		p.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: owner, Controller: &yes}}
	}
	return p
}

func TestWorkloadOf(t *testing.T) {
	cases := []struct {
		pod        *corev1.Pod
		kind, name string
	}{
		{clientPod("shop", "cart-6d9f-x1", "", "ReplicaSet", "cart-6d9f", map[string]string{"pod-template-hash": "6d9f"}), "Deployment", "cart"},
		{clientPod("shop", "cart-x1", "", "ReplicaSet", "cart-rs", nil), "ReplicaSet", "cart-rs"},
		{clientPod("shop", "ledger-0", "", "StatefulSet", "ledger", nil), "StatefulSet", "ledger"},
		{clientPod("shop", "agent-x", "", "DaemonSet", "agent", nil), "DaemonSet", "agent"},
		{clientPod("shop", "report-29380000-x", "", "Job", "report-29380000", nil), "CronJob", "report"},
		{clientPod("shop", "migrate-x", "", "Job", "migrate", nil), "Job", "migrate"},
		{clientPod("shop", "debug", "", "", "", nil), "Pod", "debug"},
	}
	for _, c := range cases {
		if k, n := workloadOf(c.pod); k != c.kind || n != c.name {
			t.Errorf("%s: %s %s, want %s %s", c.pod.Name, k, n, c.kind, c.name)
		}
	}
}

// batches collects what a ClientPodWatcher writes.
type batches struct {
	mu  sync.Mutex
	all []ClientPodBatch
}

func (b *batches) write(_ context.Context, x ClientPodBatch) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all = append(b.all, x)
	return nil
}

// wait returns the batches once one satisfies ok.
func (b *batches) wait(t *testing.T, what string, ok func(ClientPodBatch) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		for _, x := range b.all {
			if ok(x) {
				b.mu.Unlock()
				return
			}
		}
		b.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no batch with %s: %+v", what, b.all)
}

// The watch lists and watches only Pods, only in the listed namespaces,
// keeps the pods that have an address, and writes what changed: the
// first list, a pod moved to another node, a pod that ended, a pod
// deleted.
func TestClientPodWatcherWithFakeClientset(t *testing.T) {
	cs := fake.NewClientset(
		clientPod("shop", "cart-6d9f-a", "10.4.0.1", "ReplicaSet", "cart-6d9f", map[string]string{"pod-template-hash": "6d9f", "app.kubernetes.io/name": "cart"}),
		clientPod("shop", "report-29380000-b", "10.4.0.2", "Job", "report-29380000", nil),
		clientPod("shop", "pending", "", "", "", nil),
		clientPod("payments", "ledger-0", "10.5.0.1", "StatefulSet", "ledger", nil))
	var mu sync.Mutex
	var actions []string
	watches := make(chan *watch.FakeWatcher, 4)
	cs.PrependReactor("*", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		sel := ""
		if l, ok := a.(k8stesting.ListActionImpl); ok {
			sel = l.ListOptions.LabelSelector
		}
		actions = append(actions, a.GetVerb()+" "+a.GetResource().Resource+" "+a.GetNamespace()+" "+sel)
		return false, nil, nil
	})
	cs.PrependWatchReactor("*", func(a k8stesting.Action) (bool, watch.Interface, error) {
		mu.Lock()
		actions = append(actions, "watch "+a.GetResource().Resource+" "+a.GetNamespace())
		mu.Unlock()
		w := watch.NewFake()
		watches <- w
		return true, w, nil
	})
	got := &batches{}
	c := &clock{t: t0}
	w := NewClientPodWatcher(ClientPodOptions{Client: cs, Namespaces: []string{"shop"}, Write: got.write, Record: (&sink{}).Record,
		Log: quietLog(), Now: c.now, FlushEvery: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	got.wait(t, "the first list", func(b ClientPodBatch) bool {
		if len(b.Relists) != 1 || b.Relists[0].Namespace != "shop" || len(b.Seen) != 2 {
			return false
		}
		cart, report := b.Seen[0], b.Seen[1]
		if cart.Name != "cart-6d9f-a" || cart.WorkloadKind != "Deployment" || cart.Workload != "cart" || cart.AppName != "cart" ||
			cart.IPs[0] != "10.4.0.1" || !cart.StartedAt.Equal(t0.Add(-time.Hour)) || report.WorkloadKind != "CronJob" || report.Workload != "report" {
			t.Fatalf("seen %+v", b.Seen)
		}
		return true
	})
	var fw *watch.FakeWatcher
	select {
	case fw = <-watches:
	case <-time.After(5 * time.Second):
		t.Fatal("no watch")
	}
	moved := clientPod("shop", "cart-6d9f-a", "10.4.0.1", "ReplicaSet", "cart-6d9f", map[string]string{"pod-template-hash": "6d9f", "app.kubernetes.io/name": "cart"})
	moved.Spec.NodeName = "node-2"
	fw.Modify(moved)
	got.wait(t, "the move", func(b ClientPodBatch) bool { return len(b.Seen) == 1 && b.Seen[0].Node == "node-2" })
	// A status change that keeps what is stored writes nothing.
	got.mu.Lock()
	n := len(got.all)
	got.mu.Unlock()
	same := moved.DeepCopy()
	same.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	fw.Modify(same)
	done2 := clientPod("shop", "report-29380000-b", "10.4.0.2", "Job", "report-29380000", nil)
	done2.Status.Phase = corev1.PodSucceeded
	fw.Modify(done2)
	got.wait(t, "the ended job", func(b ClientPodBatch) bool { _, ok := b.Ended["uid-report-29380000-b"]; return ok })
	got.mu.Lock()
	for _, b := range got.all[n:] {
		if len(b.Seen) > 0 {
			t.Errorf("a status change was written: %+v", b.Seen)
		}
	}
	got.mu.Unlock()
	fw.Delete(moved)
	got.wait(t, "the deleted pod", func(b ClientPodBatch) bool { _, ok := b.Ended["uid-cart-6d9f-a"]; return ok })

	mu.Lock()
	defer mu.Unlock()
	for _, a := range actions {
		if !strings.HasPrefix(a, "list pods shop ") && a != "watch pods shop" {
			t.Errorf("unexpected request %q", a)
		}
		if strings.HasPrefix(a, "list") && a != "list pods shop " {
			t.Errorf("listed with a selector: %q", a)
		}
	}
}

// A watch without permission logs one explanation, records that the pods
// are not watched once it failed for gap_after, tries again with backoff,
// writes nothing, and stops when asked.
func TestClientPodWatcherForbidden(t *testing.T) {
	cs := fake.NewClientset()
	lists := 0
	var mu sync.Mutex
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		lists++
		mu.Unlock()
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
	})
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&syncWriter{w: &logs}, nil))
	rec := &sink{}
	got := &batches{}
	c := &clock{t: t0}
	w := NewClientPodWatcher(ClientPodOptions{Client: cs, Namespaces: []string{"shop"}, Write: got.write, Record: rec.Record,
		GapAfter: 30 * time.Second, Log: log, Now: c.now, FlushEvery: 5 * time.Millisecond})
	var waits []time.Duration
	var wmu sync.Mutex
	w.sleep = func(ctx context.Context, d time.Duration) {
		wmu.Lock()
		waits = append(waits, d)
		wmu.Unlock()
		c.add(d)
		select {
		case <-ctx.Done():
		case <-time.After(time.Millisecond):
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := find(rec.all(), KindNotWatching); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	e, ok := find(rec.all(), KindNotWatching)
	if !ok {
		t.Fatalf("no %s: %v", KindNotWatching, rec.kinds())
	}
	if raw := rawOf(t, e); raw["source"] != "client pods in shop" || raw["error"] != "Kubernetes API returned HTTP 403" {
		t.Fatalf("raw %v", raw)
	}
	if n := strings.Count(logs.String(), "may not watch client pods in shop"); n != 1 {
		t.Fatalf("explained %d times:\n%s", n, logs.String())
	}
	wmu.Lock()
	if len(waits) < 3 || waits[0] != time.Second || waits[1] != 2*time.Second || waits[2] != 4*time.Second {
		t.Fatalf("waits %v", waits)
	}
	wmu.Unlock()
	got.mu.Lock()
	defer got.mu.Unlock()
	for _, b := range got.all {
		if len(b.Seen) > 0 || len(b.Relists) > 0 || len(b.Ended) > 0 {
			t.Fatalf("written without a list: %+v", b)
		}
	}
}

// syncWriter serialises writes to w.
type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// flushed runs one flush of w and returns the batch written.
func flushed(t *testing.T, w *ClientPodWatcher, touch bool) ClientPodBatch {
	t.Helper()
	got := &batches{}
	w.o.Write = got.write
	w.flush(context.Background(), touch)
	if len(got.all) == 0 {
		return ClientPodBatch{}
	}
	return got.all[0]
}

// A truncated list ends no pod; the touch names only the namespaces
// whose watch is healthy; a pod that lived less than a flush is written
// with its end.
func TestClientPodWatcherApply(t *testing.T) {
	c := &clock{t: t0}
	w := NewClientPodWatcher(ClientPodOptions{Log: quietLog(), Now: c.now, Record: (&sink{}).Record})
	w.healthy = map[string]*atomic.Bool{"shop": {}, "payments": {}}
	w.healthy["shop"].Store(true)
	src := &source{kind: "clientpods", ns: "shop"}
	a, b := clientPod("shop", "a", "10.4.0.1", "", "", nil), clientPod("shop", "b", "10.4.0.2", "", "", nil)
	w.apply(update{src: src, isList: true, listed: []runtime.Object{a, b}})
	if x := flushed(t, w, true); len(x.Seen) != 2 || len(x.Relists) != 1 || !slices.Equal(x.Touch, []string{"shop"}) {
		t.Fatalf("first list: %+v", x)
	}
	w.apply(update{src: src, isList: true, truncated: true, listed: []runtime.Object{a}})
	if x := flushed(t, w, false); len(x.Relists) != 0 || w.live["uid-b"] == nil {
		t.Fatalf("truncated list: %+v, b kept %v", x, w.live["uid-b"] != nil)
	}
	short := clientPod("shop", "job-x", "10.4.0.3", "Job", "job", nil)
	w.apply(update{src: src, ev: watch.Event{Type: watch.Added, Object: short}})
	w.apply(update{src: src, ev: watch.Event{Type: watch.Deleted, Object: short}})
	x := flushed(t, w, false)
	if len(x.Seen) != 1 || x.Seen[0].Name != "job-x" {
		t.Fatalf("short pod not written: %+v", x)
	}
	if _, ok := x.Ended["uid-job-x"]; !ok {
		t.Fatalf("short pod not ended: %+v", x)
	}
}

// A listed pod keeps only what is matched on.
func TestSlimPod(t *testing.T) {
	p := clientPod("shop", "cart-6d9f-a", "10.4.0.1", "ReplicaSet", "cart-6d9f", map[string]string{"pod-template-hash": "6d9f", "app.kubernetes.io/name": "cart", "team": "x"})
	p.Spec.Containers = []corev1.Container{{Name: "app", Image: "example.com/cart"}}
	s := slimPod(p)
	if len(s.Spec.Containers) != 0 || len(s.Labels) != 2 || clientPodOfEqual(clientPodOf(s), clientPodOf(p)) == false || ended(s) != ended(p) {
		t.Fatalf("slim %+v", s)
	}
}

func clientPodOfEqual(a, b ClientPod) bool {
	return a.UID == b.UID && a.Name == b.Name && a.Namespace == b.Namespace && a.Node == b.Node && a.WorkloadKind == b.WorkloadKind &&
		a.Workload == b.Workload && a.AppName == b.AppName && slices.Equal(a.IPs, b.IPs) && a.StartedAt.Equal(b.StartedAt) && a.HostNetwork == b.HostNetwork
}

// Each namespace is refreshed on its own: one unhealthy at a refresh is
// refreshed at its next healthy flush, not a whole TouchEvery later.
func TestClientPodTouchPerNamespace(t *testing.T) {
	c := &clock{t: t0}
	w := NewClientPodWatcher(ClientPodOptions{Client: fake.NewClientset(), Log: quietLog(), Now: c.now, Record: (&sink{}).Record})
	w.healthy = map[string]*atomic.Bool{}
	w.loopFor("shop", nil)
	w.loopFor("payments", nil)
	w.healthy["shop"].Store(true)
	c.add(DefaultClientPodTouchEvery - time.Second)
	if x := flushed(t, w, true); len(x.Touch) != 0 {
		t.Fatalf("touched before TouchEvery: %v", x.Touch)
	}
	c.add(time.Second)
	if x := flushed(t, w, true); !slices.Equal(x.Touch, []string{"shop"}) {
		t.Fatalf("at TouchEvery with payments failing: %v", x.Touch)
	}
	w.healthy["payments"].Store(true)
	c.add(DefaultClientPodFlushEvery)
	if x := flushed(t, w, true); !slices.Equal(x.Touch, []string{"payments"}) {
		t.Fatalf("next flush once payments is healthy: %v", x.Touch)
	}
}
