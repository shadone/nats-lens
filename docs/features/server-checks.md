# Server checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [system-checks.md](system-checks.md), [client-checks.md](client-checks.md), [servers.md](servers.md), [monitoring-snapshots.md](monitoring-snapshots.md)

## What it does

The server checks judge each NATS server on its own: whether it answers, whether its health endpoint and JetStream are ready, whether it restarted, whether its store or memory is filling, whether its clients are slow, stalled, churning or going stale, and whether a TLS certificate of its listeners expires soon. A server finding is keyed by the server's stable name, so it survives the server's restarts. There are 16 server checks, `server-001` to `server-016`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

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

- **Problem:** the server closed or flagged connections that could not read their messages fast enough.
- **Fires when:** the server's slow consumer counter rose within the lookback, counted per server process.
- **Severity:** warning. Critical when a route, gateway or leaf node was a slow consumer, as that drops traffic between servers.
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

## Behavior and rules

- **Keyed by server name.** A server finding belongs to the server's name, so a restart does not start a new finding.
- **Counters are read per process.** The checks that count growth of a server counter (`server-009`, `server-010`, `server-011`, `server-014`, `server-015`) compare values of the same server process, because a restart resets the counters.
- **Planned restarts are quiet.** `server-001` is info for an announced stop, `server-003` is a warning while a server restarts, and `server-002` leaves out a restarting server's catch-up errors.
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

### Tune the store warning

- **Surfaces:** `cli`
- **Given** servers whose stores are planned to run at 80%
- **When** I set `checks.thresholds.server-007: {warn_ratio: 0.85, crit_ratio: 0.95}`
- **Then** `server-007` fires from 85% and is critical from 95%

## Planned

These server checks are not built. Each line says what it would detect and what data it needs.

- **Route down.** A server holds fewer routes than the cluster has peers. Needs: ROUTEZ, which nats-lens stores every collection.
- **Slow route round trip.** The round trip time of a route stays above a threshold. Needs: the route RTT in ROUTEZ, stored today.
- **Route or gateway pending bytes.** Bytes queue up on a route or gateway connection. Needs: pending bytes from ROUTEZ and GATEWAYZ, stored today.
- **Gateway disconnected.** A server has no outbound gateway to a configured remote cluster. Needs: GATEWAYZ, stored today. NATS sends no event for a gateway disconnect, so a drop shorter than one collect interval stays invisible.
- **Leaf node disconnects.** A leaf node that was connected is gone, or reconnects repeatedly. Needs: LEAFZ, stored today. NATS publishes no leaf node disconnect event (its leaf node connect event is for internal use only), so only drops that span a collection can be seen.
- **Connections near `max_connections`.** A server holds most of the connections it allows. Needs: the open connections and `max_connections` from VARZ, stored today.
- **JetStream API error rate.** A server's share of JetStream API calls that fail is high. Needs: the API totals and errors from JSZ, stored today per server and per account.
- **Internal queue backlog.** A server's internal queues hold a growing backlog. Needs: IPQUEUESZ, stored today.
- **JetStream API requests queued.** A server holds many JetStream API requests that wait to be processed, as JSZ's meta `pending` counts them (the routed API requests and info requests queued on the server, not meta group operations). Needs: `meta_pending` from JSZ, stored today per server and collection.
- **CPU saturated.** A server's CPU stays near the number of cores it may use. Needs: CPU, cores and `GOMAXPROCS` from VARZ, stored today.
- **OCSP peer rejected.** A server rejected a peer's certificate by OCSP. Needs: the `$SYS.SERVER.*.OCSP.PEER.CONN.REJECT` events, which nats-lens does not subscribe to yet.

## Not supported / out of scope

- **No per-server thresholds.** A threshold applies to every server.
- **No memory limit check.** nats-server does not report the memory limit of its container, so `server-008` compares servers with each other.
- **No error text for raft write errors.** nats-server does not send it; `server-013` points at the server log.
- **No restart of a server.** nats-lens reports; a person acts.
- **Kubernetes events do not feed the server checks.** A restart is judged from the process start time, not from pod events.
