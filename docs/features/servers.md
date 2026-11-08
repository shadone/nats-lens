# Servers

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [server-checks.md](server-checks.md), [monitoring-snapshots.md](monitoring-snapshots.md), [navigation.md](navigation.md), [topology.md](topology.md), [multiple-systems.md](multiple-systems.md)

## What it does

The Servers page shows a system's servers at a glance, built for 20 servers or more: a row of data points, charts across all servers, a map with one square per server, and a list grouped by cluster with the servers that need attention first. A server's own page shows six metric boxes, where it runs, its routes, its limits and settings, its recent events, the monitoring requests it answered, its findings and each process lifetime. Together they answer: which servers answer, which restarted, which is the meta leader, which is configured or versioned apart, and which one is short of storage or under load.

Page and shared-chart handoff preserve known identities through [agent investigations](agent-investigations.md).

## Behavior and rules

- **Chart descriptions.** Summary, fleet and detail charts use the shared [chart help](chart-help.md) controls. The surface inventory and catalogue behavior are documented there.

- **Chart help.** Each of the five Servers summary cards and four fleet charts has an About info control. Shared trend charts also expose help on Overview and Capacity. Metric charts read their descriptions from the API catalogue on first inspection and reuse that cache; failures remain separate from chart-data availability. Summary-card help describes its own calculation and scope. Info previews use the shared viewport overlay, open on hover/focus or click, and dismiss with Escape or leaving focus. Slow-consumer caveats live in this optional help while investigation links remain visible. The complete surface inventory is in [Chart help](chart-help.md).

- **By stable name.** A restart gives a server a new server ID, so every view shows one entry per server name.
- **Status line.** "24 servers in 3 clusters · meta leader nats-eu-1": the count, the clusters when there are several, and the meta leader.
- **Data points.** Five cards:
  - **Answering:** how many servers answered in the newest collection out of those seen or expected, a square per server (red when it did not answer), and the names of those that did not.
  - **Versions:** how many versions run, a bar split by version, and each version with its count (with more than four, the fourth onwards as "other versions"). The legend draws each version's swatch.
  - **Restarts:** the restarts in the range ("Restarts, 24 h" for a live range), a strip with a tick at each restart, and the servers that restarted.
  - **JetStream store:** the median use of the store limit across servers, a track with the range from the emptiest to the fullest server and a tick at the median, and the fullest server.
  - **Slow-consumer detections:** "A detection means NATS had trouble sending data because a receiving connection could not keep up." The card offers "Investigate detections", a link from the busiest server and keyboard-accessible links from nonzero chart buckets. Links freeze the observed absolute time range and preserve system and account context. Optional help explains clients, routes, gateways, leaf connections and repeated retries. These detections are separate from unique JetStream consumers and pending messages.
- **Slow-consumer investigation.** The investigation shows counter increases between collections of the same server process, with collection numbers and process IDs. Baselines, counter resets and gaps longer than three collect intervals have no inferred increase. Short missing-report gaps widen the observed interval. The interval can begin before the selected range; its end collection lies inside it. The page lists recorded slow-consumer disconnects and slow-consumer finding transitions separately. Event IDs open the recorded payload; finding links open the finding. Client names and connection IDs appear only when recorded. The counters do not establish an exact detection time, affected connection, application slowness, data loss or disconnect. Event and detection counts need not match.
- **Counter agreement.** The headline, server summaries, charts and investigation select report endpoints from the range start inclusive to its end exclusive. A report up to three collection intervals before the start can supply a baseline. A missing counter breaks the baseline; a missing server report can widen the interval up to three collection intervals. Each process starts a separate baseline. Chart buckets add these same increases.
- **One observed interval.** "Investigate this interval" selects the observation's process ID and collection number for the counter total. Supporting events and findings retain the entire observed interval, with visible inclusive and exclusive bounds. Links round the start down to its second and end after the endpoint's second. For 13:41:00–13:41:30, supporting evidence spans 13:41:00 inclusive to 13:41:31 exclusive. An earlier counter endpoint at 13:41:00 does not enter the selected counter total. Recorded evidence in that second remains available independently.
- **Evidence coverage.** The investigation distinguishes unavailable capture history, incomplete history and no matching recorded disconnects. A counter-only result directs the reader to the affected server and its diagnostics. Partial collections remain explicit. An account filter limits recorded client evidence while counters and server findings remain system-wide.
- **Bounded agent access.** The read-only investigation API returns the same counter intervals, stable evidence references, coverage and attribution limits. Each evidence list defaults to 100 rows and is capped at 200 with an independent truncation flag. Totals cover all matches. A request spans at most 30 days; callers narrow time or server when a list is cut. The response identifies stored or synthetic demo provenance.
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
  - **Metric boxes** for CPU, memory, JetStream file storage, connections, messages (in as the value and line, out dashed with its current rate in the note) and slow-consumer detections. Each has a title with an (i) button describing the metric, the current value at the top right, a line chart over the range drawn as on the Overview tiles (line and fill neutral, the change in the accent or, in a box that is not ok, in its health colour), and a track with the window's lowest value on the left, its highest on the right and a dot at the current value. The ends are the window's own lowest and highest values; a current value outside them puts the dot at the nearer end. When the value did not change the track reads "steady at N all window"; without a series there is no track and the box reads "no data for the window"; without a current value the track has no dot and reads "no current value". A configured limit, such as the connection limit, is in the note under the track. Health colours the value and the dot, with a word only when it is not ok: CPU at 60% of a core or more is "high", connections at 80% of the limit or more are "near limit", and slow-consumer detections in the range mark the box. The detections box offers the same concise explanation and an investigation link filtered to that server. A server that did not answer shows no current value, only its history.
  - **JetStream file storage:** the current file bytes and the positive configured limit from the newest collection, a history chart of bucket averages, and the percentage of that limit when both values are reported. No current value is borrowed from history or an older limit. JSZ supplies storage, with VARZ as fallback. Bucket averages use available storage samples; the existing server and STATSZ/VARZ hatches do not identify every storage-only missing report. The card explains that filesystem used bytes and capacity are not collected. Metric descriptions open below their header on focus or click, stay inside the card width, and close with Escape. History follows the stable server name across process restarts; navigating to another server clears the previous server’s series while the new series loads.
  - Placement, routes and limits share a responsive row. **Recent events** spans the full width below them. Long names, tags and event summaries wrap within their cards.
  - **Where it runs:** the pod and namespace with its restarts in the range, the node (or "not scheduled"), when the pod was scheduled, and the zone from an `az:` tag. Hidden when nats-lens does not watch Kubernetes.
  - **Routes:** the route count with the slowest route's remote server and round trip, bytes waiting on routes when there are any, gateways, leaf nodes and the meta leader in the server's view.
  - **Limits and settings:** maximum payload, maximum pending, write deadline, ping interval with the unanswered pings after which a connection closes, JetStream file and memory limits, the connection limit, and the tags. A setting whose value differs from the value most servers have is marked "differs from most servers", and a line counts them. The time the configuration was loaded closes the card.
  - **Recent events:** the server's newest six events, routine ones hidden and repeats folded ("×4"), each linked to the event with selectable summary text, with a link to all of the server's events.
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

### Investigate detections without knowing counter terminology

- **Surfaces:** `ui`
- **Given** the servers report slow-consumer detections in the selected range
- **When** I open Servers or a server page
- **Then** the main text explains that NATS had trouble sending data because a receiving connection could not keep up
- **And** "Investigate detections" opens the evidence with system, account and absolute time preserved
- **And** optional metric help explains counter semantics without sending the reader to JetStream backlog

### Compare counter intervals with recorded evidence

- **Surfaces:** `ui`, `api`
- **Given** a server counter increases by five and two recorded disconnects name the same connection
- **When** I investigate the selected range
- **Then** counter detections and recorded disconnects have separate totals
- **And** each observation identifies its server process, collection and observed interval without an exact detection time
- **And** recorded event IDs and payload connection IDs remain separate from counter attribution

### Keep totals when opening an investigation

- **Surfaces:** `ui`, `api`
- **Given** a server reports counters 0, 5 and 8 at 30-second intervals
- **When** I select a range starting 15 seconds after the first report and ending after the third
- **Then** the headline, server chart and investigation each count 8 detections
- **And** the first report supplies a baseline without entering the selected range

### Distinguish a missing counter from a missing report

- **Surfaces:** `ui`, `api`
- **Given** a server reports counter 0, misses a collection, then reports 6 and 9
- **When** another monitoring endpoint answered in the missed collection but the counter is absent
- **Then** the return report has an unknown increase, and the next report contributes 3 detections
- **And** when the whole server report is absent, the return contributes 6 only if its baseline is at most three collection intervals old
- **And** a longer gap or a new process starts a baseline, so the next report contributes 3
- **And** summaries, detail totals, charts and investigation count the same increases
- **And** the investigation gives unavailable intervals a reason; a comparable unchanged counter adds zero without an unavailable observation

### Select one observation without losing supporting events

- **Surfaces:** `ui`, `api`
- **Given** consecutive observations end at 13:41:00 and 13:41:30
- **When** I investigate the second observation's interval
- **Then** the counter total includes only that process and collection
- **And** supporting events and findings span 13:41:00 inclusive to 13:41:31 exclusive
- **And** the page displays the counter selection and supporting evidence bounds separately

### Investigate a counter without captured identity

- **Surfaces:** `ui`, `api`
- **Given** counter increases exist and no matching disconnect event is recorded
- **When** I investigate the range
- **Then** the response explains that the counter cannot identify the affected connection
- **And** coverage distinguishes unavailable or incomplete capture history from no matching recorded event

### Bound an investigation

- **Surfaces:** `ui`, `api`
- **Given** more observations or events match than the requested result limit
- **When** I investigate the range
- **Then** each list is bounded and identifies its truncation while totals include every match
- **And** the reader can narrow to an observed interval and server

### Read an aggregate without inferring client disconnects

- **Surfaces:** `ui`
- **Given** a server reports seven slow-consumer counter increases, from clients or other connection types
- **When** I open Servers
- **Then** the total shows seven detections and does not identify seven unique clients or seven disconnects
- **And** the server page explains the same counter meaning and separates it from JetStream pending messages

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

### Read server details on a narrow screen

- **Surfaces:** `ui`
- **Given** a server with long names, tags and event summaries
- **When** I open its page at a narrow or wide window size
- **Then** the information cards fit the window, and Recent events follows them in a separate full-width row with wrapping text

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
