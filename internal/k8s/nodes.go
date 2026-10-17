package k8s

import (
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Labels and taints of GKE and the cluster autoscaler.
const (
	poolLabel = "cloud.google.com/gke-nodepool"
	// autoscalerTaint marks a node the cluster autoscaler is removing; it
	// drains the node without setting it unschedulable.
	autoscalerTaint = "ToBeDeletedByClusterAutoscaler"
)

// nodeState is what the tracker keeps of a node.
type nodeState struct {
	name, uid, pool, version string
	// known is false for a node only named by a NATS pod so far.
	known    bool
	cordoned bool
	// cause is why the node is cordoned: unschedulable or autoscaler.
	cause string
	ready bool
	// drained is set once a drain was recorded for the current cordon.
	drained bool
	// natsAt is when a NATS pod was last seen on the node.
	natsAt time.Time
	// windowAdded is set for a node added while a drain or upgrade was
	// under way.
	windowAdded bool
}

func nodeStateOf(n *corev1.Node) *nodeState {
	s := &nodeState{name: n.Name, uid: string(n.UID), pool: n.Labels[poolLabel], version: n.Status.NodeInfo.KubeletVersion,
		known: true, cordoned: n.Spec.Unschedulable}
	for _, tn := range n.Spec.Taints {
		if tn.Key == autoscalerTaint {
			s.cordoned = true
		}
	}
	switch {
	case n.Spec.Unschedulable:
		s.cause = "unschedulable"
	case s.cordoned:
		s.cause = "autoscaler"
	}
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			s.ready = c.Status == corev1.ConditionTrue
		}
	}
	return s
}

// noteNATSNode marks a node as running a NATS pod now.
func (t *tracker) noteNATSNode(name string) {
	n := t.nodes[name]
	if n == nil {
		n = &nodeState{name: name}
		t.nodes[name] = n
	}
	n.natsAt = t.now()
	if n.pool != "" {
		t.pools[n.pool] = n.natsAt
	}
}

// natsServersOn lists the servers of the NATS pods on a node.
func (t *tracker) natsServersOn(node string) []string {
	var out []string
	for _, p := range t.pods {
		if p.node == node {
			out = append(out, p.server)
		}
	}
	slices.Sort(out)
	return out
}

// natsNode reports whether a NATS pod runs on the node or left it within
// natsNodeFor.
func (t *tracker) natsNode(n *nodeState) bool {
	return len(t.natsServersOn(n.name)) > 0 || (!n.natsAt.IsZero() && t.now().Sub(n.natsAt) <= natsNodeFor)
}

// relevant reports whether a node's changes are kept: a NATS node, a
// node added while a drain or upgrade was under way (the surge node that
// ends it), or with poolNodes a node of a pool that held a NATS node. On
// Autopilot every node is in a few autoscaler pools, so the pool rule is
// off by default.
func (t *tracker) relevant(n *nodeState) bool {
	if t.natsNode(n) || n.windowAdded {
		return true
	}
	if !t.poolNodes {
		return false
	}
	at, ok := t.pools[n.pool]
	return n.pool != "" && ok && t.now().Sub(at) <= natsPoolFor
}

// nodesListed takes a (re)list of the nodes; like podsListed, the first
// list only sets the state.
func (t *tracker) nodesListed(nodes []*corev1.Node, initial bool) {
	present := map[string]bool{}
	for _, n := range nodes {
		present[n.Name] = true
		if initial {
			t.setNode(nodeStateOf(n), t.nodes[n.Name])
			continue
		}
		t.nodeChanged(n)
	}
	for name, s := range t.nodes {
		if s.known && !present[name] {
			t.nodeGone(s)
		}
	}
	if initial {
		t.nodesAtStart(nodes)
	}
}

// setNode stores cur, keeping what old knew of NATS pods and drains.
func (t *tracker) setNode(cur, old *nodeState) {
	if old != nil {
		cur.natsAt = old.natsAt
		cur.drained = old.drained && cur.cordoned
		cur.windowAdded = old.windowAdded
	}
	if !cur.natsAt.IsZero() && cur.pool != "" {
		if cur.natsAt.After(t.pools[cur.pool]) {
			t.pools[cur.pool] = cur.natsAt
		}
	}
	if len(t.natsServersOn(cur.name)) > 0 {
		cur.natsAt = t.now()
		if cur.pool != "" {
			t.pools[cur.pool] = cur.natsAt
		}
	}
	t.nodes[cur.name] = cur
}

// nodeChanged records the differences between the stored state of n and n.
func (t *tracker) nodeChanged(n *corev1.Node) {
	old := t.nodes[n.Name]
	cur := nodeStateOf(n)
	t.setNode(cur, old)
	if (old == nil || !old.known || old.uid != cur.uid) && t.inDisruption() {
		cur.windowAdded = true
	}
	if !t.relevant(cur) {
		return
	}
	raw := t.nodeRaw(cur)
	if old == nil || !old.known || old.uid != cur.uid {
		t.record(n.CreationTimestamp.Time, KindNodeAdded, "Added", "", "",
			fmt.Sprintf("node %s added to pool %s, %s", cur.name, firstNonEmpty(cur.pool, "?"), cur.version), raw)
		old = &nodeState{name: cur.name, ready: false}
	}
	if cur.cordoned && !old.cordoned {
		t.record(time.Time{}, KindNodeCordoned, "Cordoned", "", cordonCause(n),
			fmt.Sprintf("node %s cordoned (%s)%s", cur.name, cordonCause(n), serversNote(raw)), raw)
		// The watches do not order a node's cordon and its pods' eviction:
		// a NATS pod already leaving makes this cordon a drain.
		for _, p := range t.pods {
			if p.node == cur.name && (p.deleting || p.disrupted) {
				t.podDisrupted(p)
				break
			}
		}
	}
	if !cur.cordoned && old.cordoned {
		t.record(time.Time{}, KindNodeUncordoned, "Uncordoned", "", "", fmt.Sprintf("node %s uncordoned", cur.name), raw)
	}
	if cur.ready != old.ready {
		kind, typ, word := KindNodeReady, "NodeReady", "ready"
		if !cur.ready {
			kind, typ, word = KindNodeNotReady, "NodeNotReady", "not ready"
		}
		t.record(nodeReadyTime(n), kind, typ, "", "", fmt.Sprintf("node %s %s%s", cur.name, word, serversNote(raw)), raw)
	}
	if old.version != "" && cur.version != old.version {
		t.record(time.Time{}, KindNodeUpgrade, "KubeletVersion", "", "",
			fmt.Sprintf("node %s kubelet %s -> %s", cur.name, old.version, cur.version), raw)
		t.lastDisruption = t.now()
	}
}

// nodeGone records a deleted node and forgets it.
func (t *tracker) nodeGone(s *nodeState) {
	if t.relevant(s) {
		t.record(time.Time{}, KindNodeDeleted, "Deleted", "", "", fmt.Sprintf("node %s deleted from pool %s", s.name, firstNonEmpty(s.pool, "?")),
			t.nodeRaw(s))
	}
	delete(t.nodes, s.name)
}

// podDisrupted records the drain of a cordoned node the first time one
// of its NATS pods is evicted or terminated.
func (t *tracker) podDisrupted(p *podState) {
	n := t.nodes[p.node]
	if n == nil || !n.cordoned || n.drained {
		return
	}
	n.drained = true
	t.lastDisruption = t.now()
	raw := t.nodeRaw(n)
	raw["servers"] = []string{p.server}
	raw["cause"] = n.cause
	t.record(time.Time{}, KindNodeDrained, "NodeDrain", "", "",
		fmt.Sprintf("node %s %s: %s leaves it", n.name, map[bool]string{true: "removed by the autoscaler", false: "cordoned and drained"}[n.cause == "autoscaler"], p.server), raw)
}

func (t *tracker) nodeRaw(n *nodeState) map[string]any {
	raw := map[string]any{"node": n.name, "pool": n.pool, "kubelet_version": n.version}
	if s := t.natsServersOn(n.name); len(s) > 0 {
		raw["servers"] = s
	}
	return raw
}

func serversNote(raw map[string]any) string {
	if s, ok := raw["servers"].([]string); ok && len(s) > 0 {
		return fmt.Sprintf(", running %v", s)
	}
	return ""
}

func cordonCause(n *corev1.Node) string {
	if n.Spec.Unschedulable {
		return "unschedulable"
	}
	return "autoscaler removal"
}

func nodeReadyTime(n *corev1.Node) time.Time {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.LastTransitionTime.Time
		}
	}
	return time.Time{}
}

// disruptionGap is how long after a drain or an upgrade a node added
// still belongs to it; the store provider's windows use the same gap.
const disruptionGap = 15 * time.Minute

// inDisruption reports whether a drain or upgrade was recorded within
// disruptionGap.
func (t *tracker) inDisruption() bool {
	return !t.lastDisruption.IsZero() && t.now().Sub(t.lastDisruption) <= disruptionGap
}
