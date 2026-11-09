package api

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// K8sLaneKinds are the Kubernetes events drawn on the timeline: node
// disruptions, and the NATS pods' stops, starts and readiness. Pod kinds
// are drawn only for NATS pods (with a server name).
var K8sLaneKinds = []string{
	"k8s.node.cordoned", "k8s.node.drained", "k8s.node.not_ready", "k8s.node.upgrade", "k8s.node.deleted",
	"k8s.pod.killing", "k8s.pod.evicted", "k8s.pod.terminating", "k8s.pod.started", "k8s.pod.ready", "k8s.pod.not_ready",
	"k8s.pod.restarted", "k8s.pod.scheduled", "k8s.pod.preempted", "k8s.pod.oom_killed",
	"collector.k8s_gap", "collector.k8s_not_watching",
}

// NATSLaneKinds are the NATS events worth drawing on the timeline's NATS
// lane, besides the finding transitions.
var NATSLaneKinds = []string{
	"stream.created", "stream.deleted", "stream.leader_elected", "stream.quorum_lost",
	"consumer.created", "consumer.deleted", "consumer.leader_elected", "consumer.quorum_lost",
	"meta.leader_elected", "server.lameduck", "server.shutdown", "server.out_of_storage", "collector.gap",
}

// K8sUpgradeGap is the longest quiet time inside one node upgrade window:
// an event later than this after the window's last one starts a new
// window.
const K8sUpgradeGap = 15 * time.Minute

// K8sWindowKinds are the events a window is made of; windowStarts are
// the ones that open a window. A cordon alone, which the cluster autoscaler
// also makes, opens none: a NATS pod has to leave the node.
var (
	K8sWindowKinds = []string{
		"k8s.node.cordoned", "k8s.node.drained", "k8s.node.upgrade", "k8s.node.deleted", "k8s.node.added",
		"k8s.node.ready", "k8s.node.not_ready", "k8s.node.uncordoned",
		"k8s.pod.killing", "k8s.pod.evicted", "k8s.pod.terminating", "k8s.pod.deleted", "k8s.pod.scheduled",
		"k8s.pod.started", "k8s.pod.ready", "k8s.pod.not_ready", "k8s.pod.restarted",
	}
	windowStarts = map[string]bool{"k8s.node.drained": true, "k8s.node.upgrade": true}
	// podStops count as pod restarts in a window's detail.
	podStops = map[string]bool{"k8s.pod.killing": true, "k8s.pod.evicted": true, "k8s.pod.terminating": true, "k8s.pod.restarted": true}
)

// K8sWindowEvent is a Kubernetes event as the windows need it.
type K8sWindowEvent struct {
	ID   string
	At   time.Time
	Kind string
	// Server is a NATS pod's server; Node, Version, Cause and Pool come
	// from the payload: the node, its kubelet version, a drain's cordon
	// cause (unschedulable or autoscaler) and its GKE node pool.
	Server, Node, Version, Cause, Pool string
}

// K8sShortLabel names a lane item briefly: "nats-0 Killing", or "node"
// and the node's last name segment.
func K8sShortLabel(ev *v1.Event, typ, node string) string {
	what := strings.TrimPrefix(strings.TrimPrefix(ev.GetKind(), "k8s."), "collector.")
	if ev.GetServer() != "" {
		return ev.GetServer() + " " + cmp.Or(typ, what)
	}
	if strings.HasPrefix(ev.GetKind(), "k8s.node.") {
		// GKE node names end in a short unique suffix: gke-pool-1-7f3a.
		if i := strings.LastIndex(node, "-"); i >= 0 && i < len(node)-1 {
			return "node " + node[i+1:]
		}
		return "node " + cmp.Or(node, strings.TrimPrefix(what, "node."))
	}
	return what
}

// DeriveK8sWindows groups time-ordered events into windows: a window
// opens at a node drain or upgrade and takes in every following event
// that comes within gap of the one before; one without a drain or a NATS
// pod event is not shown.
func DeriveK8sWindows(evs []K8sWindowEvent, gap time.Duration) []*v1.TimeWindow {
	var out []*v1.TimeWindow
	var cur []K8sWindowEvent
	closeWindow := func() {
		if touchesNATS(cur) {
			out = append(out, describeWindow(cur, gap))
		}
		cur = nil
	}
	for _, e := range evs {
		if len(cur) > 0 && e.At.Sub(cur[len(cur)-1].At) > gap {
			closeWindow()
		}
		if len(cur) == 0 && !windowStarts[e.Kind] {
			continue
		}
		cur = append(cur, e)
	}
	closeWindow()
	return out
}

func describeWindow(evs []K8sWindowEvent, gap time.Duration) *v1.TimeWindow {
	drained, stopped, versions := map[string]bool{}, map[string]bool{}, map[string]bool{}
	upgrade, gke := false, false
	autoscaler := 0
	for _, e := range evs {
		gke = gke || e.Pool != ""
		switch {
		case e.Kind == "k8s.node.drained":
			drained[e.Node] = true
			if e.Cause == "autoscaler" {
				autoscaler++
			}
		case e.Kind == "k8s.node.upgrade":
			upgrade = true
		case podStops[e.Kind]:
			stopped[e.Server] = true
		}
		if strings.HasPrefix(e.Kind, "k8s.node.") && e.Version != "" {
			versions[e.Version] = true
		}
	}
	label := "Node drain"
	switch {
	case (upgrade || len(versions) > 1) && gke:
		label = "GKE node upgrade"
	case upgrade || len(versions) > 1:
		label = "Node upgrade"
	case len(drained) > 0 && autoscaler == countKind(evs, "k8s.node.drained"):
		label = "Node removed by the autoscaler"
	}
	ids := []string{}
	seen := map[string]bool{}
	unavailable := []string{"continuous_capture"}
	canonical := make([]K8sWindowEvent, len(evs))
	for i, e := range evs {
		canonical[i] = e
		canonical[i].At = e.At.UTC()
		if e.ID == "" {
			if !slices.Contains(unavailable, "supporting_event_identity") {
				unavailable = append(unavailable, "supporting_event_identity")
			}
			continue
		}
		if seen[e.ID] {
			if !slices.Contains(unavailable, "duplicate_supporting_event_identity") {
				unavailable = append(unavailable, "duplicate_supporting_event_identity")
			}
			continue
		}
		seen[e.ID] = true
		ids = append(ids, e.ID)
	}
	raw, _ := json.Marshal(struct {
		Events []K8sWindowEvent
		Gap    int64
	}{canonical, int64(gap)})
	hash := sha256.Sum256(raw)
	return &v1.TimeWindow{Id: hex.EncodeToString(hash[:]), SupportingEventIds: ids, EvidenceTotal: int32(len(evs)), Unavailable: unavailable,
		Derivation: fmt.Sprintf("Starts at a recorded node drain or upgrade and groups eligible node and NATS pod events with successive observed gaps of at most %g minutes. Bounds are the first and last included observations, not an exact maintenance duration or proof of causation.", gap.Minutes()), Start: ts(evs[0].At), End: ts(evs[len(evs)-1].At), Label: label,
		Detail: fmt.Sprintf("%s, %s", Plural(len(drained), "drain"), Plural(len(stopped), "pod restart"))}
}

// touchesNATS reports whether a window holds a drain or a NATS pod event:
// an upgrade of nodes without NATS pods is not shown.
func touchesNATS(evs []K8sWindowEvent) bool {
	for _, e := range evs {
		if e.Kind == "k8s.node.drained" || e.Server != "" {
			return true
		}
	}
	return false
}

func countKind(evs []K8sWindowEvent, kind string) int {
	n := 0
	for _, e := range evs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}
