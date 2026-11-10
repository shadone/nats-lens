package k8s

import (
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func flag(b bool) *bool { return &b }

func TestFirstListsComparedWithTheStore(t *testing.T) {
	tr, s, _ := newTestTracker()
	tr.prior = Prior{
		Pods: map[string]PriorState{
			"example/nats-0": {Flag: flag(true)},  // now not ready
			"example/nats-1": {Flag: flag(true)},  // still ready
			"example/nats-2": {Flag: flag(false)}, // gone
			"example/nats-9": {Deleted: true},     // gone before, not again
			"other/nats-0":   {Flag: flag(true)},  // another namespace
		},
		Nodes: map[string]PriorState{
			"n0": {Flag: flag(false)}, // now cordoned
			"n1": {Flag: flag(false)}, // unchanged
			"n2": {},                  // gone
		},
	}
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", false), natsPod("nats-1", "n1", true)}, true, false)
	cord := node("n0", "pool-1", "v1")
	cord.Spec.Unschedulable = true
	tr.nodesListed([]*corev1.Node{cord, node("n1", "pool-1", "v1")}, true)
	want := []string{KindPodNotReady, KindPodDeleted, KindNodeCordoned, KindNodeDeleted}
	if got := s.kinds(); !slices.Equal(got, want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	for _, e := range s.all() {
		if rawOf(t, e)["observed_at_start"] != true {
			t.Fatalf("%s not marked: %s", e.Kind, e.Raw)
		}
	}
	if e := s.all()[1]; e.ServerName != "nats-2" {
		t.Fatalf("deleted %+v", e)
	}
	// Later relists are not compared with the store again.
	s.reset()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", false), natsPod("nats-1", "n1", true)}, false, false)
	if len(s.all()) != 0 {
		t.Fatalf("relist recorded %v", s.kinds())
	}
}

func TestStartGap(t *testing.T) {
	if _, ok := startGap(Prior{}, t0, 30*time.Second); ok {
		t.Fatal("gap for an empty store")
	}
	if _, ok := startGap(Prior{Last: t0.Add(-20 * time.Second)}, t0, 30*time.Second); ok {
		t.Fatal("gap shorter than gap_after")
	}
	ev, ok := startGap(Prior{Last: t0.Add(-5 * time.Minute)}, t0, 30*time.Second)
	if !ok || ev.Kind != KindGap || !ev.Time.Equal(t0.Add(-5*time.Minute)) || rawOf(t, ev)["seconds"].(float64) != 300 {
		t.Fatalf("gap %+v", ev)
	}
}
