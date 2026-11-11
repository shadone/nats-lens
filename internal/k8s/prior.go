package k8s

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/shadone/nats-lens/internal/events"
)

// PriorState is what the stored events last said of a pod or a node.
type PriorState struct {
	// Flag is ready for a pod, cordoned for a node; nil when unknown.
	Flag    *bool
	Deleted bool
}

// Prior is what the store knew when natslens started: the last state of
// the NATS pods (by namespace/name) and nodes, and when the watcher last
// ran.
type Prior struct {
	Pods  map[string]PriorState
	Nodes map[string]PriorState
	Last  time.Time
}

// startGap returns the collector.k8s_gap for the time natslens did not
// watch, from prior.Last to now, when it is longer than gapAfter.
func startGap(prior Prior, now time.Time, gapAfter time.Duration) (events.Event, bool) {
	if prior.Last.IsZero() || now.Sub(prior.Last) <= gapAfter {
		return events.Event{}, false
	}
	gap := now.Sub(prior.Last).Round(time.Second)
	ev := collectorEvent(prior.Last, KindGap, "natslens was not running",
		fmt.Sprintf("kubernetes not watched for %s while natslens was not running, from %s to %s", gap,
			prior.Last.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339)),
		map[string]any{"source": "natslens", "from": prior.Last.UTC(), "to": now.UTC(), "seconds": int64(gap / time.Second)})
	ev.ReceivedAt = now.UTC()
	return ev, true
}

// observedAtStart marks an event recorded from the difference between the
// stored state and the first list.
func observedAtStart(raw map[string]any) map[string]any {
	raw["observed_at_start"] = true
	return raw
}

// podsAtStart records how the NATS pods of namespace ns changed while
// natslens was down: readiness, and pods that are gone, unless the list
// was truncated and may have left them out.
func (t *tracker) podsAtStart(ns string, pods []*corev1.Pod, truncated bool) {
	present := map[string]bool{}
	for _, p := range pods {
		k := podKey(p.Namespace, p.Name)
		present[k] = true
		was, ok := t.prior.Pods[k]
		s := t.pods[k]
		if !ok || was.Deleted || was.Flag == nil || s == nil || *was.Flag == s.ready {
			continue
		}
		kind, typ, word := KindPodReady, "Ready", "ready"
		if !s.ready {
			kind, typ, word = KindPodNotReady, "NotReady", "not ready"
		}
		t.record(time.Time{}, kind, typ, s.server, "", fmt.Sprintf("pod %s %s on %s (observed at start)", s.name, word, s.node),
			observedAtStart(podRaw(p)))
	}
	for k, was := range t.prior.Pods {
		if truncated || !strings.HasPrefix(k, ns+"/") || was.Deleted || present[k] {
			continue
		}
		name := strings.TrimPrefix(k, ns+"/")
		t.record(time.Time{}, KindPodDeleted, "Deleted", t.serverName(ns, name), "",
			fmt.Sprintf("pod %s deleted (observed at start)", name),
			observedAtStart(map[string]any{"namespace": ns, "pod": name}))
		t.natsPods[k] = t.now()
	}
}

// nodesAtStart records the cordons, uncordons and deletions of the nodes
// the store knew while natslens was down; a truncated list records no
// deletion.
func (t *tracker) nodesAtStart(nodes []*corev1.Node, truncated bool) {
	present := map[string]bool{}
	for _, n := range nodes {
		present[n.Name] = true
		was, ok := t.prior.Nodes[n.Name]
		s := t.nodes[n.Name]
		if !ok || was.Deleted || was.Flag == nil || s == nil || *was.Flag == s.cordoned {
			continue
		}
		raw := observedAtStart(t.nodeRaw(s))
		if s.cordoned {
			t.record(time.Time{}, KindNodeCordoned, "Cordoned", "", cordonCause(n),
				fmt.Sprintf("node %s cordoned (%s) (observed at start)", s.name, cordonCause(n)), raw)
		} else {
			t.record(time.Time{}, KindNodeUncordoned, "Uncordoned", "", "", fmt.Sprintf("node %s uncordoned (observed at start)", s.name), raw)
		}
	}
	for name, was := range t.prior.Nodes {
		if truncated || was.Deleted || present[name] {
			continue
		}
		t.record(time.Time{}, KindNodeDeleted, "Deleted", "", "", fmt.Sprintf("node %s deleted (observed at start)", name),
			observedAtStart(map[string]any{"node": name}))
	}
}
