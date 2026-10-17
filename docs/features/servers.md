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
- **More in the API (API only).** The API returns these, and the pages do not show them yet. Each server in the list has its stream and consumer leaders, its slow consumers inside the range and its configured tags. Slow consumers are counted per process: a restart starts a new count. The server detail has:
  - each route with the remote server, round trip time and pending bytes, from the newest collection in which ROUTEZ answered;
  - the gateways connected (remote clusters, unset when GATEWAYZ did not answer) and the leaf node connections;
  - the limits: maximum payload, maximum pending, write deadline, ping interval and unanswered pings, JetStream file and memory limits, and the connection limit. Each is the newest value the server reported in the range, so a collection that missed VARZ or JSZ does not hide it;
  - the limits whose value differs from the value most servers have. A tie for the most common value marks nothing, and a server that did not report a limit is neither marked nor counted for it;
  - where the server runs in Kubernetes: the pod, namespace, node, when the pod was scheduled and its container restarts in the range, The pod is the one the newest Kubernetes event for the server names. The node comes from that pod's newest event with a node; after the pod's deletion it is empty until the pod is scheduled again. Events of a pod of the same name in another watched namespace do not count. Without any such event, as when nats-lens does not watch Kubernetes, the placement is unset;
  - when the configuration was loaded or last reloaded.
- **Unreported is unset.** A limit a server did not report, such as every VARZ limit stored before nats-lens recorded them, is unset, never 0.

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

### Spot a server configured apart from the others (API only)

- **Surfaces:** `api`
- **Given** three servers, one with `max_payload` 8 MiB and two with 1 MiB
- **When** I call `GetServer` for the 8 MiB server
- **Then** `limits_differ` holds `max_payload`, and for the other two it is empty

### Read where a server runs without the Kubernetes watcher (API only)

- **Surfaces:** `api`
- **Given** nats-lens does not watch Kubernetes
- **When** I call `GetServer`
- **Then** `kubernetes` is unset, not a placement with empty names

### Read the same from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** I call `ListServers` and then `GetServer` with a server name
- **Then** I receive the list with the expected and missing servers, and the one server's numbers, endpoint outcomes and processes

## Planned

- **Servers page for 20 or more servers.** A summary row: servers answering (with how many are down), versions in use, restarts in the last 24 hours, the JetStream store's median use with its range and the fullest server, and slow consumers in 24 hours with the server most of them came from.
- **Charts across servers.** CPU, memory, total connections and total messages in and out over the range, each drawn as a band from the minimum to the maximum server with the median and the busiest server.
- **A map of every server.** One square per server, grouped by cluster, coloured by CPU, memory, connections, store used, leaders or version, with down servers outlined and the meta leader marked. Hovering a square names the server and value; a click opens it.
- **A list grouped by cluster.** Filter by name, version or tag; show "Needs attention" or all; group by cluster or nothing; problems first, then by name. Columns: state, role, version, uptime, CPU, memory, connections, leaders (stream and consumer leaders on the server), store and tags. A cluster opens by itself when one of its servers needs attention; with one cluster the list is flat. Each cluster row sums its servers.
- **Server page additions.** Tiles with each metric's range over the period. "Where it runs": the pod and node from the Kubernetes watcher, hidden when nats-lens does not watch Kubernetes. Routes with the slowest route's round trip, and gateways. "Limits and settings" (maximum payload, maximum pending, write deadline, JetStream limits, ping interval), with settings that differ between servers marked. Recent events in plain words, and how many monitoring endpoints answered.

## Not supported / out of scope

- **No server actions.** nats-lens does not restart, reload or put a server in lame duck mode.
- **No server log.** nats-lens does not read server logs.
- **No per-server thresholds.**
- **No zones.** nats-lens does not record a server's zone. Server tags, such as `az:eu-1a`, are returned instead (API only for now).
- **No "slow to answer" state.** nats-lens stores whether each monitoring request succeeded, not how long the reply took.
