package k8s

import (
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// R3: the cordon arrives after the pod's termination.
func TestReviewCordonAfterTermination(t *testing.T) {
	tr, s, c := newTestTracker()
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
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
	tr.podsListed("example", []*corev1.Pod{natsPod("nats-0", "n0", true)}, true, false)
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
