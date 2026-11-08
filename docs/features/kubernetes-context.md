# Kubernetes context

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [event-capture.md](event-capture.md), [overview-page.md](overview-page.md), [events.md](events.md), [running-nats-lens.md](running-nats-lens.md), [design.md](../design.md)

## What it does

When NATS runs in Kubernetes, many of its troubles start outside NATS: a node is drained for an upgrade, a pod is evicted, a container is killed for memory. With `kubernetes.enabled`, nats-lens watches the Kubernetes API next to NATS and records what happens to the NATS server pods, the nodes they run on, and the warnings of the namespace. It draws these events in a Kubernetes lane on the status timeline and shades node drains and upgrades, so that a quorum loss and the node upgrade that caused it appear on one clock.

## Behavior and rules

- **Read-only, optional.** The watcher only lists and watches. It reads Events (`events.k8s.io/v1`) and the NATS server pods in `kubernetes.namespaces` (by default the namespace nats-lens runs in). With `kubernetes.nodes` (on by default) it also reads the nodes and the node Events in `kubernetes.node_events_namespace` (`default`); that needs a ClusterRole that may read nodes. It needs `events.enabled`, because its events go through the event capture. Inside the cluster it uses the pod's service account; outside, it uses `kubernetes.kubeconfig` and `kubernetes.context`.
- **NATS pods are found by label.** `kubernetes.nats_selector` selects the NATS server pods; the default matches the NATS Helm chart's labels. `kubernetes.server_name` (`{pod}` by default) maps a pod to its NATS server name, so a pod's events appear with that server's own events.
- **What it records about NATS pods.** Scheduled, ready and not ready, restarted (with the last termination reason), OOM killed, evicted, terminating and deleted, and the kubelet's and scheduler's reports about the server container (killing, started, unhealthy, back-off, failed scheduling, preempted). Routine Events (`Scheduled`, `Pulling`, `Pulled`, `Created`) and Events of the config reloader container are not stored.
- **OOM kills of NATS pods.** A NATS pod restart whose last termination is `OOMKilled` is recorded as `k8s.pod.oom_killed`.
- **What it records about nodes.** Added, cordoned and uncordoned (a cordon is `spec.unschedulable` or the cluster autoscaler's removal taint), ready and not ready, upgraded (a changed kubelet version, or a node Event whose reason names an upgrade) and deleted. nats-lens derives `k8s.node.drained` when a NATS pod is evicted, terminated or killed on a cordoned node, once per cordon.
- **Only the nodes that matter.** Node events are kept for nodes that run or ran a NATS pod in the last hour, and for nodes added within 15 minutes of a drain or an upgrade (the surge node). With `kubernetes.pool_nodes` nats-lens also keeps every node of a pool that held a NATS node in the last day; it is off by default, because on an autoscaled cluster that records the autoscaler's churn.
- **Other pods.** Events of other pods in the namespace are kept when they are warnings, kills, evictions or preemptions, without a server name.
- **Client pods.** A pod is a NATS client when its name follows "@" in a connection name (a service that names its connection `service@pod`), or when `kubernetes.client_pods` matches it. The timeline's Kubernetes lane draws the failed scheduling, back-offs (including `CrashLoopBackOff`) and OOM kills of client pods as warnings.
- **Each Event once.** A Kubernetes Event that repeats with a growing count is one row, updated as the count grows, and is not stored again after a relist or a restart of nats-lens.
- **Gaps are recorded.** A list or watch that fails for `kubernetes.gap_after` (30 seconds) is recorded as `collector.k8s_not_watching`, and its recovery as `collector.k8s_gap` with the span. At start, nats-lens records a gap since its last Kubernetes event or collection, and compares the pods and nodes with what it last recorded. A change it missed is recorded then, with "(observed at start)" in the summary.
- **Watch retries.** Failed lists, watch requests, and streams that close within one second retry after 1, 2, 4, 8, 16, 32, then 60 seconds. Retries continue until recovery or shutdown. A successful list or watch request alone does not reset an outage. An event, a bookmark, or a watch that stays open for one second restores watching, including in a quiet namespace. Normal watch timeouts resume from the last resource version; an expired resource version (410) triggers a relist. A stream that closes within one second without an error also triggers a relist before the next watch, as a GKE API server closes a watch from a resource version it no longer has that way instead of answering 410.
- **Bounded failure logs.** Each source logs the first failure, then at most one warning per minute during that outage, even when errors change. Warnings include the next retry delay, total failed attempts, and warnings suppressed since the previous summary. Recovery logs the total attempts and remaining suppressed warnings. Watcher failure logs and gap records use bounded error classifications and status codes. They omit server error bodies and credential-plugin output. Dependency libraries may log failures separately.
- **The Kubernetes lane.** The status timeline shows node cordons, drains, readiness, upgrades and deletions, the NATS pods' stops, starts and readiness, the watcher's gaps, and the client pod warnings. On a timeline for one server, the lane shows only that server's pods.
- **Drain and upgrade windows.** A window opens at a node drain or upgrade and takes in every node or NATS pod event that follows within 15 minutes of the one before. A window without a drain or a NATS pod event is not shown. The window is labelled "Node upgrade" when it holds an upgrade or nodes of different kubelet versions, "GKE node upgrade" when a node in it also carries GKE's `cloud.google.com/gke-nodepool` label, "Node removed by the autoscaler" when every drain in it was the autoscaler's, and "Node drain" otherwise, with the number of drained nodes and NATS pods stopped.

## Scenarios

### Turn the watcher on

- **Surfaces:** `cli`
- **Given** nats-lens runs in the cluster with the service account and roles of the `k8s/` manifests
- **When** I set `kubernetes.enabled: true` and restart it
- **Then** nats-lens records the NATS pod and node events with source `k8s`
- **And** setting `kubernetes.enabled` without `events.enabled` stops the start with the error "kubernetes.enabled needs events.enabled"

### See a node upgrade beside a quorum loss

- **Surfaces:** `ui`
- **Given** a node upgrade drained the nodes of the NATS servers one at a time
- **When** I open the Overview with a range that covers the upgrade
- **Then** the timeline's Kubernetes lane shows the cordons, drains and pod restarts, and a shaded window labelled "Node upgrade" ("GKE node upgrade" on GKE nodes)
- **And** the `stream-001` cells below it show when a stream lost and regained quorum inside that window

### A NATS server pod runs out of memory

- **Surfaces:** `ui`
- **Given** the container of the pod of server `nats-1` is killed for memory and restarts
- **When** I open the Events page with the Kubernetes source
- **Then** I see a `k8s.pod.oom_killed` event named for `nats-1`
- **And** the server's `server-004` "Server restarted" finding fires at the same time

### A client pod is crash-looping

- **Surfaces:** `ui`
- **Given** a service whose connections are named `resize-worker@resize-worker-7d9f` and whose pod is in `CrashLoopBackOff`
- **When** I open the Overview
- **Then** the Kubernetes lane shows that pod's back-off as a warning

### A watch repeatedly fails before credentials recover

- **Surfaces:** `cli`, `ui`
- **Given** a Kubernetes source repeatedly rejects watch requests or closes streams within one second
- **When** the failures continue beyond `kubernetes.gap_after`
- **Then** nats-lens records one `collector.k8s_not_watching` event and increases retry delays up to one minute
- **And** that source emits at most one failure warning per minute after its first warning, with retry and suppression counts
- **When** a retry receives an event or bookmark, or stays open for one second without events
- **Then** nats-lens records the outage span as `collector.k8s_gap`, logs recovery, and resets the retry delay
- **And** a successful list or relist alone does not end the watch outage

### The server no longer has a quiet source's resource version

- **Surfaces:** `cli`
- **Given** a quiet source, such as the node Events in `default`, has seen no event or bookmark for hours, so the API server no longer has the resource version it watches from
- **When** the API server closes the next watch from that version at once without an error, as a GKE API server does in place of a 410
- **Then** nats-lens counts the closure as a failure, waits the retry delay, lists the source again and watches from the list's resource version
- **And** when that watch stays open for one second, watching is restored; an episode shorter than `kubernetes.gap_after` records no gap

## Not supported / out of scope

- **No checks read Kubernetes events.** The restart rules of the checks rely on process start times and on lame duck and shutdown announcements, not on pod events.
- **OOM kills of client pods are rarely seen.** Client pods are not watched, so an OOM kill of a client pod reaches nats-lens only through an Event with the reason `OOMKilled` or `OOMKilling`. Kubelets send `OOMKilling` as a node Event, not for the pod, so in practice this case is untested on a real cluster.
- **No other workloads.** nats-lens does not watch Deployments, StatefulSets or PodDisruptionBudgets.
- **No writes.** The watcher never cordons, evicts or deletes anything.
- **Large lists are cut.** Lists are paged by 500 objects, at most 20 pages; objects beyond that are left out, with a warning in the log.
