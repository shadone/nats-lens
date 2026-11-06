# Server checks

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [system-checks.md](system-checks.md), [client-checks.md](client-checks.md), [servers.md](servers.md), [monitoring-snapshots.md](monitoring-snapshots.md)

## What it does

The server checks judge each NATS server on its own: whether it answers, whether its health endpoint and JetStream are ready, whether it restarted, whether its store or memory is filling, whether its clients are slow, stalled, churning or going stale or fill its connection limit, whether a TLS certificate of its listeners expires soon, whether its routes and gateways are all there and keep up, and whether its CPU, internal queues or JetStream API are overloaded, and whether it rejected an OCSP peer connection. A server finding is keyed by the server's stable name, so it survives the server's restarts. There are 26 checks in the server group, `server-001` to `server-026`; the leaf-site check reports on the system. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

## The checks

### `server-001` Server not reporting

- **Problem:** a server that nats-lens expects did not answer any monitoring request in this collection.
- **Fires when:** a server that answered within `server_forget_after` (1 hour), or that another server still lists as a route or JetStream meta peer, answers nothing.
- **Severity:** warning. Info while the server announced lame duck mode or shutdown before it went away, within `checks.restart_window` (a planned restart). Critical when at least half of the expected servers are missing, as the JetStream meta group then has no quorum; the detail then counts the replicated streams and consumers left without a quorum.
- **Thresholds:** none of its own; it uses `server_forget_after`.
- **Left out:** a server that has been away longer than `server_forget_after` and that no peer lists any more.

### `server-002` Healthz not ok

- **Problem:** the server's HEALTHZ endpoint, asked with details, does not report ok.
- **Fires when:** HEALTHZ reports an error in this collection.
- **Severity:** critical.
- **Thresholds:** none.
- **Left out:** errors that only concern JetStream readiness, which are `server-003`'s. For a restarting server, its stream and consumer errors while it catches up, and a failure without detail.

### `server-003` JetStream not ready

- **Problem:** JetStream on the server is not ready to serve: its meta layer is still recovering, or it is not current with the meta leader. Or JetStream is configured on the server but reported disabled.
- **Fires when:** HEALTHZ reports a JetStream error in every collection for longer than `min_duration`, or JSZ reports JetStream disabled on a server configured for it.
- **Severity:** critical. Warning while the server restarted within `checks.restart_window`. JetStream disabled on a server configured for it is critical at once.
- **Thresholds:** `min_duration` 3m.
- **Left out:** a crash loop whose restarts come faster than `min_duration`; `server-004` reports it as repeated restarts.

### `server-004` Server restarted

- **Problem:** the server process restarted. The detail says whether it came with an upgrade (another version), a configuration change (another configuration digest), or neither.
- **Fires when:** a process of the server started within the lookback and replaced an earlier process of the same name. The finding stays firing for the lookback after the restart.
- **Severity:** info. Critical when the server restarted more than `max_restarts` times within the lookback without an upgrade or a configuration change.
- **Thresholds:** lookback 1h, `max_restarts` 2.
- **Left out:** the first process nats-lens sees of a server, as it replaced nothing nats-lens knows of.

### `server-005` Version differs from the majority

- **Problem:** the server runs another nats-server version than most servers. Expected during a rolling upgrade; a cluster that stays mixed runs replicas with different behaviour.
- **Fires when:** the answering servers run more than one version, for each server whose version is not the most common one.
- **Severity:** info.
- **Thresholds:** none.
- **Left out:** servers that did not report a version.

### `server-006` Configuration reloaded

- **Problem:** the server reloaded its configuration without restarting.
- **Fires when:** the server's configuration load time is later than its process start, within the lookback.
- **Severity:** info.
- **Thresholds:** lookback 1h.
- **Left out:** the configuration load at process start; that is a restart (`server-004`).

### `server-007` JetStream store usage high

- **Problem:** the server's JetStream file store or memory store is close to its configured limit (`max_file_store`, `max_memory_store`). At the limit, the server refuses new messages for the streams placed on it.
- **Fires when:** the fuller of the two stores holds at least `warn_ratio` of its limit.
- **Severity:** warning. Critical at or above `crit_ratio`.
- **Thresholds:** `warn_ratio` 0.75, `crit_ratio` 0.9.
- **Left out:** a store without a configured limit.

### `server-008` Memory far above the other servers

- **Problem:** the server uses far more memory than its peers. nats-server does not report its memory limit, so the check compares servers with each other instead.
- **Fires when:** the server's resident memory is above `multiplier` times the median of the other servers, and at least `min_bytes`.
- **Severity:** warning.
- **Thresholds:** `multiplier` 1.5, `min_bytes` 512MiB.
- **Left out:** a server without peers that report memory.

### `server-009` Slow consumers

- **Problem:** the server closed or flagged connections that could not read their messages fast enough. The aggregate `slow_consumers` includes clients, routes, gateways and leaf connections. Retries may increment it without a disconnect; it does not measure JetStream consumer backlog.
- **Fires when:** the server's slow consumer counter rose within the lookback, counted per server process.
- **Severity:** warning. Critical when the route, gateway or leaf slow consumer counters grow, indicating pressure on traffic between servers.
- **Thresholds:** lookback 15m.
- **Left out:** growth across a restart: a new process starts the count again. `client-003` names the clients.

### `server-010` Stalled clients

- **Problem:** a publisher was paused because a subscriber's outbound buffer was full. A stall is the step before a slow consumer disconnect.
- **Fires when:** the server's stalled client counter rose within the lookback, counted per server process.
- **Severity:** warning.
- **Thresholds:** lookback 15m.
- **Left out:** growth across a restart.

### `server-011` Connection churn

- **Problem:** clients reconnect over and over, or something opens a connection per request.
- **Fires when:** within the lookback, the server accepted at least `min_new` new connections and at least `churn_ratio` times the connections it holds, in each of the last `min_epochs` collections. A firing finding stays firing while the count is at or above `resolve_ratio` of both thresholds, and resolves once it falls below either, so churn that hovers at the threshold does not flap. The detail names the three clients that connected most, from the `client.connect` and `client.churn` events.
- **Severity:** warning.
- **Thresholds:** lookback 10m, `min_new` 50, `churn_ratio` 0.5, `min_epochs` 2, `resolve_ratio` 0.8.
- **Left out:** tooling connections, from the client list in the detail only. The count itself comes from the server's connection counter and includes them.

- **Missed evaluations:** after an error or a collection skipped for budget, hysteresis uses this check's last successful evaluation. Disabling the check resolves its findings. Re-enabling needs the full thresholds in each of the last `min_epochs` collections.

### `server-012` Raft groups catching up for too long

- **Problem:** one or more raft groups on the server are still catching up with their leader. A replica that is catching up does not count towards its group's quorum.
- **Fires when:** RAFTZ has reported a group as catching up in every collection for longer than `max_catchup`. The detail lists up to five of them.
- **Severity:** warning.
- **Thresholds:** `max_catchup` 5m.
- **Left out:** groups that catch up for less than `max_catchup`, as after a normal restart.

### `server-013` Raft write error

- **Problem:** a raft group on the server stopped on a write-ahead log error, so the stream or consumer has one replica fewer until the server restarts. nats-server does not report the error text; it is only in the server log.
- **Fires when:** RAFTZ reports a write-ahead log error for any group on the server.
- **Severity:** critical.
- **Thresholds:** none.
- **Left out:** nothing.

### `server-014` JetStream store full soon

- **Problem:** at its current growth, the server's JetStream file store reaches `max_file_store` soon.
- **Fires when:** the days until full, at the rate of the lookback, are below `warn_days`. The rate is the smaller of the slopes of the window's two halves, and both must be positive, so a one-off step such as a restore is not read as steady growth.
- **Severity:** warning. Critical below `crit_days`.
- **Thresholds:** lookback 6h, `warn_days` 14, `crit_days` 3, `min_span` 1h (the least span of data needed for a rate).
- **Left out:** a server without a file store limit, and growth across a restart: a new process starts a new window.

### `server-015` Stale connections

- **Problem:** the server closed connections because the client answered none of its pings (`ping_interval`, by default 2 minutes, times `ping_max`, by default 2). The client was gone without closing, or too busy to answer.
- **Fires when:** the server's stale connection counter rose within the lookback, counted per server process.
- **Severity:** info.
- **Thresholds:** lookback 15m.
- **Left out:** growth across a restart.

### `server-016` TLS certificate expires soon

- **Problem:** the TLS certificate of one of the server's listeners (client, cluster, gateway, leaf node, websocket, MQTT) expires soon or has expired. Once it expires, clients, routes, gateways or leaf nodes that verify it can no longer connect through that listener.
- **Fires when:** a certificate VARZ reports expires within `warn_within` of the collection, or has expired. One finding per server; the detail names every listener whose certificate expires within `warn_within`, soonest first, such as "nats-0: client certificate expires 2026-10-29 09:00 UTC, in 12 days."
- **Severity:** warning. Critical within `crit_within`, or once expired.
- **Thresholds:** `warn_within` 14d, `crit_within` 3d.
- **Related settings:** the expiry of every listener certificate the server reports, under its VARZ field name (`tls_cert_not_after`, `cluster.tls_cert_not_after`, and so on), read from that server.
- **Left out:** a listener without TLS, or whose certificate comes from a certificate store, since VARZ reports no expiry for it. A server that did not answer VARZ in the collection keeps the certificates of its newest answer within 24 hours, as on the Accounts page, and its detail says when VARZ reported them; an older answer is not read.

### `server-017` Route missing

- **Problem:** the server holds no route to another server of its cluster, so messages and interest do not pass directly between the two, and subscribers on one miss what publishers on the other send.
- **Fires when:** the server's ROUTEZ lists no route to a server of the same cluster, by name or server ID, while both answered, in each of the last `min_epochs` collections. A firing finding stays while the route is missing in the newest collection and resolves in the first collection that lists it again. One finding per server; the detail names every peer without a route, such as "nats-0 has no route to nats-2, while both answered."
- **Severity:** warning. Critical when the server has no route to any other server of its cluster that answered. In a cluster of two servers each has one peer, so any lost route is critical: the two servers then pass nothing to each other (proposed, not yet confirmed).
- **Thresholds:** `min_epochs` 3 (proposed, not yet confirmed).
- **Left out:** a peer that did not answer, which is `server-001`'s, and a server whose ROUTEZ did not answer, as its list is not whole; either breaks the run of collections. Servers that report no cluster. A restarted peer counts by name, so a route to its new process is a route. The number of pooled routes per peer is not checked: a server with one route of a pool to a peer has a route.

### `server-018` Bytes queued on a route or gateway

- **Problem:** bytes wait to be written on one of the server's route or gateway connections: the remote end or the network does not take the traffic as fast as it comes. nats-server holds the backlog in memory without a cap: unlike a client connection, a route or gateway is not closed at `max_pending`, which this check uses only as the scale (proposed, not yet confirmed), so the value can pass 1. Messages on the link arrive late and the server's memory grows. A write that takes longer than the write deadline counts the link as a slow consumer (`server-009`); by default the server retries it and closes the link only when nothing could be written; with `write_timeout: close` in the cluster or gateway block it closes the link at once.
- **Fires when:** one route (by remote server, the largest of its pooled routes) or gateway connection (by direction and remote cluster, named "outbound gateway to eu" or "inbound gateway from eu") holds at least `warn_ratio` of the server's `max_pending` in each of the last `min_epochs` collections. A firing finding stays while a connection holds at least `resolve_ratio` of that level. The detail names up to three connections, the fullest first, such as "nats-0: 16.0 MiB on the route to nats-2 (25%) waiting to be written, of max_pending 64.0 MiB."
- **Severity:** warning. Critical at or above `crit_ratio` of `max_pending`.
- **Thresholds:** `warn_ratio` 0.25, `crit_ratio` 0.5, `min_epochs` 3, `resolve_ratio` 0.5 (proposed, not yet confirmed).
- **Related settings:** `max_pending`, read from the server.
- **Left out:** a server process that never reported `max_pending` in VARZ; a collection whose VARZ failed keeps the process's newest value. A collection in which the server did not report the connection breaks the run. Leaf node connections, since LEAFZ reports no pending bytes. Client connections are `client-002`'s.

### `server-019` Gateway missing

- **Problem:** the server has no outbound gateway to a remote cluster of the supercluster, so its messages for that cluster wait until the gateway comes back.
- **Fires when:** the server's cluster had a gateway to or from a remote cluster within the lookback, and the server has no outbound gateway to it, in each of the last `min_epochs` collections in which it answered GATEWAYZ. A firing finding stays while the gateway is missing in the newest collection. The detail names the remote clusters and how many servers of the cluster have a gateway to each, such as "us-0 has no outbound gateway to cluster eu (1 of 2 servers of us have one)."
- **Severity:** warning. Critical when no server of the cluster that answered GATEWAYZ has an outbound gateway to that cluster, while the remote cluster still connects in or its servers answer.
- **Thresholds:** lookback 24h, as the Topology page keeps a pair of clusters for a day; `min_epochs` 2 (both proposed, not yet confirmed).
- **Left out:** a server whose GATEWAYZ did not answer, which also breaks the run, and a server that is restarting (within `checks.restart_window`), as its gateways connect some time after it answers. A remote cluster removed: when, in each of the last `min_epochs` collections, no server of the cluster has an inbound gateway from it while they answer GATEWAYZ, and none of the remote cluster's own servers answers, it is no longer judged, so a cluster removed on purpose fires nothing (proposed, not yet confirmed). A remote cluster whose servers still answer stays judged, so a gateway cut in both directions by a firewall, a closed port or a load balancer keeps firing. A remote cluster nats-lens does not see itself, as one watched by another collector, and cut in both directions is taken as removed: only `server-001` may then show it, if nats-lens ever saw its servers. GATEWAYZ lists only connected gateways, not the configured ones, so a remote cluster never connected within the lookback is not known. Inbound gateways are not judged themselves. A drop and reconnect between two collections, which the Topology page counts as a drop when the connection ID changed.

### `server-020` Connections near max_connections

- **Problem:** the server holds most of the client connections its `max_connections` allows. At the limit it refuses every new client connection with "maximum connections exceeded".
- **Fires when:** the server's client connections are at least `warn_ratio` of its `max_connections` in each of the last `min_epochs` collections. A firing finding stays while the share is at least `resolve_ratio` of `warn_ratio`.
- **Severity:** warning. Critical at or above `crit_ratio`.
- **Thresholds:** `warn_ratio` 0.8, `crit_ratio` 0.95, `min_epochs` 2, `resolve_ratio` 0.9 (proposed, not yet confirmed). `max_connections` can be reloaded without a restart.
- **Related settings:** `max_connections`, read from the server.
- **Left out:** a server without a limit (`max_connections` 0), and a process that never reported one in VARZ; a collection whose VARZ failed keeps the process's newest value. An account's connection limit is `account-001`'s.

### `server-021` JetStream API errors high

- **Problem:** the server answers a large share of the JetStream API requests it serves with an error.
- **Fires when:** within the lookback, the server answered at least `min_errors` requests with an error, and at least `error_ratio` of all its requests, in each of the last `min_epochs` collections, counted per server process. A firing finding stays while the errors are at least `resolve_ratio` of both thresholds. The detail reads like "nats-0 answered 120 of 240 JetStream API requests (50%) with an error in the last 15m."
- **Severity:** warning.
- **Thresholds:** lookback 15m, `min_errors` 100, `error_ratio` 0.5, `min_epochs` 2, `resolve_ratio` 0.8 (proposed, not yet confirmed).
- **Left out:** a server whose JetStream is not ready, which `server-003` reports for the same server and which makes every request fail; growth across a restart. nats-server counts every error answer, such as a consumer info before the consumer is created or a message get of a message that is not there; a direct get, as KV buckets use, is not counted. JSZ does not say which errors they were.

### `server-022` Internal queue backlog

- **Problem:** one of the server's internal queues, such as a stream's, a raft group's or the JetStream API queue, stays full: the server falls behind on that work.
- **Fires when:** one queue of IPQUEUESZ holds at least `min_pending` items in each of the last `min_epochs` collections in which the server answered IPQUEUESZ. A firing finding stays while a queue holds at least `resolve_ratio` of `min_pending`. The detail names up to three queues, the fullest first, and how many items each held in the oldest of the collections when that differs, such as `nats-0 is behind on internal queues: "JS API Queue" holds 3000 items, up from 1000.`
- **Severity:** warning.
- **Thresholds:** `min_pending` 10000, `min_epochs` 3, `resolve_ratio` 0.5 (proposed, not yet confirmed). Only waiting items count, not those being processed (`in_progress`). Measured on 2026-11-05 against the local three-server dev cluster (nats-server 2.15.1, loopback, one laptop): under 5,000 messages a second into an R3 file stream no queue held more than 16 items; at 167,000 messages a second, with every publish acknowledged, the stream's message queue peaked at 890 waiting items (1,498 in progress) in the collections and 681 in samples every 2 seconds. 10000 is about ten times that peak; a larger server under more load may queue more.
- **Left out:** a collection whose IPQUEUESZ did not answer, which breaks the run rather than counting as empty; a queue IPQUEUESZ leaves out held nothing. A process that is restarting (within `checks.restart_window`), whose queues fill while it catches up. A server that recovers without a restart, such as a follower stuck catching up, is judged (proposed, not yet confirmed).

### `server-023` CPU saturated

- **Problem:** the server process uses nearly all the CPU it may use, so it answers clients, routes and raft heartbeats late.
- **Fires when:** the process used at least `busy_ratio` of its usable cores, the smaller of the cores it sees and its GOMAXPROCS, in at least `busy_share` of its collections within `min_duration`, and at least `min_samples` of them, so one calm sample does not reset it. The process must have answered from the start of `min_duration` to now, with no gap of more than three collect intervals (the median gap between collections). A firing finding stays while `busy_share` of the collections are at least `resolve_ratio` of that level. The detail reads like "nats-0 used 3.6 of 4 usable cores (90%); 11 of the 11 collections in the last 5 minutes were at least 90% busy (8 cores, GOMAXPROCS 4)."
- **Severity:** warning.
- **Thresholds:** `busy_ratio` 0.9, `min_duration` 5m, `busy_share` 0.8, `min_samples` 5, `resolve_ratio` 0.8 (proposed, not yet confirmed).
- **Left out:** a server that reports neither its cores nor GOMAXPROCS. A collection without a CPU value is skipped, not read as calm. A restarting process (within `checks.restart_window`) is not judged, and a new process needs `min_duration` of its own collections. A server that stays between about 80% and 90% of its usable cores does not fire, by design: it runs hot but is not saturated. Once a finding fires, it holds down to 72% (proposed, not yet confirmed). A container CPU limit below GOMAXPROCS: the process is throttled before it reaches the level, so the check fires late or not at all. nats-server built with Go 1.25 or later sets GOMAXPROCS to the limit.

### `server-024` OCSP peer rejected

- **Fires when:** the server reports a final TLS peer connection rejection by Online Certificate Status Protocol (OCSP) verification in the last 15 minutes. A chain-link status event alone does not count. The event can describe an inbound client or a leaf connection; it does not establish that the certificate was revoked. Responder failures can also prevent verification.

- **Evidence:** the finding links to the latest rejection event and counts distinct event IDs per server process. Both the event time and receipt time must fall in the lookback. The finding resolves once the window is empty, without a counter or a second collection of failures. It keeps the event's captured server name across a restart or a missing snapshot, and uses the server ID when the event has no name. Event-only servers can raise findings too. Without live event capture, missed rejections cannot be recovered.
- **Severity:** warning.
- **Thresholds:** lookback 15m.
- **Left out:** chain-link status events without a final peer rejection; rejections outside the event-time or receipt-time lookback.

- **Next steps:** read the emitting server's TLS and OCSP logs and identify the peer by the certificate fingerprint retained in the event. The failing certificate can be another link in the peer's chain. Check revocation, validity, responder reachability, and clocks before replacing certificates or restoring access. Keep verification enabled.

### `server-025` Slow routes

- **Fires when:** a route pair touching the server is slow by [Topology's rule](topology.md) in two eligible collections. Both endpoints receive a warning; the finding does not identify which endpoint is responsible. An ordinary peer of a slow server can therefore have this finding too.
- **Evidence:** the peer names, each pair's current round trip, the two collection numbers, and a value counting the slow pairs touching the server.
- **Eligibility:** the previous collection is the newest earlier one with stored route rows, within three collect intervals. Each collection is judged independently, by cluster. An unknown interval, an absent pair or an unreported/zero round trip cannot prove sustained slowness. A missing ROUTEZ answer contributes no observations; another server's report can still measure the pair.
- **Identity:** pairs use stable server names, across pooled and pinned connections and process replacements. A restart does not manufacture an RTT observation; two actual measurements can establish slowness even across a restart. Route IDs and process counters are not compared.
- **Tuning:** disabled checks, remediation extensions and mutes work as for other checks. The classifier's constants are shared with Topology and have no independent thresholds.
- **Remediation:** compare the named peers' latency, packet loss, CPU, scheduling and network paths with the other servers. A warning establishes a slow pair, not its cause.

The warning severity, attribution to both endpoints, evidence and name-based identity are proposed, not yet confirmed.

### `server-026` Leaf node disconnects or repeated reconnects

- **Fires when:** a previously observed leaf site is absent for two consecutive fully observed collections, or has at least three observed connection drops in the last hour. The last sighting must be within 24 hours. Sites never observed cannot fire, even if an operator expects them.
- **Identity and attribution:** one warning belongs to the system, with each affected site name and account in its evidence. An unnamed site uses its IP. The identity matches [Topology](topology.md) across hubs and clusters. Identical names on different accounts remain separate; identical names on the same account describe one observed identity, even if several remote processes share it. The finding does not blame a hub or identify a remote process.
- **Eligible observations:** every expected server must be observed and have exactly one successful LEAFZ answer by server name in both collections. The expected names and reporting process IDs must remain the same. A restart, duplicate hub name, missing answer, timeout, or gap longer than three collect intervals invalidates the pair. The interval must be known. Sustained absence requires uninterrupted eligible pairs from the last sighting.
- **Reconnects:** Topology's connection-drop rule is shared with the check. The page still counts hub moves; the check excludes a drop when the identity appears on another hub in the next collection. Repeated-drop warnings require at most one connection for that identity per collection throughout the 24-hour history, so redundant connections and duplicate rows cannot inflate the count. Uplinks and identities with neither a name nor an IP do not fire.
- **Recovery:** a reconnect clears the absence condition. A repeated-drop warning clears when fewer than three eligible drops remain in the rolling hour. These windows provide the persistence; there is no separate lower reconnect threshold. A collection without an eligible latest pair retains evidence from this check’s latest successful execution, including across failed, skipped or unapplied executions. Disabling the check clears that retained state. Current or future mutable finding state cannot supply evidence to a historical evaluation. This also applies when an identity remains absent across uncertain coverage. Each retained identity expires when its original last observation becomes older than 24 hours; repeated uncertain evaluations never renew that time. Fresh confirmed evidence for another identity joins the warning without extending older evidence. Expiry ends the warning for lack of recent evidence; it does not prove recovery. With no previous warning, uncertain data creates none. History older than 24 hours cannot establish continued absence.
- **Evidence:** the affected identities, observed drops in one hour, last observation time and collection, and a value counting affected identities. There is no causal NATS disconnect event: only changes spanning collections or changed connection IDs are observed. An outage and return between collections with the same ID cannot be detected.
- **Remediation:** inspect Topology and the remote site's process and network logs, and compare the hub logs. Confirm whether an outage is expected before changing configuration. Disabled checks, remediation extensions and system mutes apply as usual.

The system identity, warning severity, two-collection absence, three-drop hour, 24-hour sighting window, per-identity evidence expiry across missed evaluations and conservative eligibility rules are proposed, not yet confirmed.

## Behavior and rules

- **Keyed by server name.** Except for the system-level `server-026`, a server finding belongs to the server's name, so a restart does not start a new finding.
- **Counters are read per process.** The checks that count growth of a server counter (`server-009`, `server-010`, `server-011`, `server-014`, `server-015`, `server-021`) compare values of the same server process, because a restart resets the counters.
- **Planned restarts are quiet.** `server-001` is info for an announced stop, `server-003` is a warning while a server restarts, and `server-002` leaves out a restarting server's catch-up errors.
- **A server that did not answer is not judged.** The link, queue and limit checks (`server-017` to `server-023`) read only what a server reported: a peer or server that did not answer is `server-001`'s, and a collection in which the endpoint a check reads did not answer breaks its run of collections instead of counting as healthy or as failing. `server-018` and `server-020` keep the process's newest `max_pending` or `max_connections` when VARZ fails, as the limit changes only with the configuration.
- **Firing findings hold.** For `server-017` to `server-023`, a firing finding stays on a lower level (`resolve_ratio`), or for as long as its condition holds in the newest collection, even before the condition has lasted long enough to fire again, as with `server-011`, so a value that hovers at its threshold does not fire and resolve on every collection.
- **The remediation names the next step.** Where another check covers the cause or the clients, the remediation points at it, such as `client-003` for the clients behind `server-009`.

## Scenarios

### A server crashes

- **Given** three servers, and `nats-2` stops without announcing lame duck mode
- **When** nats-lens collects
- **Then** `server-001` "Server not reporting" fires as a warning for `nats-2`, with the time it last answered

### A server is restarted on purpose

- **Given** `nats-2` enters lame duck mode and then stops for an upgrade
- **When** nats-lens collects while it is away and after it is back
- **Then** `server-001` is info while it is away, and `server-004` "Server restarted" is info once it is back, saying "for an upgrade from" the old version "to" the new one

### A server restarts in a loop

- **Given** a server that restarts four times within an hour with the same version and configuration
- **When** nats-lens collects
- **Then** `server-004` is critical, and its detail reads like "restarted at … with version … unchanged; 4 restarts in the last 1h"
- **And** the detail counts the restarts without an upgrade or configuration change separately only when some restarts came with one

### A disk fills

- **Given** a server whose file store grew steadily over the last six hours
- **When** the forecast says it is full in two days
- **Then** `server-014` "JetStream store full soon" is critical, with the growth per day and the time left ("full in 2 days", or in hours when under a day) in the detail

### A certificate is about to expire

- **Given** `nats-0`'s cluster certificate expires in 2 days and its client certificate in 12 days
- **When** nats-lens collects
- **Then** `server-016` "TLS certificate expires soon" is critical for `nats-0`, naming the cluster certificate first and then the client certificate, each with its date and the days left

### A route between two servers is gone

- **Given** three servers of cluster `lens` that all answer, and `nats-0`'s ROUTEZ has listed no route to `nats-2` for the last three collections
- **When** nats-lens collects
- **Then** `server-017` "Route missing" fires as a warning for `nats-0`, reading "nats-0 has no route to nats-2, while both answered."
- **And** it resolves in the first collection in which `nats-0` lists the route again

### A remote cluster is unreachable

- **Given** a supercluster whose clusters `us` and `eu` were joined by gateways an hour ago, and no server of `us` has an outbound gateway to `eu` in the last two collections, while `eu` still connects in to `us`
- **When** nats-lens collects
- **Then** `server-019` "Gateway missing" is critical for every server of `us`, each detail ending "(no server of us has one)"

### A remote cluster is removed on purpose

- **Given** cluster `eu` is taken out of the supercluster, so no gateway joins `us` and `eu` in either direction, its servers no longer answer, and the servers of `us` do
- **When** nats-lens collects twice
- **Then** `server-019` fires nothing for the servers of `us`

### A gateway is cut in both directions

- **Given** a firewall rule cuts the gateways between `us` and `eu` in both directions, and the servers of both clusters still answer
- **When** nats-lens collects twice
- **Then** `server-019` is critical for every server of both clusters

### A busy server

- **Given** `nats-0` runs with GOMAXPROCS 4 and has used at least 3.6 cores in 10 of the 11 collections of the last five minutes
- **When** nats-lens collects
- **Then** `server-023` "CPU saturated" fires as a warning for `nats-0`, with the cores it used and how many collections were busy
- **And** it stays firing while 80% of the collections of five minutes are at or above 72% of the four cores, and resolves below that

### Connection churn remains near the thresholds after a skipped check

- **Given** `server-011` fires for `nats-0`, then cannot run for two collections
- **When** the next successful evaluation counts 45 accepted connections against 10 open connections, with the default thresholds
- **Then** the finding stays firing, including after nats-lens restarts
- **And** another server that never fired needs the full thresholds to fire
- **When** the accepted connections leave the lookback
- **Then** the finding resolves

### Tune the store warning

- **Surfaces:** `cli`
- **Given** servers whose stores are planned to run at 80%
- **When** I set `checks.thresholds.server-007: {warn_ratio: 0.85, crit_ratio: 0.95}`
- **Then** `server-007` fires from 85% and is critical from 95%

### Find sustained slow routes

- **Surfaces:** `ui`, `api`, `cli`
- **Given** a cluster of five servers with one route pair at 4 ms and all other pairs at 0.4 ms
- **When** two eligible collections report those measurements
- **Then** Topology marks the pair slow and `server-025` warns on both endpoint servers, naming the peer and round trip
- **And** the other three servers have no slow-route finding
- **And** one spike, an even split between zones, two servers alone, or an unknown collection interval fires nothing

### A leaf site disconnects or reconnects repeatedly

- **Given** all expected hubs answer LEAFZ in consecutive collections, and `pickup-0417` on `APP` was observed connected
- **When** it is absent for two collections, each within three collect intervals of the preceding one
- **Then** `server-026` warns on the system, naming the site, account and last observation without blaming its last hub
- **And** moving to another hub without an absent collection does not trigger the absence or repeated-drop condition
- **And** three eligible connection changes on the same hub within one hour trigger the repeated-drop condition
- **And** a site never observed, an uplink, or a pair with a failed LEAFZ answer creates no warning

## Planned

These server checks are not built. Each line says what it would detect and what it needs.

- **JetStream API requests queued.** A server holds many JetStream API requests that wait to be processed, as JSZ's meta `pending` counts them (the routed API requests and info requests queued on the server, not meta group operations). Needs: `meta_pending` from JSZ, stored today per server and collection.

### An OCSP peer cannot connect

- **Given** live system event capture records a final OCSP peer connection rejection from `nats-0`
- **When** the next collection runs within 15 minutes of the event and its receipt
- **Then** `server-024` fires as a warning for `nats-0` and links to the event
- **And** a server with only a chain-link status event has no such finding
- **And** the finding resolves after the last rejection leaves the window, even if the server stopped reporting

## Not supported / out of scope

- **No per-server thresholds.** A threshold applies to every server.
- **No memory limit check.** nats-server does not report the memory limit of its container, so `server-008` compares servers with each other.
- **No error text for raft write errors.** nats-server does not send it; `server-013` points at the server log.
- **No restart of a server.** nats-lens reports; a person acts.
- **Kubernetes events do not feed the server checks.** A restart is judged from the process start time, not from pod events.
