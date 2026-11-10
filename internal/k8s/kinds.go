// Package k8s watches the Kubernetes objects around the NATS servers and
// records their changes as events: the Events of a namespace, the NATS
// server pods (scheduling, readiness, restarts, eviction, deletion) and
// the nodes they run on (cordon, drain, readiness, upgrade, deletion). It
// only reads from the API server.
package k8s

// Kinds of the events the watcher records. Each kind has one origin, so
// that one change is never stored twice: the Event stream gives the
// kubelet's and controllers' reports (killing, started, unhealthy, a node
// upgrade), the pod and node watches give state changes.
const (
	// From the pod watch.
	KindPodScheduled   = "k8s.pod.scheduled"
	KindPodReady       = "k8s.pod.ready"
	KindPodNotReady    = "k8s.pod.not_ready"
	KindPodRestarted   = "k8s.pod.restarted"
	KindPodEvicted     = "k8s.pod.evicted"
	KindPodTerminating = "k8s.pod.terminating"
	KindPodDeleted     = "k8s.pod.deleted"

	// From Events of pods.
	KindPodKilling          = "k8s.pod.killing"
	KindPodStarted          = "k8s.pod.started"
	KindPodPreempted        = "k8s.pod.preempted"
	KindPodUnhealthy        = "k8s.pod.unhealthy"
	KindPodBackOff          = "k8s.pod.backoff"
	KindPodFailedScheduling = "k8s.pod.failed_scheduling"
	// KindPodOOMKilled is a NATS pod's container restarted after an OOM
	// kill, from the pod watch (the last termination's reason). The Event
	// mapping below covers clusters whose tooling emits OOMKilled Events;
	// kubelets do not (theirs is the Node Event OOMKilling), so for client
	// pods, which are not watched, it is rarely used.
	KindPodOOMKilled = "k8s.pod.oom_killed"

	// From the node watch; k8s.node.drained is derived: a NATS pod
	// evicted or terminated on a cordoned node.
	KindNodeAdded      = "k8s.node.added"
	KindNodeCordoned   = "k8s.node.cordoned"
	KindNodeUncordoned = "k8s.node.uncordoned"
	KindNodeDrained    = "k8s.node.drained"
	KindNodeReady      = "k8s.node.ready"
	KindNodeNotReady   = "k8s.node.not_ready"
	KindNodeDeleted    = "k8s.node.deleted"
	// KindNodeUpgrade is a node's kubelet version changing, or an Event of
	// a node whose reason names an upgrade.
	KindNodeUpgrade = "k8s.node.upgrade"

	// KindEvent is any other Event worth keeping: a warning, or a
	// report about a NATS pod without a kind of its own.
	KindEvent = "k8s.event"

	// Collector events about the watches themselves.
	KindNotWatching = "collector.k8s_not_watching"
	// KindClientPodsCapped is recorded once when the client pod watch
	// sees more pods than it keeps.
	KindClientPodsCapped = "collector.k8s_client_pods_capped"
	KindGap              = "collector.k8s_gap"
)

// podEventKinds maps the reasons of pod Events that have a kind of their
// own. Scheduled, Pulling, Pulled and Created are left out: the pod watch
// records the scheduling, and the others repeat Started.
var podEventKinds = map[string]string{
	"Killing":          KindPodKilling,
	"Started":          KindPodStarted,
	"Evicted":          KindPodEvicted,
	"Preempted":        KindPodPreempted,
	"Preempting":       KindPodPreempted,
	"Unhealthy":        KindPodUnhealthy,
	"BackOff":          KindPodBackOff,
	"CrashLoopBackOff": KindPodBackOff,
	"FailedScheduling": KindPodFailedScheduling,
	"OOMKilled":        KindPodOOMKilled,
	"OOMKilling":       KindPodOOMKilled,
}

// skippedPodReasons are routine pod Events that are not stored even for
// a NATS pod.
var skippedPodReasons = map[string]bool{
	"Scheduled": true, "Pulling": true, "Pulled": true, "Created": true,
}

// clientPodReasons are the Normal Events of pods other than the NATS
// servers that are stored: a client killed or evicted during a drain.
// Warnings are stored for every pod.
var clientPodReasons = map[string]bool{
	"Killing": true, "Evicted": true, "Preempted": true, "Preempting": true,
}
