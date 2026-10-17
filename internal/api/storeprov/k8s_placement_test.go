package storeprov_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/events"
)

// A newer event of the same pod without a node keeps the node; a
// deletion clears it until the pod is scheduled again.
func TestKubernetesNodeFromTheLastEventWithOne(t *testing.T) {
	notReady := podEvent(t0.Add(-30*time.Second), "nats", "k8s.pod.not_ready", "")
	if k := getServer(t, detailStoreWith(t, true, detailOpts{extra: []events.Event{notReady}}), "nats-0").GetKubernetes(); k.GetNode() != "node-b" {
		t.Errorf("after an event without a node: %v", k)
	}
	deleted := podEvent(t0.Add(-40*time.Second), "nats", "k8s.pod.deleted", "node-b")
	if k := getServer(t, detailStoreWith(t, true, detailOpts{extra: []events.Event{deleted, notReady}}), "nats-0").GetKubernetes(); k.GetNode() != "" {
		t.Errorf("after the pod's deletion: %v", k)
	}
	again := podEvent(t0.Add(-20*time.Second), "nats", "k8s.pod.scheduled", "node-c")
	if k := getServer(t, detailStoreWith(t, true, detailOpts{extra: []events.Event{deleted, again, notReady}}), "nats-0").GetKubernetes(); k.GetNode() != "node-c" {
		t.Errorf("after the pod was scheduled again: %v", k)
	}
}

// Events of a pod of the same name in another watched namespace do not
// count for the server's pod.
func TestKubernetesPlacementKeepsToItsNamespace(t *testing.T) {
	other := []events.Event{
		podEvent(t0.Add(-15*time.Minute), "other", "k8s.pod.scheduled", "node-x"),
		podEvent(t0.Add(-14*time.Minute), "other", "k8s.pod.restarted", "node-x"),
	}
	k := getServer(t, detailStoreWith(t, true, detailOpts{extra: other}), "nats-0").GetKubernetes()
	if k.GetNamespace() != "nats" || k.GetNode() != "node-b" || !k.GetScheduledAt().AsTime().Equal(t0.Add(-20*time.Minute)) || k.GetRestartsInRange() != 1 {
		t.Errorf("kubernetes = %v", k)
	}
}
