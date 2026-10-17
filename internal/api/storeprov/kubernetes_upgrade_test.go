package storeprov

import (
	"testing"
	"time"
)

func TestDeriveWindows(t *testing.T) {
	base := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	evs := []k8sEvent{
		// A cordon alone, as the cluster autoscaler makes, opens nothing.
		{at: at(0), kind: "k8s.node.cordoned", node: "n9", version: "v1.33.4"},
		// The upgrade: three drains 12 minutes apart, each NATS pod stops
		// and comes back, new nodes on the next version.
		{at: at(20), kind: "k8s.node.cordoned", node: "n0", version: "v1.33.4"},
		{at: at(21), kind: "k8s.node.drained", node: "n0", version: "v1.33.4"},
		{at: at(21), kind: "k8s.pod.evicted", server: "nats-0"},
		{at: at(24), kind: "k8s.pod.ready", server: "nats-0"},
		{at: at(25), kind: "k8s.node.added", node: "n3", version: "v1.33.5"},
		{at: at(33), kind: "k8s.node.drained", node: "n1", version: "v1.33.4"},
		{at: at(33), kind: "k8s.pod.killing", server: "nats-1"},
		{at: at(36), kind: "k8s.pod.ready", server: "nats-1"},
		{at: at(45), kind: "k8s.node.drained", node: "n2", version: "v1.33.4"},
		{at: at(45), kind: "k8s.pod.terminating", server: "nats-2"},
		{at: at(50), kind: "k8s.pod.ready", server: "nats-2"},
		// 20 minutes later a single drain: a window of its own.
		{at: at(70), kind: "k8s.node.drained", node: "n3", version: "v1.33.5"},
		{at: at(71), kind: "k8s.pod.restarted", server: "nats-0"},
	}
	ws := deriveWindows(evs, 15*time.Minute)
	if len(ws) != 2 {
		t.Fatalf("windows %v", ws)
	}
	w := ws[0]
	if !w.GetStart().AsTime().Equal(at(21)) || !w.GetEnd().AsTime().Equal(at(50)) || w.GetLabel() != "GKE node upgrade" ||
		w.GetDetail() != "3 drains, 3 pod restarts" {
		t.Fatalf("first window %v", w)
	}
	w = ws[1]
	if !w.GetStart().AsTime().Equal(at(70)) || !w.GetEnd().AsTime().Equal(at(71)) || w.GetLabel() != "Node drain" ||
		w.GetDetail() != "1 drain, 1 pod restart" {
		t.Fatalf("second window %v", w)
	}
	if ws := deriveWindows(evs[:1], 15*time.Minute); len(ws) != 0 {
		t.Fatalf("a cordon alone made %v", ws)
	}
}

func TestWindowRules(t *testing.T) {
	base := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	// An upgrade Event with no drain and no NATS pod event is no window.
	if ws := deriveWindows([]k8sEvent{{at: at(0), kind: "k8s.node.upgrade", node: "n0"}, {at: at(1), kind: "k8s.node.ready", node: "n0"}},
		15*time.Minute); len(ws) != 0 {
		t.Fatalf("upgrade without NATS made %v", ws)
	}
	// An upgrade followed by a NATS pod restart is.
	ws := deriveWindows([]k8sEvent{{at: at(0), kind: "k8s.node.upgrade", node: "n0"}, {at: at(2), kind: "k8s.pod.killing", server: "nats-0"}},
		15*time.Minute)
	if len(ws) != 1 || ws[0].GetLabel() != "GKE node upgrade" {
		t.Fatalf("upgrade with a pod %v", ws)
	}
	// Drains by the autoscaler only.
	ws = deriveWindows([]k8sEvent{{at: at(0), kind: "k8s.node.drained", node: "n0", cause: "autoscaler"},
		{at: at(1), kind: "k8s.pod.terminating", server: "nats-0"}}, 15*time.Minute)
	if len(ws) != 1 || ws[0].GetLabel() != "Node removed by the autoscaler" || ws[0].GetDetail() != "1 drain, 1 pod restart" {
		t.Fatalf("autoscaler window %v", ws)
	}
}
