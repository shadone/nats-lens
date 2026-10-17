# Servers

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [server-checks.md](server-checks.md), [monitoring-snapshots.md](monitoring-snapshots.md), [navigation.md](navigation.md), [topology.md](topology.md), [multiple-systems.md](multiple-systems.md)

## What it does

The Servers page lists every NATS server seen in the time range, by its stable name, with its health and load. A server's own page shows its numbers, its charts over the range, its findings, the outcome of every monitoring request in the newest collection, each process lifetime, and its events. Together they answer: which servers answer, which restarted, which is the meta leader, and which one is short of storage or memory.

## Behavior and rules

- **By stable name.** A restart gives a server a new server ID, so the list shows one row per server name. The page says so.
- **Who does not answer.** A box at the top names the servers that did not answer in the newest collection: "Not answering: … did not answer in the newest epoch." Their rows are marked "missing". A note lists the servers expected in the newest collection.
- **The list.** For each server: its status (the worst firing finding on it), name, version, uptime, role (meta leader, follower, or no JetStream), healthz, connections, JetStream storage with its limit, memory, CPU, and the times of its restarts in the range.
- **The server page.** Its header shows the version, role and cluster, and says when the server did not answer in the newest collection. Stat cards show uptime with the start time, healthz with its error, connections (with a link to the connection list of that server), subscriptions, slow consumers since the process started, routes, the streams and consumers with a replica on it, storage with its limit, memory and CPU, the meta leader in this server's view, and the configuration digest.
- **Charts.** Connections, messages in and out, bytes in and out, memory, CPU and JetStream storage over the range, with drain and upgrade windows shaded. Rates are computed per server process, so they have a gap at each restart rather than a spike.
- **Monitoring endpoints.** The outcome of every request to the server in the newest collection: `ok`, `error` (with the error as a tooltip) or `missing`.
- **Processes.** Each process lifetime in the range: server ID, start time, and when nats-lens first and last saw it answer.
- **Findings and events.** The findings that fire now for the server, worst first, and its newest 15 events, with a link to all of them.
- **Spread across servers in the API.** `GetTimeSeries` serves `cluster.cpu_percent` and `cluster.mem_bytes` as bands, which the page does not draw yet. Each point's value is the median server, and `low` and `high` are the lowest and highest server, each averaged over the bucket. `top` names the busiest server, the one with the highest mean over the range, and `top_points` holds its own series in the same buckets. A server that did not report the number is left out. `server.slow_consumers` is a server's slow consumer increase, per process, so a restart is not a negative count. A cluster metric with an entity is refused as `invalid_argument`.

## Scenarios

### Find the server that restarted

- **Given** `nats-1` restarted at 04:12 UTC within the range
- **When** I open the Servers page
- **Then** the row of `nats-1` lists 04:12 under "Restarts in range" and a shorter uptime than the others

### See why a server's data is incomplete

- **Given** a server that did not answer JSZ in the newest collection
- **When** I open its page
- **Then** "Monitoring endpoints" shows JSZ as `missing`, and the other endpoints as `ok`

### Read a server's history of processes

- **Given** a server that restarted twice in the range
- **When** I open its page
- **Then** "Processes" lists three server IDs with their start times and when each first and last answered

### Read the same from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** I call `ListServers` and then `GetServer` with a server name
- **Then** I receive the list with the expected and missing servers, and the one server's numbers, endpoint outcomes and processes

### Find the busiest server from the API

- **Surfaces:** `api`
- **Given** three servers at 2, 5 and 40 percent CPU
- **When** I call `GetTimeSeries` for `cluster.cpu_percent`
- **Then** each point has the value 5, `low` 2 and `high` 40, and `top` is the server at 40 percent with its own points

## Planned

- **Servers page for 20 or more servers.** A summary row: servers answering (with how many are slow or down), versions in use, restarts in the last 24 hours, the JetStream store's median use with its range and the fullest server, and slow consumers in 24 hours with the server most of them came from.
- **Charts across servers.** CPU, memory, total connections and total messages in and out over the range, each drawn as a band from the minimum to the maximum server with the median and the busiest server.
- **A map of every server.** One square per server, grouped by cluster, coloured by CPU, memory, connections, store used, leaders or version, with down servers outlined and the meta leader marked. Hovering a square names the server and value; a click opens it.
- **A list grouped by cluster.** Filter by name, zone, version or tag; show "Needs attention" or all; group by cluster, zone or nothing; problems first, then by name. Columns: state, role, version, uptime, CPU, memory, connections, leaders (stream and consumer leaders on the server), store and zone. A cluster opens by itself when one of its servers needs attention; with one cluster the list is flat. Each cluster row sums its servers.
- **Server page additions.** Tiles with each metric's range over the period. "Where it runs": the pod, node and zone from the Kubernetes watcher, hidden when nats-lens does not watch Kubernetes. Routes with the slowest route's round trip, and gateways. "Limits and settings" (maximum payload, maximum pending, write deadline, JetStream limits, ping interval), with settings that differ between servers marked. Recent events in plain words, and how many monitoring endpoints answered.

## Not supported / out of scope

- **No server actions.** nats-lens does not restart, reload or put a server in lame duck mode.
- **No server log.** nats-lens does not read server logs.
- **No per-server thresholds.**
- **No zones yet.** The zone of a server is planned with the Kubernetes data above; today the list has no zone column.
