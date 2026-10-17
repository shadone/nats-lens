# Overview page

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [navigation.md](navigation.md), [triage-and-mutes.md](triage-and-mutes.md), [checks.md](checks.md), [kubernetes-context.md](kubernetes-context.md), [events.md](events.md), [design.md](../design.md)

## What it does

The Overview answers "is anything wrong, and what happened?" on one page. It leads with the worst current finding, not an average grade, then shows the system's key numbers, a status timeline of every check with the Kubernetes events above it, the newest events, and short lists of streams and servers. Everything on it follows the global time range.

## Behavior and rules

- **Worst finding first.** The top card shows the worst unmuted finding that fires now: its severity, code and name, the entity, the detail, when nats-lens first saw it, its transition state, and the cause event where the check names one. "Open finding" opens it, and "Mute…" opens the mute dialog. With nothing firing, the card reads "Nothing needs attention", with the number of findings resolved in the 24 hours before the range's end. When no finding resolved in those 24 hours and none fires muted, it reads "No check has reported a finding in this range. Checks may not have run yet; the status timeline below shows what ran."
- **The tally.** Beside the card, the counts of critical, warning, info and muted findings, and of findings resolved in the last 24 hours.
- **Other findings.** Below them, the other findings that fire, muted ones included and marked with who muted them, why and until when.
- **"Cluster now" tiles.** Nine tiles: Servers (answering out of expected, with the versions and the shortest uptime), Meta leader (the server more than half of the servers name, with the leader changes in the range), Streams (with how many are replicated and how many have a replica behind its leader; KV buckets and object stores not counted), KV Stores (with the buckets on one replica), Object Stores, Consumers (durable, with how many have a growing backlog), Accounts (with the account that has the most connections), Clients (connections, with the number of applications and the slow consumer events in the range) and Storage (JetStream storage, with the share of the servers' combined limit and the growth per day). Each tile shows its current value, a chart over the range with the drain and upgrade windows shaded, and a track from the range's minimum to its maximum with a mark at the current value. The storage tile shows how full the store is instead. Hovering a tile's label shows what its number counts and how it is measured.
- **A tile's colour is a finding.** A tile takes the status of the worst firing finding of the checks behind it, or muted when that finding is muted: Servers `server-001` to `server-006` and the system checks; Meta leader `system-002` and `system-003`; Streams, KV Stores and Object Stores the stream checks of their own kind of stream; Consumers every consumer check; Clients the client checks, `server-009`, `server-010`, `server-011` and `server-015`; Storage `server-007`, `server-014`, `stream-004` and `stream-006`. Accounts has no checks behind it and shows no data as its status. A tile is ok only when those checks ran, and shows no data without a value. A number that was not observed is shown as a dash, never as zero.
- **Applications are names.** The Clients tile counts the distinct connection names up to an `@`, so `catalog-api@pod-7f` and `catalog-api@pod-2c` are one application; nats-lens's own connections are left out, as the client checks leave them out.
- **The status timeline.** One row per check, one cell per time bucket, with the worst state of that bucket: ok, info, warning, critical, muted, or no data when the check did not run. Checks failing now are listed first, and a "now" column ends each row. A Kubernetes lane sits on top when the range holds Kubernetes events. "Show as table" gives the same content as text. On a narrow screen the cells scroll sideways and open at now.
- **The zoom strip.** When the range holds a node drain or upgrade window, a strip under the timeline shows the newest window minute by minute: Kubernetes events above the time axis, NATS events and finding transitions below.
- **Caps are stated.** The timeline draws at most the newest 1000 NATS events, 1000 finding transitions and 1000 Kubernetes events of the range, and says so when a range holds more.
- **Events.** The newest 40 events of the range, filtered by All, NATS, Kubernetes or Findings. The event that caused the worst finding opens by itself. A link leads to all events in the range.
- **Trend series in the API.** `GetTimeSeries` also serves the series behind the planned trends and tiles, which the page does not draw yet: `cluster.streams` (KV buckets and object stores not counted), `cluster.kv_buckets`, `cluster.object_stores`, `cluster.accounts` (accounts other than the system account with a connection or a stream), `cluster.meta_leader_changes`, `cluster.assets_replicated` and `cluster.assets_single` (streams and durable consumers by configured replicas; a consumer's replicas 0 means its stream's), and `consumer.delivered_rate` (from the leader's delivered sequence). A count needs a JSZ reply from every server that answered the collection, so a partial answer has no point rather than a dip. Accounts need an ACCSTATZ reply from every server instead; an account's streams count only when JSZ was complete too, so without JetStream accounts are counted by their connections. A tie between servers on the meta leader is no change. A rate has no value after a gap in collection of more than three intervals, and the delivered rate none at a leader move or a recreated consumer.
- **Tiles in the API.** `GetOverview` returns each tile with a one-sentence `description` for its tooltip. A tile whose value is a name sets `value_text`: the Meta leader tile names the leader there, and its `value` is the number of leader changes in the range. The meta leader is the server more than half of the expected servers report (the answering ones when none is expected), the rule `cluster.meta_leader_changes` uses; without such a majority `value_text` is empty.
- **Streams and servers.** The streams of the newest collection, worst first, with messages, bytes, consumers (present out of expected where the inventory names the stream), a 7-day bytes chart and growth per day. The servers with their version, uptime, role, healthz and the time of their last restart.

## Scenarios

### Start the day

- **Given** one stream finding fires as critical and two as warnings
- **When** I open the Overview
- **Then** the top card shows the critical finding, and the tally shows 1 critical and 2 warnings
- **And** the two warnings are listed below it

### Read a node upgrade off the timeline

- **Given** a node upgrade drained the NATS nodes within the range
- **When** I look at the status timeline
- **Then** the window is shaded and labelled, the Kubernetes lane shows the drains and pod restarts, and the zoom strip below shows the window minute by minute

### Count assets by replica count

- **Surfaces:** `api`
- **Given** a stream with 3 replicas and a durable consumer on it configured with replicas 0
- **When** I call `GetTimeSeries` for `cluster.assets_replicated`
- **Then** each point counts both: the consumer has its stream's 3 replicas

### Read the meta leader

- **Given** every server names `nats-1` as the JetStream meta leader and the leader did not change in the range
- **When** I open the Overview
- **Then** the Meta leader tile shows `nats-1`, with "0 leader changes in range"

### Get the same data from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API on 127.0.0.1:8080
- **When** I post `{}` to `/api/natslens.v1.LensService/GetOverview`
- **Then** I receive the cluster name, freshness, servers, the tally, the worst finding, the tiles with their series, and the windows to shade

## Planned

- **Health by area.** The nine tiles as boxes, each with its value at the top right, a state word, a short note, a chart, and the range track from minimum to maximum with a dot at the current value. An info button on each box shows its description. A box links to the page of its area.
- **Grouped causes.** Findings of one check with one cause appear as one item, with a table of the affected assets per server, "Copy nats CLI check" and "Accept as known…" ([checks.md](checks.md)). Accepted findings are summed in one line with a link to review them.
- **Trends on one time axis.** Charts of servers, connections, slow consumers, messages and bytes per second, CPU, memory and assets by replica count share one time axis. Hovering a point marks the same moment in every chart and in the lanes below.
- **What happened, in lanes.** Lanes for Kubernetes, servers (restarts, lame duck), leaders (meta, streams, consumers), clients (connects, drops) and findings on one clock. Dragging zooms in, and a click on a mark tells its story.
- **Busiest consumers.** The consumers with the highest delivery rate, with their backlog and unacknowledged messages. The API serves them already: `ListConsumers` sorted by delivery rate ([consumers.md](consumers.md)).
- **Placement at a glance.** The stream list shows one square per replica: leader, in sync, behind, disagreeing, offline or missing. The API serves the squares already, as each stream's `placement` ([streams.md](streams.md)).

## Not supported / out of scope

- **No rearranging.** The layout is fixed; tiles and sections cannot be moved or hidden.
- **No average health score.** The Overview leads with the worst finding instead.
- **One system at a time.** The page shows the system the address's `?system=` names, or the first configured; a switcher between systems is planned ([multiple-systems.md](multiple-systems.md)).
- **No per-subject traffic.** Message rates are per server and for the whole system, not per subject.
