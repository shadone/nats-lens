# Client checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [server-checks.md](server-checks.md), [account-checks.md](account-checks.md), [connections.md](connections.md), [event-capture.md](event-capture.md), [applications.md](applications.md), [accounts-and-auth.md](accounts-and-auth.md)

## What it does

The client checks judge the applications connected to NATS: whether their round trip time is high, whether the server queues bytes they cannot read fast enough, whether they were disconnected as slow consumers, whether they keep reconnecting, whether they open connections that last only seconds, whether a client keeps failing to log in, whether connections go without a name, and whether an application uses an old client library. A client finding is keyed by connection name rather than by connection, so it survives reconnects and covers every instance that uses the name; `client-006` is keyed by the user a client logs in as. Clients without a name are grouped as `(unnamed)`. There are 8 client checks, `client-001` to `client-008`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

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

### `client-006` Repeated authentication errors

- **Problem:** the servers keep refusing the login of one user in one account: a client retries with credentials that are wrong, revoked or expired, so it never connects and its work does not get done. A client that retries every minute shows up here and on the Accounts page's failed logins before anyone notices that it never connected.
- **Fires when:** the `client.auth_error` events of the user in the account within the lookback reach `min_failed_logins`, from any address and server. The detail gives the count, the rate per hour, the addresses (five, then how many more), the servers, the reasons, the time of the last one and the client names the clients gave, for example "16 failed logins in the last 15m (about 64 an hour) as label-printer in account APP from 10.20.4.31, 10.20.4.32 (nats-0, nats-1): Authentication Failure; the last at 09:18:00 UTC. Client names: label-printer@lp-a, label-printer@lp-b." The finding links the last event.
- **Severity:** warning.
- **Thresholds:** lookback 15m, `min_failed_logins` 15, `resolve_ratio` 0.8. 15 in 15 minutes is the rate of 60 an hour at which the Accounts page marks a group of failed logins. A firing finding stays firing while the count is at or above `resolve_ratio` of `min_failed_logins` (12), and resolves below it, so a client that retries at about the threshold's rate does not flap. Proposed, not yet confirmed.
- **Keyed by:** the user the client gave and the account the server names, as `<user> in <account>` (for example `label-printer in APP`), so the pods of one deployment that share a credential are one finding and a pod that restarts with a new address keeps it, while one user name in two accounts is two credentials and two counts. nats-server's server-wide event names, for a client refused before it got an account, the global account `$G` or `N/A`; only the account-scoped event of auth callout names the account the client tried. So an event without an account, or with `$G` or `N/A`, is keyed `<user> in (unknown account)`, and the detail says "in an unknown account": in operator mode `$G` would name an account that does not exist. With most servers this is the usual key. Proposed, not yet confirmed. A client that gave no user is grouped as `(no user)`, with its addresses in the detail, and one that logged in with a token, which nats-server reports as `[REDACTED]`, as `(token)`. nkey and JWT users appear as their public keys, as the servers report them. The Accounts page groups the same events by user, address and reason and shows the account under the user; the check does not split by address or reason, so a new pod's address neither starts a finding nor splits the count. Proposed, not yet confirmed.
- **Keys changed.** A `client-006` finding from a build that keyed by user only (`label-printer`) resolves once after the upgrade and is replaced by one with the new key (`label-printer in (unknown account)`); a mute set on the old key no longer applies, so mute the new key again.
- **Reasons:** nats-server gives every refused login the reason "Authentication Failure" ("Proxy Not Trusted" or "Proxy Required" with proxy authentication), so the finding cannot say whether the credentials were wrong, revoked or expired. Failures without a user from addresses nobody knows are often a port scanner, or a load balancer or HTTP health check that probes the client port rather than the monitoring port's `/healthz`; the remediation says so. A client that gives up waiting for authentication ("Authentication Timeout") sends no authentication error event and does not count.
- **Needs event capture.** Without it nats-lens stores no `client.auth_error` event and the check stays quiet; it cannot tell "no failed login" from "not captured". The Accounts page says which it is.
- **Left out:** tooling connections, unless `checks.include_tooling` is set: a port-forwarded `nats` CLI with a mistyped password is an operator's typo, not a client problem. Proposed, not yet confirmed.

### `client-007` Unnamed connections

- **Problem:** connections without a name make every other client check less useful: the client checks, the Applications page and the Events page group clients by their name, so unnamed clients are merged into one `(unnamed)` entry, and a finding for it cannot say which service it is about.
- **Fires when:** a connection without a name, or with nothing before the "@" (the Applications page's `(unnamed)`), has been connected for `min_age` or longer in this collection. The detail counts the unnamed connections and those connected for `min_age`, and names their accounts, users, addresses, client libraries (five of each, then how many more) and servers, for example "3 connections without a name, 1 connected for 10m or longer; accounts $G, APP, user alice, from 10.0.0.5 (APP), 10.0.0.6 (APP), 10.0.0.7 ($G), go 1.54.0, nats.js 3.0.2 (nats-0, nats-1, nats-2)." With more than one account, each address is paired with its account. A connection without an account counts as `$G`.
- **Severity:** info.
- **Thresholds:** `min_age` 10m, so a short script or a one-off CLI call does not fire. Once firing, the finding stays while any unnamed connection is connected, however young, so a client that reconnects does not resolve and fire again. It is also kept through a collection that may lack connections: a server did not answer, its CONNZ failed, or its list was cut at `connz_limit`; the detail then says that no unnamed connection was seen in a collection that lacks some servers' connections. The hold ends at the 10th such collection in a row without an unnamed connection, which resolves the finding. It resolves at once in a whole collection without one. A collection that stays partial (`connz_limit` below the number of clients, or a server that never answers again) cannot show that the unnamed clients are gone, so the finding would hold for those 10 collections each time it fires; raise `connz_limit` to fix it. Proposed, not yet confirmed.
- **Keyed by:** `(unnamed)`, one finding for every unnamed connection of the system, so the Applications page lists it on the `(unnamed)` row.
- **Left out:** MQTT clients, which have no NATS connection name to set (websocket clients can set one and count), and tooling connections, unless `checks.include_tooling` is set, so a port-forwarded CLI without a name does not count. A connection without a start time is counted but never old enough to fire. Proposed, not yet confirmed.
- **Not the Applications page's count.** The Applications page's `(unnamed)` row still counts MQTT clients, so it can list connections that `client-007` leaves out.

### `client-008` Outdated client library

- **Problem:** an application uses a client library older than the minimum set for its language. An old library misses fixes in reconnect handling, JetStream and security.
- **Fires when:** within the lookback, a connection of the application reported a library language with a minimum and a version below it. The version is compared by its first three numbers, so 2.10.0 is newer than 2.9.0 and 1.37.0-beta.1 counts as 1.37.0. The detail describes the newest collection within the lookback that had such a connection: how many of the application's connections used an old library then, which libraries with their minimums, and the servers, for example "2 of 3 connections use a client library older than its minimum: go 1.31.0 (minimum 1.37.0) (nats-0, nats-1)." When that collection is older than the one checked, the detail starts "In the collection of 09:54:30 UTC".
- **Severity:** info.
- **Thresholds:** lookback 1h, and a minimum per language as a version threshold (one to three numbers joined by dots):

  | Threshold | Language the client sends | Library | Default |
  | --- | --- | --- | --- |
  | `min_go` | `go` | nats.go | 1.37.0 |
  | `min_nats_js` | `nats.js` | nats.js on Node.js | 2.28.0 |
  | `min_python` | `python3` | nats.py | 2.9.0 |
  | `min_java` | `java` | jnats | 2.20.0 |
  | `min_dotnet` | `.NET` | NATS.Net | 2.5.0 |
  | `min_rust` | `rust` | async-nats | 0.37.0 |
  | `min_c` | `C` | nats.c | 3.9.0 |

  Each default is the release line that was current on 1 November 2024, two years before the defaults were set; the newest releases now are nats.go 1.55.0, nats.js 3.4.0, nats.py 2.16.0, jnats 2.26.4, NATS.Net 3.3.2, async-nats 0.51.0 and nats.c 3.14.0. A minimum of 0 turns a language off. Proposed, not yet confirmed.
- **Resolves:** a lookback after the last old connection of the application is gone, so an upgraded application resolves within the hour and a job that connects with an old library now and then does not flap.
- **Keyed by:** the application, the connection name before the first "@", as the Applications page groups it; a client without a name is `(unnamed)`.
- **Unknown versions.** A version that does not start with a number (`dev`, `unknown`, or one with a leading space) is unknown and not judged, and does not stop the check for other applications. So is nats.py's `0.0.0`, which a build without package metadata reports. Proposed, not yet confirmed.
- **Left out:** languages without a minimum (`nats.deno`, `nats.ws`, the older NATS.Client for .NET, which sends `C#`, and others), connections without a version or whose version does not start with a number, and tooling connections unless `checks.include_tooling` is set: nats-lens's own connections and a port-forwarded CLI are not applications to upgrade.

## Behavior and rules

- **By name, not by connection.** A connection ID changes with every reconnect, so client findings are keyed by connection name. `client-001` and `client-008` use the part before "@" (the application); `client-002` to `client-005` use the whole name; `client-007` is one finding, `(unnamed)`. `client-006` is keyed by the user a client logs in as and its account, since a client that fails to log in is known by its credentials.
- **Tooling connections are left out.** nats-lens's own connections and, by default, clients from 127.0.0.1 or ::1 do not count, with the loopback guard described in [checks.md](checks.md). The Connections page marks them "tooling".
- **Events feed four of the checks.** `client-003`, `client-004` and `client-005` read the connect, disconnect and `client.churn` events, and `client-006` the `client.auth_error` events, so they need event capture (on by default in `natslens run`); without it they stay quiet. `client-005` also needs `events.churn.collapse`, on by default.
- **One problem, one finding.** A refused login also ends with a `client.disconnect` whose reason is "Authentication Failure", but no `client.connect`, so it does not count as a reconnect for `client-004` unless other clients of the same name connect in the same 10 minutes. `account-004` warns before the user JWTs of connected clients expire; once a JWT has expired, its client is refused and counts for `client-006`. `client-007` and `client-008` are info: the Applications page shows the `(unnamed)` row and each application's library too, and the findings make them part of the review of findings.
- **Per application.** The [Applications](applications.md) page lists each application's client findings on its row (`client-007` on the `(unnamed)` row; `client-006`, keyed by user and account, on none), and counts its reconnects of the last 24 hours by `client-004`'s rule, its slow consumer disconnects as `client-003` counts them; the API also counts its short-lived connections as `client-005` counts them.

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

### A revoked client keeps retrying

- **Given** a client logging in as `label-printer`, whose credentials were revoked, retrying about once a minute from two pods, `10.20.4.31` and `10.20.4.32`, on `nats-0` and `nats-1`
- **When** nats-lens collects
- **Then** `client-006` fires once, for `label-printer in APP`, with the count in the last 15 minutes, about 64 an hour, both addresses, both servers, the reason "Authentication Failure" and the client names
- **When** the client gets valid credentials and stops failing
- **Then** the finding resolves once fewer than 12 failed logins are left in the last 15 minutes

### A service connects without a name

- **Given** a service that connects with nats.go and gives no connection name, connected for an hour from `10.0.0.5` as user `alice` in account `APP`
- **When** nats-lens collects
- **Then** `client-007` fires at info for `(unnamed)`, with the account, the user, the address, the library and the server in the detail
- **And** the Applications page lists the finding on the `(unnamed)` row

### An application runs an old library

- **Given** `resize-worker` with three pods, two on nats.go 1.31.0 and one on 1.54.0
- **When** nats-lens collects
- **Then** `client-008` fires at info for `resize-worker`: "2 of 3 connections use a client library older than its minimum: go 1.31.0 (minimum 1.37.0)", with the servers
- **When** the two pods are upgraded
- **Then** the finding resolves an hour after the last old connection was seen

### Follow my own upgrade policy

- **Surfaces:** `cli`
- **Given** a team that supports only the newest nats.js line
- **When** I set `checks.thresholds.client-008.min_nats_js: "3.0"` and `min_python: "0"`, and restart nats-lens
- **Then** applications on nats.js 2.x fire `client-008`, and python clients are not judged

### Count tooling as clients

- **Surfaces:** `cli`
- **Given** a deployment where every client legitimately connects from a loopback address
- **When** I set `checks.loopback_is_tooling: false`
- **Then** loopback clients count in the client checks like any other client

## Planned

These client checks are not built. Each line says what it would detect and what data it needs.

- **Permission violations (deny-listed subjects).** A client publishes or subscribes to a subject it may not use. nats-server publishes no event for a permission violation; it only logs it and sends the error to the client. Needs: the server log, which nats-lens does not read, or an advisory from a later nats-server.

Built since this list was written: "Account connection limit near its maximum" is `account-001` ([account-checks.md](account-checks.md)); "Repeated authentication errors" is `client-006`; "Unnamed connections" is `client-007`; "Outdated client library" is `client-008`.

## Not supported / out of scope

- **No per-subject traffic.** NATS servers do not report a client's messages per subject.
- **No log reading.** nats-lens does not read server or client logs, so errors that appear only there are invisible to it.
- **No disconnecting of clients.** nats-lens never kicks a client.
- **Only stored connections count.** A server with more connections than `connz_limit` (4096) is judged on the connections nats-lens stored.
- **No old TLS version check.** nats-server refuses a `min_version` below TLS 1.2 and offers nothing older, so a client cannot connect with TLS 1.0 or 1.1, and TLS 1.2 is not a problem in itself; nats-lens stores each connection's TLS version from CONNZ. A connection whose TLS ends at a load balancer in front of the servers shows no TLS version at all. Dropped from the planned checks by the session that built `client-006` to `client-008`; proposed, not yet confirmed.
