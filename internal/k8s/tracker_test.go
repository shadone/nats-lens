package k8s

import (
	"context"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/shadone/nats-lens/internal/events"
)

func TestInitialListsRecordNothing(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true), natsPod("nats-1", "n1", false)}, true, false)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1.33.4"), node("n1", "pool-1", "v1.33.4")}, true, false)
	if got := s.all(); len(got) != 0 {
		t.Fatalf("initial lists recorded %v", s.kinds())
	}
	if !tr.natsNode(tr.nodes["n0"]) || tr.pools["pool-1"].IsZero() {
		t.Fatalf("n0 is not a NATS node, pools %v", tr.pools)
	}
}

func TestPodReadinessAndServerName(t *testing.T) {
	s := &sink{}
	c := &clock{t: t0}
	tr := newTracker(serverNameFunc("{namespace}-{pod}"), c.now, s.Record, nil)
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
	tr.podChanged(natsPod("nats-0", "n0", false))
	tr.podChanged(natsPod("nats-0", "n0", true))
	evs := s.all()
	if got := s.kinds(); !slices.Equal(got, []string{KindPodNotReady, KindPodReady}) {
		t.Fatalf("kinds %v", got)
	}
	for _, e := range evs {
		if e.Source != events.SourceK8s || e.ServerName != "example-nats-0" {
			t.Fatalf("event %+v: want source k8s, server example-nats-0", e)
		}
	}
	if !evs[0].Time.Equal(t0) {
		t.Fatalf("not ready time %v, want the condition's transition %v", evs[0].Time, t0)
	}
}

func TestPodRestartedScheduledDeleted(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-1", "n1", true)}, true, false)
	p := natsPod("nats-1", "n1", true)
	p.Status.ContainerStatuses[0].RestartCount = 1
	p.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137,
		FinishedAt: metav1.NewTime(t0)}
	tr.podChanged(p)
	e, ok := find(s.all(), KindPodRestarted)
	if !ok || e.Reason != "OOMKilled" || e.ServerName != "nats-1" {
		t.Fatalf("restart %+v (%v)", e, s.kinds())
	}
	s.reset()
	tr.podDeleted(p)
	// The StatefulSet recreates the pod: unscheduled first, then on a node.
	n := natsPod("nats-1", "", false)
	n.UID = types.UID("uid-nats-1-b")
	tr.podChanged(n)
	n2 := n.DeepCopy()
	n2.Spec.NodeName = "n2"
	tr.podChanged(n2)
	if got := s.kinds(); !slices.Equal(got, []string{KindPodDeleted, KindPodScheduled}) {
		t.Fatalf("kinds %v", got)
	}
	if !tr.natsNode(tr.nodes["n2"]) {
		t.Fatal("the new node is not a NATS node")
	}
}

func TestDrainDerivedFromCordonAndEviction(t *testing.T) {
	tr, s, c := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1.33.4"), node("x9", "pool-other", "v1.33.4")}, true, false)

	cordoned := node("n0", "pool-1", "v1.33.4")
	cordoned.Spec.Unschedulable = true
	tr.nodeChanged(cordoned)
	// A node without NATS pods in a pool without NATS: not recorded.
	other := node("x9", "pool-other", "v1.33.4")
	other.Spec.Unschedulable = true
	tr.nodeChanged(other)

	c.add(time.Minute)
	p := natsPod("nats-0", "n0", true)
	p.Status.Conditions = append(p.Status.Conditions, corev1.PodCondition{Type: corev1.DisruptionTarget, Status: corev1.ConditionTrue,
		Reason: "EvictionByEvictionAPI", Message: "Eviction API: evicting"})
	tr.podChanged(p)
	del := p.DeepCopy()
	ts := metav1.NewTime(c.now().Add(60 * time.Second))
	del.DeletionTimestamp = &ts
	tr.podChanged(del)
	// The kubelet's Killing Event and an Evicted Event follow; Killing is
	// kept, the eviction is not recorded twice, the drain only once.
	tr.eventObserved(kevent("e1", "example", "Pod", "nats-0", "Killing", "Normal", "Stopping container nats", 1, c.now()))
	tr.eventObserved(kevent("e2", "example", "Pod", "nats-0", "Evicted", "Warning", "evicted", 1, c.now()))

	want := []string{KindNodeCordoned, KindPodEvicted, KindNodeDrained, KindPodKilling}
	if got := s.kinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	d, _ := find(s.all(), KindNodeDrained)
	raw := rawOf(t, d)
	if raw["node"] != "n0" || raw["pool"] != "pool-1" || d.ServerName != "" {
		t.Fatalf("drained %+v raw %v", d, raw)
	}
	ev, _ := find(s.all(), KindPodEvicted)
	if ev.Reason != "EvictionByEvictionAPI" || ev.ServerName != "nats-0" {
		t.Fatalf("evicted %+v", ev)
	}

	// Uncordon ends the cordon; the next cordon and termination drain again.
	s.reset()
	tr.nodeChanged(node("n0", "pool-1", "v1.33.4"))
	tr.podDeleted(del)
	tr.podChanged(natsPod("nats-0", "n0", false))
	tr.nodeChanged(cordoned)
	again := natsPod("nats-0", "n0", false)
	ts2 := metav1.NewTime(c.now())
	again.DeletionTimestamp = &ts2
	tr.podChanged(again)
	want = []string{KindNodeUncordoned, KindPodDeleted, KindPodScheduled, KindNodeCordoned, KindPodTerminating, KindNodeDrained}
	if got := s.kinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
}

func TestAutoscalerTaintIsACordon(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-2", "n2", true)}, true, false)
	tr.nodesListed([]*corev1.Node{node("n2", "pool-1", "v1")}, true, false)
	n := node("n2", "pool-1", "v1")
	n.Spec.Taints = []corev1.Taint{{Key: autoscalerTaint, Effect: corev1.TaintEffectNoSchedule}}
	tr.nodeChanged(n)
	e, ok := find(s.all(), KindNodeCordoned)
	if !ok || e.Reason != "autoscaler removal" {
		t.Fatalf("cordon %+v (%v)", e, s.kinds())
	}
}

func TestNodeChangesInANATSPool(t *testing.T) {
	tr, s, c := newTestTracker()
	tr.poolNodes = true
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1.33.4")}, true, false)
	// Surge: a node is added to the pool, becomes ready later.
	added := node("n3", "pool-1", "v1.33.5")
	added.Status.Conditions[0].Status = corev1.ConditionFalse
	tr.nodeChanged(added)
	tr.nodeChanged(node("n3", "pool-1", "v1.33.5"))
	// In-place kubelet upgrade, not ready, deleted.
	tr.nodeChanged(node("n0", "pool-1", "v1.33.5"))
	nr := node("n0", "pool-1", "v1.33.5")
	nr.Status.Conditions[0].Status = corev1.ConditionUnknown
	tr.nodeChanged(nr)
	tr.podDeleted(natsPod("nats-0", "n0", true))
	c.add(10 * time.Minute)
	tr.nodesListed([]*corev1.Node{node("n3", "pool-1", "v1.33.5")}, false, false)
	want := []string{KindNodeAdded, KindNodeReady, KindNodeUpgrade, KindNodeNotReady, KindPodDeleted, KindNodeDeleted}
	if got := s.kinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	up, _ := find(s.all(), KindNodeUpgrade)
	if up.Summary != "node n0 kubelet v1.33.4 -> v1.33.5" {
		t.Fatalf("upgrade summary %q", up.Summary)
	}
	// Outside the pool's memory nothing of the pool is kept.
	s.reset()
	c.add(natsPoolFor)
	tr.expire()
	tr.nodeChanged(node("n4", "pool-1", "v1.33.5"))
	if len(s.all()) != 0 {
		t.Fatalf("recorded %v for a pool without NATS for a day", s.kinds())
	}
}

func TestRelistRecordsWhatChanged(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true), natsPod("nats-1", "n1", true)}, true, false)
	tr.podsListed("other", []*corev1.Pod{}, true, false)
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", false)}, false, false)
	want := []string{KindPodNotReady, KindPodDeleted}
	if got := s.kinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	// A relist of another namespace leaves the example pods alone.
	s.reset()
	tr.podsListed("other", nil, false, false)
	if len(s.all()) != 0 {
		t.Fatalf("other namespace relist recorded %v", s.kinds())
	}
}

// A truncated relist deletes no pod it left out.
func TestTruncatedRelistKeepsPods(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true), natsPod("nats-1", "n1", true)}, true, false)
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, false, true)
	if len(s.all()) != 0 || tr.pods["example/nats-1"] == nil {
		t.Fatalf("truncated relist recorded %v", s.kinds())
	}
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1"), node("n1", "pool-1", "v1")}, true, false)
	s.reset()
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1")}, false, true)
	if len(s.all()) != 0 || tr.nodes["n1"] == nil {
		t.Fatalf("truncated node relist recorded %v", s.kinds())
	}
}

// A truncated first list records no pod or node deleted while natslens
// was down.
func TestTruncatedFirstListsRecordNoDeletion(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.prior = Prior{Pods: map[string]PriorState{"example/nats-2": {Flag: flag(true)}}, Nodes: map[string]PriorState{"n2": {Flag: flag(false)}}}
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, true)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1")}, true, true)
	if len(s.all()) != 0 {
		t.Fatalf("truncated first lists recorded %v", s.kinds())
	}
}

// listAll stops at the loop's page limit and says the list was cut; the
// client pod watch lists up to 100 pages.
func TestListAllTruncated(t *testing.T) {
	pages := 0
	src := &source{name: "pods in shop", list: func(context.Context, metav1.ListOptions) ([]runtime.Object, string, string, error) {
		pages++
		return []runtime.Object{&corev1.Pod{}}, "1", "more", nil
	}}
	for _, c := range []struct{ maxPages, want int }{{0, maxListPages}, {maxClientPods / listPage, 100}} {
		pages = 0
		l := &loop{src: src, log: quietLog(), maxPages: c.maxPages}
		items, rv, truncated, err := l.listAll(context.Background())
		if err != nil || !truncated || rv != "1" || pages != c.want || len(items) != c.want {
			t.Fatalf("max %d: %d pages, %d items, truncated %v, err %v", c.maxPages, pages, len(items), truncated, err)
		}
	}
	w := NewClientPodWatcher(ClientPodOptions{Client: fake.NewClientset(), Namespaces: []string{"shop"}})
	if got := w.loopFor("shop", nil).maxPages; got != 100 {
		t.Fatalf("client pod watch lists %d pages, want 100", got)
	}
}

func TestEventsClassifiedAndDeduplicated(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1")}, true, false)
	at := t0.Add(time.Minute)
	steps := []struct {
		name  string
		e     func()
		kinds []string
		srv   string
	}{
		{"nats killing", func() {
			tr.eventObserved(kevent("a", "example", "Pod", "nats-0", "Killing", "Normal", "Stopping container nats", 1, at))
		}, []string{KindPodKilling}, "nats-0"},
		{"same count again", func() {
			tr.eventObserved(kevent("a", "example", "Pod", "nats-0", "Killing", "Normal", "Stopping container nats", 1, at))
		}, nil, ""},
		{"count grows", func() {
			tr.eventObserved(kevent("a", "example", "Pod", "nats-0", "Killing", "Normal", "Stopping container nats", 2, at))
		}, []string{KindPodKilling}, "nats-0"},
		{"reloader container", func() {
			e := kevent("b", "example", "Pod", "nats-0", "Killing", "Normal", "Stopping container reloader", 1, at)
			e.Regarding.FieldPath = "spec.containers{reloader}"
			tr.eventObserved(e)
		}, nil, ""},
		{"nats container started", func() {
			e := kevent("c", "example", "Pod", "nats-0", "Started", "Normal", "Started container nats", 1, at)
			e.Regarding.FieldPath = "spec.containers{nats}"
			tr.eventObserved(e)
		}, []string{KindPodStarted}, "nats-0"},
		{"pulled is routine", func() {
			tr.eventObserved(kevent("d", "example", "Pod", "nats-0", "Pulled", "Normal", "image present", 1, at))
		}, nil, ""},
		{"nats unknown reason", func() {
			tr.eventObserved(kevent("e", "example", "Pod", "nats-0", "SuccessfulAttachVolume", "Normal", "attached", 1, at))
		}, []string{KindEvent}, "nats-0"},
		{"client started is not kept", func() {
			tr.eventObserved(kevent("f", "example", "Pod", "media-worker-1", "Started", "Normal", "Started", 1, at))
		}, nil, ""},
		{"client killing is kept", func() {
			tr.eventObserved(kevent("g", "example", "Pod", "media-worker-1", "Killing", "Normal", "Stopping", 1, at))
		}, []string{KindPodKilling}, ""},
		{"client warning", func() {
			tr.eventObserved(kevent("h", "example", "Pod", "media-worker-1", "Unhealthy", "Warning", "Readiness probe failed", 1, at))
		}, []string{KindPodUnhealthy}, ""},
		{"statefulset warning", func() {
			tr.eventObserved(kevent("i", "example", "StatefulSet", "nats", "FailedCreate", "Warning", "quota", 1, at))
		}, []string{KindEvent}, ""},
		{"statefulset normal", func() {
			tr.eventObserved(kevent("j", "example", "StatefulSet", "nats", "SuccessfulCreate", "Normal", "create", 1, at))
		}, nil, ""},
		{"node upgrade event", func() {
			tr.eventObserved(kevent("k", "default", "Node", "n0", "NodeUpgrade", "Normal", "upgrading to 1.34", 1, at))
		}, []string{KindNodeUpgrade}, ""},
		{"upgrade of a node without NATS", func() {
			tr.eventObserved(kevent("k2", "default", "Node", "n7", "NodeUpgrade", "Normal", "upgrading to 1.34", 1, at))
		}, nil, ""},
		{"routine node event", func() {
			tr.eventObserved(kevent("l", "default", "Node", "n0", "NodeNotSchedulable", "Normal", "cordoned", 1, at))
		}, nil, ""},
		{"nats node warning", func() {
			tr.eventObserved(kevent("m", "default", "Node", "n0", "FrequentKubeletRestart", "Warning", "restarts", 1, at))
		}, []string{KindEvent}, ""},
	}
	for _, st := range steps {
		s.reset()
		st.e()
		got := s.all()
		if !slices.Equal(s.kinds(), st.kinds) {
			t.Fatalf("%s: kinds %v, want %v", st.name, s.kinds(), st.kinds)
		}
		for _, e := range got {
			if e.ServerName != st.srv || e.Source != events.SourceK8s || !e.Time.Equal(at) {
				t.Fatalf("%s: event %+v", st.name, e)
			}
			raw := rawOf(t, e)
			if raw["uid"] == "" || raw["count"] == nil {
				t.Fatalf("%s: raw %v lacks uid and count", st.name, raw)
			}
		}
	}
}

func TestSeenEventsAreNotStoredAgain(t *testing.T) {
	s := &sink{}
	c := &clock{t: t0}
	tr := newTracker(serverNameFunc("{pod}"), c.now, s.Record, map[string]int64{"a": 2})
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
	tr.eventObserved(kevent("a", "example", "Pod", "nats-0", "Killing", "Normal", "x", 2, t0))
	if len(s.all()) != 0 {
		t.Fatalf("seeded event stored again: %v", s.kinds())
	}
	tr.eventObserved(kevent("a", "example", "Pod", "nats-0", "Killing", "Normal", "x", 3, t0))
	if len(s.all()) != 1 {
		t.Fatalf("a new occurrence was not stored: %v", s.kinds())
	}
	// Events of a deleted NATS pod still carry its server for natsPodFor.
	s.reset()
	tr.podDeleted(natsPod("nats-0", "n0", true))
	tr.eventObserved(kevent("z", "example", "Pod", "nats-0", "Killing", "Normal", "x", 1, t0))
	if e, ok := find(s.all(), KindPodKilling); !ok || e.ServerName != "nats-0" {
		t.Fatalf("killing after delete %+v", e)
	}
}

func TestDrainRecordsTheCordonCause(t *testing.T) {
	for _, tc := range []struct {
		name, cause, summary string
		edit                 func(*corev1.Node)
	}{
		{"cordon", "unschedulable", "node n0 cordoned and drained: nats-0 leaves it", func(n *corev1.Node) { n.Spec.Unschedulable = true }},
		{"autoscaler", "autoscaler", "node n0 removed by the autoscaler: nats-0 leaves it", func(n *corev1.Node) {
			n.Spec.Taints = []corev1.Taint{{Key: autoscalerTaint}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, s, c := newTestTracker()
			tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
			tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1")}, true, false)
			n := node("n0", "pool-1", "v1")
			tc.edit(n)
			tr.nodeChanged(n)
			del := natsPod("nats-0", "n0", true)
			ts := metav1.NewTime(c.now().Add(time.Minute))
			del.DeletionTimestamp = &ts
			tr.podChanged(del)
			d, ok := find(s.all(), KindNodeDrained)
			if !ok || rawOf(t, d)["cause"] != tc.cause || d.Summary != tc.summary {
				t.Fatalf("drained %+v (%v)", d, s.kinds())
			}
		})
	}
}

func TestNodesKeptOnAutopilot(t *testing.T) {
	tr, s, c := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
	tr.nodesListed([]*corev1.Node{node("n0", "nap-1", "v1"), node("x1", "nap-1", "v1")}, true, false)
	// Without the pool rule the autoscaler's nodes of the same pool are
	// not kept.
	tr.nodeChanged(node("x2", "nap-1", "v1"))
	tr.nodesListed([]*corev1.Node{node("n0", "nap-1", "v1"), node("x2", "nap-1", "v1")}, false, false)
	if len(s.all()) != 0 {
		t.Fatalf("autoscaler nodes recorded: %v", s.kinds())
	}
	// A node added within 15 minutes of a drain belongs to it.
	cord := node("n0", "nap-1", "v1")
	cord.Spec.Unschedulable = true
	tr.nodeChanged(cord)
	del := natsPod("nats-0", "n0", true)
	ts := metav1.NewTime(c.now().Add(time.Minute))
	del.DeletionTimestamp = &ts
	tr.podChanged(del)
	c.add(10 * time.Minute)
	surge := node("n9", "nap-1", "v2")
	surge.Status.Conditions[0].Status = corev1.ConditionFalse
	tr.nodeChanged(surge)
	tr.nodeChanged(node("n9", "nap-1", "v2"))
	c.add(20 * time.Minute)
	tr.nodeChanged(node("late", "nap-1", "v2"))
	want := []string{KindNodeCordoned, KindPodTerminating, KindNodeDrained, KindNodeAdded, KindNodeReady}
	if got := s.kinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
}

// A client pod's OOM kill and crash loop get kinds of their own, so the
// timeline can draw them for the pods that are NATS clients.
func TestClientPodFailureKinds(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.eventObserved(kevent("o1", "example", "Pod", "media-worker-1", "OOMKilled", corev1.EventTypeWarning, "out of memory", 1, t0))
	tr.eventObserved(kevent("c1", "example", "Pod", "media-worker-1", "CrashLoopBackOff", corev1.EventTypeWarning, "", 1, t0))
	tr.eventObserved(kevent("f1", "example", "Pod", "media-worker-2", "FailedScheduling", corev1.EventTypeWarning, "", 1, t0))
	want := []string{KindPodOOMKilled, KindPodBackOff, KindPodFailedScheduling}
	if got := s.kinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
}

// A NATS pod's container restarted after an OOM kill (its last
// termination says OOMKilled) is also recorded as k8s.pod.oom_killed; a
// restart for another reason is not.
func TestOOMKillFromRestart(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-1", "n1", true), natsPod("nats-2", "n1", true)}, true, false)
	for name, reason := range map[string]string{"nats-1": "OOMKilled", "nats-2": "Error"} {
		p := natsPod(name, "n1", true)
		p.Status.ContainerStatuses[0].RestartCount = 1
		p.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: reason, ExitCode: 137,
			FinishedAt: metav1.NewTime(t0)}
		tr.podChanged(p)
	}
	var oom []string
	for _, e := range s.all() {
		if e.Kind == KindPodOOMKilled {
			oom = append(oom, e.ServerName)
		}
	}
	if len(oom) != 1 || oom[0] != "nats-1" {
		t.Fatalf("oom kills %v (%v)", oom, s.kinds())
	}
}

// A node's upgrade Event names the node and its pool in the payload, as
// the node watch's events do, so that Replay counts it.
func TestNodeUpgradeEventNamesNodeAndPool(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
	tr.nodesListed([]*corev1.Node{node("n0", "pool-1", "v1")}, true, false)
	s.reset()
	tr.eventObserved(kevent("k", "default", "Node", "n0", "NodeUpgrade", "Normal", "upgrading to 1.34", 1, t0.Add(time.Minute)))
	e, ok := find(s.all(), KindNodeUpgrade)
	if !ok {
		t.Fatalf("kinds %v", s.kinds())
	}
	if raw := rawOf(t, e); raw["node"] != "n0" || raw["pool"] != "pool-1" {
		t.Errorf("raw = %v", raw)
	}
}
