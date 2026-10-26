# Client checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [server-checks.md](server-checks.md), [connections.md](connections.md), [event-capture.md](event-capture.md), [applications.md](applications.md)

## What it does

The client checks judge the applications connected to NATS: whether their round trip time is high, whether the server queues bytes they cannot read fast enough, whether they were disconnected as slow consumers, whether they keep reconnecting, and whether they open connections that last only seconds. A client finding is keyed by connection name rather than by connection, so it survives reconnects and covers every instance that uses the name. Clients without a name are grouped as `(unnamed)`. There are 5 client checks, `client-001` to `client-005`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

## The checks

### `client-001` High client round trip time

- **Problem:** a client answers the server's pings slowly. Inside one Kubernetes cluster the round trip should be a millisecond or two; a high value usually means a busy client (CPU throttled, a blocked event loop), not the network.
- **Fires when:** one client of the service has had a round trip time above `max_rtt` in each of the last `min_epochs` collections. A client is one server, pod (the part of the connection name after "@") and address, or one server and connection ID without a pod. A pod whose address the server sees changing between collections, as behind a NAT or proxy, counts as a new client, which breaks the run.
- **Severity:** warning.
- **Thresholds:** `max_rtt` 100ms, `min_epochs` 2.
- **Keyed by:** the service, the connection name before any "@". Naming each pod's connection `service@pod` therefore gives one finding per service, and a mute covers every pod of it.
- **Left out:** tooling connections, unless `checks.include_tooling` is set.

### `client-002` High pending bytes

- **Problem:** the server holds bytes it could not yet write to a client: the client reads slower than its messages arrive. When the queue reaches the server's `max_pending` (64 MiB by default) or a write takes longer than `write_deadline`, the server disconnects it as a slow consumer.
- **Fires when:** one or more connections with this name have more than `max_pending` bytes queued in this collection. The detail names the server and connection ID with the most.
- **Severity:** warning.
- **Thresholds:** `max_pending` 4MiB.
- **Keyed by:** the whole connection name.
- **Left out:** tooling connections, unless `checks.include_tooling` is set.

### `client-003` Slow consumer disconnects

- **Problem:** the servers disconnected connections with this name as slow consumers, dropping every message queued for them.
- **Fires when:** `client.disconnect` events with a slow consumer reason arrived within the lookback. The detail names the servers, the time of the last one and its reason, which says whether the write deadline or the pending limit was hit.
- **Severity:** warning.
- **Thresholds:** lookback 30m.
- **Keyed by:** the whole connection name.
- **Left out:** tooling connections, unless `checks.include_tooling` is set.

### `client-004` Client reconnecting repeatedly

- **Problem:** connections with this name lose their connection and come back, over and over.
- **Fires when:** within the lookback there were at least `min_reconnects` reconnects of the name. A reconnect is a `client.disconnect` whose reason is neither "Client Closed" nor "Server Shutdown" (which lame duck mode uses too), paired with a `client.connect` of the same name, in either order and from any host. The count is the smaller of the two numbers. Connects folded into a `client.churn` event count as connects.
- **Severity:** warning.
- **Thresholds:** lookback 10m, `min_reconnects` 3.
- **Keyed by:** the whole connection name, so instances that share a name are merged.
- **Left out:** connects alone (a service that scales out opens one connection per instance under one name), disconnects the client asked for, disconnects caused by a server stopping, and tooling connections unless `checks.include_tooling` is set.

### `client-005` Short-lived connections

- **Problem:** connections with this name are opened and closed by the client itself within seconds, again and again. Each one costs a TCP and TLS handshake, authentication, and a connect and a disconnect advisory on the server. A client that opens a connection per request, per reconcile loop or per job step should keep one long-lived connection instead. A process that crashes, or exits after each unit of work, and is restarted looks the same when it closes its connection cleanly, so check its pod restarts too.
- **Fires when:** within the lookback, the connects folded into `client.churn` events of the name add up to at least `min_short_connections`. A summary is written after its minute ends, so the check lags the connects by about a minute. Each such connect had its disconnect, with the reason "Client Closed", within `events.churn.pair_within`. The detail gives the count, the rate per minute, the client hosts (five, then how many more) and the servers, for example "120 connections closed by the client within seconds in the last 10m (about 12 a minute) from 10.0.0.5 (nats-0, nats-1)."
- **Severity:** warning.
- **Thresholds:** lookback 10m, `min_short_connections` 20, `resolve_ratio` 0.8. A firing finding stays firing while the count is at or above `resolve_ratio` of `min_short_connections` (16), and resolves below it, so a client near the threshold does not flap.
- **Keyed by:** the whole connection name, so instances that share a name are merged.
- **Left out:** connections that stay open longer than `events.churn.pair_within` (a job that connects for minutes at a time), connections that end for another reason, and tooling connections unless `checks.include_tooling` is set.

## Behavior and rules

- **By name, not by connection.** A connection ID changes with every reconnect, so client findings are keyed by connection name. `client-001` uses the part before "@"; the other four use the whole name.
- **Tooling connections are left out.** nats-lens's own connections and, by default, clients from 127.0.0.1 or ::1 do not count, with the loopback guard described in [checks.md](checks.md). The Connections page marks them "tooling".
- **Events feed three of the checks.** `client-003`, `client-004` and `client-005` read the connect, disconnect and `client.churn` events, so they need event capture (on by default in `natslens run`). `client-005` also needs `events.churn.collapse`, on by default.
- **Per application.** The [Applications](applications.md) page lists each application's client findings on its row, and counts its reconnects of the last 24 hours by `client-004`'s rule, its slow consumer disconnects as `client-003` counts them; the API also counts its short-lived connections as `client-005` counts them.

## Scenarios

### A busy service answers pings late

- **Given** connections named `resize-worker@resize-worker-7d9f` and `resize-worker@resize-worker-k2p4`, one of them with a 250 ms round trip in two collections in a row
- **When** nats-lens collects
- **Then** `client-001` fires once, for `resize-worker`, with how many of its connections are slow and the server, connection ID and address of the slowest one

### A port-forwarded CLI is not a client problem

- **Given** an operator runs the `nats` CLI through `kubectl port-forward`, so it connects from 127.0.0.1, and reconnects several times
- **When** nats-lens collects
- **Then** no client finding fires for it
- **And** the Connections page hides it and counts it as a hidden tooling connection

### A client keeps losing its connection

- **Given** a client named `callback-delivery` disconnected four times in 10 minutes with the reason "Stale Connection", and connected again each time
- **When** nats-lens collects
- **Then** `client-004` fires for `callback-delivery`, with the reasons in the detail

### A poller opens a connection for every poll

- **Given** a client named `stock-poller` connects, makes one request and closes its connection about 12 times a minute
- **When** nats-lens collects
- **Then** the Events page hides its `client.churn` events as routine
- **And** `client-005` fires for `stock-poller`, with the count, the rate, its host and the servers in the detail

### Count tooling as clients

- **Surfaces:** `cli`
- **Given** a deployment where every client legitimately connects from a loopback address
- **When** I set `checks.loopback_is_tooling: false`
- **Then** loopback clients count in the client checks like any other client

## Planned

These client checks are not built. Each line says what it would detect and what data it needs.

- **Account connection limit near its maximum.** An account holds most of the connections its limit allows, so new clients will be refused. Needs: the account's connection limit, which nats-lens stores from ACCOUNTZ in `account_details` (from the account JWT, each account at most every 10 minutes), and the current connections per account, which it stores every collection. Only the check is missing.
- **Repeated authentication errors.** One user or address fails to log in again and again, for example with revoked credentials. Needs: the `client.auth_error` events, stored today.
- **Permission violations (deny-listed subjects).** A client publishes or subscribes to a subject it may not use. nats-server publishes no event for a permission violation; it only logs it and sends the error to the client. nats-lens cannot see it without the server log.
- **Old TLS version.** A client connects with an outdated TLS version. Needs: the TLS version per connection from CONNZ, stored today.
- **Unnamed connections.** Connections without a name make every other client check less useful. Needs: the connection name from CONNZ, stored today.
- **Outdated client library.** A client uses a library version older than a set minimum. Needs: the client library and version from CONNZ, stored today.

## Not supported / out of scope

- **No per-subject traffic.** NATS servers do not report a client's messages per subject.
- **No log reading.** nats-lens does not read server or client logs, so errors that appear only there are invisible to it.
- **No disconnecting of clients.** nats-lens never kicks a client.
- **Only stored connections count.** A server with more connections than `connz_limit` (4096) is judged on the connections nats-lens stored.
