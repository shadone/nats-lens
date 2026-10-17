package k8s

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/shadone/nats-lens/internal/events"
)

var t0 = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

// sink collects recorded events.
type sink struct {
	mu  sync.Mutex
	evs []events.Event
}

func (s *sink) Record(e events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, e)
}

func (s *sink) all() []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.evs)
}

func (s *sink) kinds() []string {
	var out []string
	for _, e := range s.all() {
		out = append(out, e.Kind)
	}
	return out
}

func (s *sink) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = nil
}

// clock is a settable time.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestTracker() (*tracker, *sink, *clock) {
	s := &sink{}
	c := &clock{t: t0}
	return newTracker(serverNameFunc("{pod}"), c.now, s.Record, nil), s, c
}

func natsPod(name, node string, ready bool) *corev1.Pod {
	st := corev1.ConditionFalse
	if ready {
		st = corev1.ConditionTrue
	}
	grace := int64(60)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "example", UID: types.UID("uid-" + name),
			Labels: map[string]string{"app.kubernetes.io/name": "nats", "app.kubernetes.io/component": "nats"}},
		Spec: corev1.PodSpec{NodeName: node, TerminationGracePeriodSeconds: &grace,
			Containers: []corev1.Container{{Name: "nats"}, {Name: "reloader"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: st, LastTransitionTime: metav1.NewTime(t0)}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "nats"}, {Name: "reloader"}}},
	}
}

func node(name, pool, version string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID("uid-" + name), Labels: map[string]string{poolLabel: pool},
			CreationTimestamp: metav1.NewTime(t0)},
		Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: version},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(t0)}}},
	}
}

func kevent(uid, ns, kind, name, reason, typ, note string, count int32, at time.Time) *eventsv1.Event {
	return &eventsv1.Event{
		ObjectMeta:      metav1.ObjectMeta{Name: name + "." + uid, Namespace: ns, UID: types.UID(uid), CreationTimestamp: metav1.NewTime(at)},
		Regarding:       corev1.ObjectReference{Kind: kind, Namespace: ns, Name: name},
		Reason:          reason,
		Type:            typ,
		Note:            note,
		DeprecatedCount: count, DeprecatedLastTimestamp: metav1.NewTime(at),
	}
}

func rawOf(t *testing.T, e events.Event) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.Raw, &m); err != nil {
		t.Fatalf("raw of %s: %v", e.Kind, err)
	}
	return m
}

func find(evs []events.Event, kind string) (events.Event, bool) {
	for _, e := range evs {
		if e.Kind == kind {
			return e, true
		}
	}
	return events.Event{}, false
}
