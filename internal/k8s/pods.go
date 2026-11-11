package k8s

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// podState is what the tracker keeps of a NATS pod.
type podState struct {
	uid, ns, name, node, phase string
	server                     string
	// container is the pod's first container, the NATS server; Events of
	// other containers (the config reloader) are not stored.
	container string
	ready     bool
	restarts  int32
	deleting  bool
	// disrupted is set once an eviction was recorded, so that the
	// condition and the kubelet's Event do not record it twice.
	disrupted bool
}

func podKey(ns, name string) string { return ns + "/" + name }

func (t *tracker) podStateOf(p *corev1.Pod) *podState {
	s := &podState{uid: string(p.UID), ns: p.Namespace, name: p.Name, node: p.Spec.NodeName, phase: string(p.Status.Phase),
		server: t.serverName(p.Namespace, p.Name), deleting: p.DeletionTimestamp != nil}
	if len(p.Spec.Containers) > 0 {
		s.container = p.Spec.Containers[0].Name
	}
	for _, c := range p.Status.Conditions {
		switch c.Type {
		case corev1.PodReady:
			s.ready = c.Status == corev1.ConditionTrue
		case corev1.DisruptionTarget:
			s.disrupted = c.Status == corev1.ConditionTrue
		}
	}
	for _, c := range p.Status.ContainerStatuses {
		s.restarts += c.RestartCount
	}
	return s
}

// podsListed takes a (re)list of the NATS pods of namespace ns. The first
// list sets the state without recording; a later one records what changed
// since the watch broke, timed now, and deletes the pods no longer there,
// unless the list was truncated and may have left them out.
func (t *tracker) podsListed(ns string, pods []*corev1.Pod, initial, truncated bool) {
	present := map[string]bool{}
	for _, p := range pods {
		present[podKey(p.Namespace, p.Name)] = true
		if initial {
			t.setPod(t.podStateOf(p))
			continue
		}
		t.podChanged(p)
	}
	for k, s := range t.pods {
		if !truncated && s.ns == ns && !present[k] {
			t.podGone(s, time.Time{})
		}
	}
	if initial {
		t.podsAtStart(ns, pods, truncated)
	}
}

// setPod stores the pod's state and notes its node as a NATS node.
func (t *tracker) setPod(s *podState) {
	k := podKey(s.ns, s.name)
	t.pods[k] = s
	t.natsPods[k] = t.now()
	if s.node != "" {
		t.noteNATSNode(s.node)
	}
}

// podChanged records the differences between the stored state of p and p.
func (t *tracker) podChanged(p *corev1.Pod) {
	k := podKey(p.Namespace, p.Name)
	old := t.pods[k]
	cur := t.podStateOf(p)
	if old != nil && old.uid != cur.uid {
		// A StatefulSet pod of the same name: the old one is gone.
		t.podGone(old, time.Time{})
		old = nil
	}
	if old == nil {
		old = &podState{uid: cur.uid, ns: cur.ns, name: cur.name, server: cur.server}
	}
	raw := podRaw(p)
	if old.node == "" && cur.node != "" {
		t.record(time.Time{}, KindPodScheduled, "Scheduled", cur.server, "",
			fmt.Sprintf("pod %s scheduled on %s", cur.name, cur.node), raw)
	}
	if cur.disrupted && !old.disrupted {
		reason, msg := disruption(p)
		t.record(time.Time{}, KindPodEvicted, "DisruptionTarget", cur.server, reason,
			fmt.Sprintf("pod %s evicted from %s: %s", cur.name, cur.node, firstNonEmpty(msg, reason)), raw)
		t.podDisrupted(cur)
	}
	if cur.deleting && !old.deleting {
		if !cur.disrupted {
			t.record(deletionTime(p), KindPodTerminating, "Terminating", cur.server, "",
				fmt.Sprintf("pod %s terminating on %s, grace %ds", cur.name, cur.node, grace(p)), raw)
		}
		t.podDisrupted(cur)
	}
	if cur.ready != old.ready {
		kind, typ, word := KindPodReady, "Ready", "ready"
		if !cur.ready {
			kind, typ, word = KindPodNotReady, "NotReady", "not ready"
		}
		t.record(readyTime(p), kind, typ, cur.server, "", fmt.Sprintf("pod %s %s on %s", cur.name, word, cur.node), raw)
	}
	if cur.restarts > old.restarts && old.phase != "" {
		reason, code := lastTermination(p)
		t.record(time.Time{}, KindPodRestarted, "Restarted", cur.server, reason,
			fmt.Sprintf("pod %s container restarted (%d restarts), last exit %s code %d", cur.name, cur.restarts, firstNonEmpty(reason, "unknown"), code), raw)
		if reason == "OOMKilled" {
			// The kubelet sends no Pod Event for an OOM kill; the last
			// termination of the restarted container says it.
			t.record(time.Time{}, KindPodOOMKilled, "OOMKilled", cur.server, reason,
				fmt.Sprintf("pod %s container killed out of memory (%d restarts)", cur.name, cur.restarts), raw)
		}
	}
	// Keep the eviction mark of an Event the condition has not shown yet.
	cur.disrupted = cur.disrupted || old.disrupted
	t.setPod(cur)
}

// podGone records a NATS pod deleted and forgets it. at is zero when the
// time is not known (a relist).
func (t *tracker) podGone(s *podState, at time.Time) {
	t.record(at, KindPodDeleted, "Deleted", s.server, "", fmt.Sprintf("pod %s deleted from %s", s.name, s.node),
		map[string]any{"namespace": s.ns, "pod": s.name, "uid": s.uid, "node": s.node})
	delete(t.pods, podKey(s.ns, s.name))
	t.natsPods[podKey(s.ns, s.name)] = t.now()
}

// podDeleted handles a watch's delete of p.
func (t *tracker) podDeleted(p *corev1.Pod) {
	s := t.pods[podKey(p.Namespace, p.Name)]
	if s == nil || s.uid != string(p.UID) {
		s = t.podStateOf(p)
	}
	t.podGone(s, time.Time{})
}

func podRaw(p *corev1.Pod) map[string]any {
	return map[string]any{"namespace": p.Namespace, "pod": p.Name, "uid": string(p.UID), "node": p.Spec.NodeName,
		"phase": string(p.Status.Phase)}
}

// disruption returns the reason and message of the pod's DisruptionTarget
// condition, for example EvictionByEvictionAPI.
func disruption(p *corev1.Pod) (string, string) {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.DisruptionTarget {
			return c.Reason, c.Message
		}
	}
	return "", ""
}

// readyTime is when the Ready condition last changed; zero when unknown.
func readyTime(p *corev1.Pod) time.Time {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.LastTransitionTime.Time
		}
	}
	return time.Time{}
}

// deletionTime is when the pod was asked to stop: its deletion timestamp
// less the grace period it was given.
func deletionTime(p *corev1.Pod) time.Time {
	if p.DeletionTimestamp == nil {
		return time.Time{}
	}
	return p.DeletionTimestamp.Add(-time.Duration(grace(p)) * time.Second)
}

func grace(p *corev1.Pod) int64 {
	if p.DeletionGracePeriodSeconds != nil {
		return *p.DeletionGracePeriodSeconds
	}
	if p.Spec.TerminationGracePeriodSeconds != nil {
		return *p.Spec.TerminationGracePeriodSeconds
	}
	return 0
}

// lastTermination is the reason and exit code of the newest terminated
// container state.
func lastTermination(p *corev1.Pod) (string, int32) {
	var reason string
	var code int32
	var at time.Time
	for _, c := range p.Status.ContainerStatuses {
		if tm := c.LastTerminationState.Terminated; tm != nil && !tm.FinishedAt.Time.Before(at) {
			reason, code, at = tm.Reason, tm.ExitCode, tm.FinishedAt.Time
		}
	}
	return reason, code
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
