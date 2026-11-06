# Servers

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [server-checks.md](server-checks.md), [monitoring-snapshots.md](monitoring-snapshots.md), [navigation.md](navigation.md), [topology.md](topology.md), [multiple-systems.md](multiple-systems.md)

## What it does

The Servers page shows a system's servers at a glance, built for 20 servers or more: a row of data points, charts across all servers, a map with one square per server, and a list grouped by cluster with the servers that need attention first. A server's own page shows six metric boxes, where it runs, its routes, its limits and settings, its recent events, the monitoring requests it answered, its findings and each process lifetime. Together they answer: which servers answer, which restarted, which is the meta leader, which is configured or versioned apart, and which one is short of storage or under load.

## Behavior and rules

- **By stable name.** A restart gives a server a new server ID, so every view shows one entry per server name.
- **Status line.** "24 servers in 3 clusters · meta leader nats-eu-1": the count, the clusters when there are several, and the meta leader.
- **Data points.** Five cards:
  - **Answering:** how many servers answered in the newest collection out of those seen or expected, a square per server (red when it did not answer), and the names of those that did not.
  - **Versions:** how many versions run, a bar split by version, and each version with its count (with more than four, the fourth onwards as "other versions"). The legend draws each version's swatch.
  - **Restarts:** the restarts in the range ("Restarts, 24 h" for a live range), a strip with a tick at each restart, and the servers that restarted.
  - **JetStream store:** the median use of the store limit across servers, a track with the range from the emptiest to the fullest server and a tick at the median, and the fullest server.
  - **Slow consumers:** the slow consumers counted in the range across servers, a bar per bucket of `cluster.slow_consumers`, and the server with the most.
- **Charts across servers.** The same charts as the Overview's trends: CPU and memory are drawn as a band from the lowest to the highest server, with the median as a line and the busiest server (the highest mean over the range) dashed and named with its value ("busiest: nats-ap-2, 64.1%"); connections as the total; messages in and out per second, with out dashed. Each chart starts at zero, except Memory, which spans its own range with a small margin; its legend draws its own marks, its value is the newest bucket's median ("6.7% median") or total, and it states its minimum and maximum.
- **Every server.** One square per server, grouped by cluster, coloured by a metric chosen with the "Colour by" buttons: CPU, memory, connections, store used, leaders or version. CPU and store used have fixed bins (under 10%, 10 to 30%, 30 to 60%, 60% or more); memory, connections and leaders split the spread between the smallest and largest server into quarters; version gives each version its own colour, the most common first, and with more than four versions the fourth onwards share "other versions". A percentage is binned as it is shown, so "60%" is in "60% or more", and the relative scales use only the servers that answered. The meta leader is outlined; a server that did not answer is dashed and has no fill; a server that did not report the value has no colour. Hovering or focusing a square names the server and its value; a click opens the server. The legend draws each swatch, including "did not answer", "not reported" and "meta leader" when they occur. The colours are defined for the light and the dark theme.
- **The list.**
  - Grouped by cluster, each group headed by its cluster's summary (servers, how many answer, median CPU, connections, median store). With one cluster the list is flat; a filter that leaves one of several clusters keeps its heading.
  - Problems first: a server that did not answer, then by the severity of its findings, then the other reasons below, then the rest, each by name; the group with the worst server comes first.
  - "Needs attention" is shown by default. A server needs attention when it did not answer, a warning or critical finding fires on it (info and muted ones do not count), its healthz is not ok, its version is not the one most servers run, or its store is at 60% of its limit or more. The State column names each reason ("did not answer · warning finding", "version 2.12.2", "store 71%"). An info finding is shown there too, as a notice after the reasons ("ok · info finding"), and a server with only an info finding is listed after the servers with a reason.
  - "All" shows every server, six per cluster (twenty in a flat list) with "Show N more".
  - A filter matches the name, the version or a tag.
  - Columns: server, state, role, version, uptime, CPU, memory, connections, leaders (stream and consumer leaders on the server), JetStream file storage with stored bytes, a positive configured limit when reported, and a bar with its percentage, and tags. A server that did not answer shows "–" for the numbers of the newest collection, its leaders included.
- **Storage meaning.** List storage values cover the whole server, regardless of account scope, and include local replica copies. They are not unique application data or filesystem use. Filesystem used bytes and capacity are not collected. Zero stored bytes remain visible; missing bytes read "not reported", and a missing or nonpositive limit reads "limit unknown". A server without JetStream reads "no JetStream"; a server that did not answer reads "did not answer". Percentages use only answering servers with a positive limit and reported bytes.
- **The server page.**
  - The header names the cluster in the breadcrumb, and the role, version, uptime and start time in one line, or "did not answer in the newest collection".
  - **Metric boxes** for CPU, memory, JetStream file storage, connections, messages (in as the value and line, out dashed with its current rate in the note) and slow consumers. Each has a title with an (i) button describing the metric, the current value at the top right, a line chart over the range, and a track with the window's lowest value on the left, its highest on the right and a dot at the current value. The ends are the window's own lowest and highest values; a current value outside them puts the dot at the nearer end. When the value did not change the track reads "steady at N all window"; without a series there is no track and the box reads "no data for the window"; without a current value the track has no dot and reads "no current value". A configured limit, such as the connection limit, is in the note under the track. Health colours the value and the dot, with a word only when it is not ok: CPU at 60% of a core or more is "high", connections at 80% of the limit or more are "near limit", and slow consumers in the range mark the box. A server that did not answer shows no current value, only its history.
  - **JetStream file storage:** the current file bytes and the positive configured limit from the newest collection, a history chart of bucket averages, and the percentage of that limit when both values are reported. No current value is borrowed from history or an older limit. JSZ supplies storage, with VARZ as fallback. Bucket averages use available storage samples; the existing server and STATSZ/VARZ hatches do not identify every storage-only missing report. The card explains that filesystem used bytes and capacity are not collected. Metric descriptions open below their header on focus or click, stay inside the card width, and close with Escape. History follows the stable server name across process restarts; navigating to another server clears the previous server’s series while the new series loads.
  - **Where it runs:** the pod and namespace with its restarts in the range, the node (or "not scheduled"), when the pod was scheduled, and the zone from an `az:` tag. Hidden when nats-lens does not watch Kubernetes.
  - **Routes:** the route count with the slowest route's remote server and round trip, bytes waiting on routes when there are any, gateways, leaf nodes and the meta leader in the server's view.
  - **Limits and settings:** maximum payload, maximum pending, write deadline, ping interval with the unanswered pings after which a connection closes, JetStream file and memory limits, the connection limit, and the tags. A setting whose value differs from the value most servers have is marked "differs from most servers", and a line counts them. The time the configuration was loaded closes the card.
  - **Recent events:** the server's newest six events, routine ones hidden and repeats folded ("×4"), each linked to the event, with a link to all of the server's events.
  - **Endpoints answered:** "13/13 monitoring endpoints answered in the newest collection", naming up to four that did not, or counting them by state when more failed.
  - **Findings** that fire now for the server, worst first, and **Processes**: each process lifetime in the range with its server ID, start time, and when nats-lens first and last saw it answer.
- **Spread across servers in the API.** `GetTimeSeries` serves `cluster.cpu_percent` and `cluster.mem_bytes` as bands. Each point's value is the median server, averaged over the bucket, and `low` and `high` are the lowest and highest server in any collection of the bucket, so a short spike stays visible. The series' `min` and `max` are the lowest `low` and highest `high`. `top` names the busiest server, the one with the highest mean over the range, and `top_points` holds its own averaged series in the same buckets, which lies inside the band. A server that did not report the number is left out. `server.slow_consumers` is a server's slow consumer increase, per process, so a restart is not a negative count; the first collection after a restart, and the first after a gap in collection of more than three intervals, count nothing. A cluster metric with an entity is refused as `invalid_argument`. The Servers page reads its charts across servers and the slow-consumer strip in one `GetTimeSeriesBatch` call ([overview-page.md](overview-page.md)).
- **Server data in the API.** Each server in `ListServers` has its stream and consumer leaders, its slow consumers inside the range (counted per process) and its configured tags. `GetServer` adds:
  - each route with the remote server, round trip time and pending bytes, from the newest collection in which ROUTEZ answered;
  - the gateways connected (remote clusters, unset when GATEWAYZ did not answer) and the leaf node connections;
  - the limits. Each is the newest value the server reported in the range, so a collection that missed VARZ or JSZ does not hide it;
  - the limits whose value differs from the value most servers have. A tie for the most common value marks nothing, and a server that did not report a limit is neither marked nor counted for it;
  - where the server runs in Kubernetes. The pod is the one the newest Kubernetes event for the server names. The node comes from that pod's newest event with a node; after the pod's deletion it is empty until the pod is scheduled again. Events of a pod of the same name in another watched namespace do not count. Without any such event, as when nats-lens does not watch Kubernetes, the placement is unset;
  - when the configuration was loaded or last reloaded.
- **Unobserved is a dash.** A number nats-lens did not observe shows "–" and no colour, never 0. A limit a server did not report, such as every VARZ limit stored before nats-lens recorded them, is unset in the API.

Server detail charts mark recorded missing answers and missing STATSZ/VARZ metrics reports with bounded hatches. These do not establish downtime. See [Data history](data-history.md).

## Scenarios

### Read storage without a configured limit

- **Surfaces:** `ui`
- **Given** an answering server reports zero JetStream file bytes and no positive storage limit
- **When** I show all servers
- **Then** its storage cell reads "0 B · limit unknown" and shows no percentage bar


### Read one server’s storage history

- **Surfaces:** `ui`
- **Given** a server reports 1 MiB of JetStream file bytes and a 2 MiB configured limit in the newest collection
- **When** I open its page
- **Then** the storage box shows 1 MiB, "1 MiB / 2 MiB", "50% of configured limit", and its history in bytes
- **And** missing current bytes show no current value even when historical points or older limits exist

### Find the server that restarted

- **Given** `nats-1` restarted at 04:12 UTC within the range
- **When** I open the Servers page
- **Then** the Restarts card counts the restart, ticks it at 04:12 and names `nats-1`, and under "All" its row shows a shorter uptime than the others

### See why a server's data is incomplete

- **Given** a server that did not answer JSZ in the newest collection
- **When** I open its page
- **Then** it reads "12/13 monitoring endpoints answered in the newest collection; not: JSZ missing"

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

### Find the busiest server from the API

- **Surfaces:** `api`
- **Given** three servers at 2, 5 and 40 percent CPU
- **When** I call `GetTimeSeries` for `cluster.cpu_percent`
- **Then** each point has the value 5, `low` 2 and `high` 40, and `top` is the server at 40 percent with its own points

### See which servers need attention

- **Given** the demo system `example-global`: 24 servers in clusters `eu`, `us` and `ap`; `nats-us-4` stopped answering, `nats-us-3` has a warning finding, `nats-ap-2`'s store is 71% full, and `nats-ap-4` and `nats-ap-5` run an older version
- **When** I open the Servers page
- **Then** "Needs attention 5" is selected and lists those five servers under their clusters, `nats-us-4` first in `us`, with the reasons in the State column; "All 24" lists every server

### An info finding is a notice, not a reason

- **Given** `nats-eu-0` has an info finding and nothing else is wrong with it
- **When** I open the Servers page
- **Then** it is not under "Needs attention", and under "All" its State column reads "ok · info finding"

### Colour the map by store

- **Given** `example-global`
- **When** I choose "Store used" under "Every server"
- **Then** `nats-ap-2` is in the "60% or more" colour, and the legend draws each bin's swatch

### Read a server configured apart from the others

- **Given** `nats-ap-3` with a maximum payload of 8 MiB where the others have 1 MiB
- **When** I open its page
- **Then** "Limits and settings" marks the maximum payload "differs from most servers" and says "1 setting differs from most servers."

### Open a server without the Kubernetes watcher

- **Given** nats-lens does not watch Kubernetes
- **When** I open a server's page
- **Then** there is no "Where it runs" card, and the API returns `kubernetes` unset

### One cluster

- **Given** a system whose servers form one cluster
- **When** I open the Servers page
- **Then** the list is flat, with no cluster headings

## Not supported / out of scope

- **No server actions.** nats-lens does not restart, reload or put a server in lame duck mode.
- **No server log.** nats-lens does not read server logs.
- **No per-server thresholds.**
- **No zones.** nats-lens does not record a server's zone. Server tags, such as `az:eu-1a`, are shown instead, and the zone on a server's page comes from an `az:` tag.
- **No grouping by zone or tag.** The list groups by cluster only.
- **No "slow to answer" state.** nats-lens stores whether each monitoring request succeeded, not how long the reply took.
