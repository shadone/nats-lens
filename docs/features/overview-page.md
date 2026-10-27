# Overview page

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [navigation.md](navigation.md), [triage-and-mutes.md](triage-and-mutes.md), [checks.md](checks.md), [kubernetes-context.md](kubernetes-context.md), [events.md](events.md), [streams.md](streams.md), [consumers.md](consumers.md), [design.md](../design.md)

## What it does

The Overview answers "is anything wrong, and what happened?" on one page. A one-line status leads, then the system's nine key numbers, the findings that fire grouped by cause, trends over the range, what happened in lanes on one clock, the busiest consumers, and the streams with their replica placement. Everything on it follows the global time range.

## Behavior and rules

- **The status line.** One line: "Serving normally" while nothing fires or only info findings fire, "Needs attention" when a warning fires, "Critical findings" when a critical one fires; muted findings do not count. In a live range it reads "Not collecting · last data 11:29" while the collector's connection to NATS is down, or just "Not collecting" without a collection in the range, since the header already says since when; "Not collecting" when no collector writes or the newest collection is older than three collect intervals; and "No data yet" without a collection. Beside it, one badge per severity with the number of unmuted findings firing, worst first ("1 CRITICAL", "2 WARNING"). It does not repeat what the cards say.
- **Nine tiles.** Servers (answering out of expected, with the versions and the shortest uptime), Meta leader (the server more than half of the servers name, with the leader changes in the range), Streams (with how many of them are replicated and how many have a replica behind its leader; KV buckets and object stores not counted), KV Stores (with the buckets on one replica; "1 bucket", "2 buckets", and likewise for durables and connections), Object Stores, Consumers (durable, with how many have a growing backlog once `consumer-002` has run), Accounts (with the account that has the most connections), Clients (connections, with the number of applications, or "no named connections" when the servers list none, and the slow consumer events in the range) and Storage (JetStream storage across all replicas, with the share of the servers' combined limit and the growth per day; with no limit when any server does not report its own).
- **How a tile reads.** Each tile is a box: its title with an (i) button that shows what the number counts and how it is measured; the current value at the top right; a chart over the range with the drain and upgrade windows shaded; and a track from the range's minimum (left) to its maximum (right) with a dot at the current value. A value outside the range puts the dot at the nearer end. When the minimum equals the maximum the track reads "steady at N all window". When the data begins more than a bucket and a half after the range's start, as on a system nats-lens has watched for less than the range, the track says so: "steady at N since 05:30 UTC", or "since 05:30 UTC" between the minimum and the maximum, and the track's accessible name ends with the same words in place of the range. The note under the track is the API's subtitle, followed by the limit where the tile has one ("limit 150 GiB"); the limit never moves the track's ends. The Meta leader tile shows the leader's name with a chart of the leader changes and no track, since a name has no minimum or maximum; the chart's accessible name gives the leader's name as its current value, as do other tiles whose value is text.
- **A tile's colour is a finding.** A tile takes the status of the worst firing finding of the checks behind it, or muted when that finding is muted: Servers `server-001` to `server-006` and the system checks; Meta leader `system-002` and `system-003`; Streams, KV Stores and Object Stores the stream checks of their own kind of stream; Consumers every consumer check; Clients the client checks, `server-009`, `server-010`, `server-011` and `server-015`; Storage `server-007`, `server-014`, `stream-004` and `stream-006`. Accounts has no checks behind it: the API reports no data as its status, and a tile with a value and no check reads as OK in the UI. A warning or critical tile colours its value and dot and shows the word beside the value; an info tile only tints its dot, with no word and no coloured value, since info is a notice that never changes an OK label.
- **No zeros for unknowns.** A value that was not observed is a dash with no dot, never zero; with a chart but no current value the tile says "no current value". A tile's line leaves out a count it cannot know: the stream, KV and object store lines appear only with their count.
- **Applications are names.** The Clients tile counts the distinct connection names up to an `@`, so `catalog-api@pod-7f` and `catalog-api@pod-2c` are one application; nats-lens's own connections are left out, as the client checks leave them out. Connections without a name are not counted; the [Applications](applications.md) page lists them as `(unnamed)`.
- **Cause cards.** The unmuted findings that fire are grouped into cards: findings that name the same cause event form one card; the others group by check and parent entity, where a consumer's parent is its stream, a stream's its account, and anything else's the system. So four `consumer-009` findings on consumers of `order-jobs` are one card "consumer-009 × 4", "4 consumers on stream order-jobs". A card names a stream or account only when every entity in it has that parent; a cause that reaches consumers of two streams reads "2 consumers on 2 streams". Cards are ordered by their worst severity, then among warnings a card with a finding that is not an expiry before a card of expiry warnings only (`account-003`, `account-004`, `server-016`), then by size. A card with one finding is one compact row: severity, code and name, the detail, the entity and cause beside them, and "Open finding" and "Mute…" (named for the check and entity); a card with several lists each entity with its detail, Open and Mute… (at most 8, then "N more" to the Findings page). A cause names its event kind, time and summary, and links to the event. Text the API sends (check names, details, cause summaries) is shown as sent.
- **Muted findings.** Below the cards one strip lists the muted findings, grouped by reason: the reason in quotes, who muted, and a link to each finding.
- **Trends.** Eight charts over the range on one time axis: Servers answering; Connections; Slow consumers as bars per bucket, with the total in the range; Messages and Bytes per second, in as a solid line and out dashed, with the current values as "in / out" ("1.45 / 4.59 MiB/s", or "in 900 B/s · out 2 MiB/s" when the units differ); CPU and Memory as a band from the lowest to the highest server, the median as a line and the busiest server (highest mean over the range) dashed and named; and Assets, streams and durable consumers stacked as replicated below and one replica above. Each chart starts at zero, except Memory, which spans its own minimum to maximum with a small margin so that its changes show; each breaks its line at a gap of more than two buckets (a lone point between gaps is a dot, a lone band point a short mark however narrow its band), and states its minimum and maximum (or "steady at N"); its legend draws each series' own stroke, dash or fill. A chart's accessible name carries its current value, the time it covers and its range.
- **Timeline lanes.** Three lanes on one clock from `GetStatusTimeline`: Kubernetes (events as ticks, deployments wider, drain and upgrade windows shaded), Findings (the worst state of any check per bucket, as coloured runs; buckets where every check was ok are empty; under its name, how many checks fired in the range, info included: "2 checks fired") and Events (NATS events, warnings and critical ones in their colour). The time axis under the lanes drops labels that would crowd each other or "now", as on a phone. Each lane's name links to its list, and each lane has an accessible name that says what it shows ("Findings: warning 09:15–09:45, …"). A legend draws each mark. "Each check" opens the status timeline in the same card: one row per check, one cell per bucket with the worst state (ok, info, warning, critical, muted, or no data when the check did not run), checks failing now first, a "now" column, and "Show as table" for the same content as text; on a narrow screen its cells scroll sideways and open at now.
- **The zoom strip.** When the range holds a node drain or upgrade window, a strip under the lanes shows the newest window minute by minute: Kubernetes events above the time axis, NATS events and finding transitions below.
- **Caps are stated.** The lanes draw at most the newest 1000 NATS events, 1000 finding transitions and 1000 Kubernetes events of the range, and say so when a range holds more. The cause cards read at most 500 findings and say so when more fire.
- **Busiest consumers.** The 6 consumers with the highest delivery rate over the 15 minutes before the range's end, read from each consumer's leader, with pending and unacknowledged messages and a sparkline of the delivery rate over the range. A consumer without a rate shows a dash. The list follows the account scope.
- **Streams with placement.** The streams of the newest collection, worst first, at most 10 with a link to all: one square per replica (leader, in sync, behind, disagreeing, offline or missing, each a different shape), messages, size and growth per day. A legend draws the states that occur. The list follows the account scope.
- **Trend series in the API.** `GetTimeSeries` serves the series behind the trends and tiles: `cluster.streams` (KV buckets and object stores not counted), `cluster.kv_buckets`, `cluster.object_stores`, `cluster.accounts` (accounts other than the system account with a connection or a stream), `cluster.meta_leader_changes`, `cluster.assets_replicated` and `cluster.assets_single` (streams and durable consumers by configured replicas; a consumer's replicas 0 means its stream's), `cluster.cpu_percent` and `cluster.mem_bytes` as bands, and `consumer.delivered_rate` (from the leader's delivered sequence). A count needs a JSZ reply from every server that answered the collection, so a partial answer has no point rather than a dip. Accounts need an ACCSTATZ reply from every server instead; an account's streams count only when JSZ was complete too, so without JetStream accounts are counted by their connections. A tie between servers on the meta leader is no change. A rate has no value after a gap in collection of more than three intervals, and the delivered rate none at a leader move or a recreated consumer.
- **Several series in one call.** `GetTimeSeriesBatch` reads up to 32 `GetTimeSeries` queries, each a metric, an entity and optionally its own `max_points`, over one range resolved once, so every series ends at the same instant. Results come in query order. A query that `GetTimeSeries` would refuse as `invalid_argument` or `not_found` gets the error text in its result and the others are served; any other failure fails the call, as do zero queries or more than 32. The call may take 40 seconds, each query 30 within them, which keeps it under the web's 45-second call timeout; a query that takes longer than its 30 seconds fails the whole call as `deadline_exceeded`. The Overview reads its trends in one such call and the busiest consumers' sparklines in another, so a chart whose query is refused (unknown metric or entity) shows its own error while the others draw.
- **Tiles in the API.** `GetOverview` returns each tile with a one-sentence `description` for its tooltip. A tile whose value is a name sets `value_text`: the Meta leader tile names the leader there, and its `value` is the number of leader changes in the range. The meta leader is the server more than half of the expected servers report (the answering ones when none is expected), the rule `cluster.meta_leader_changes` uses; without such a majority `value_text` is empty.

## Scenarios

### Start the day

- **Given** one stream finding fires as critical and two as warnings
- **When** I open the Overview
- **Then** the status line reads "Critical findings" with "1 CRITICAL" and "2 WARNING"
- **And** the critical finding's card comes first, the two warnings below it

### Read four findings as one

- **Given** `consumer-009` fires as info for four consumers of `order-jobs`
- **When** I open the Overview
- **Then** one card reads "consumer-009 × 4" and "4 consumers on stream order-jobs", listing each consumer with Open and Mute…
- **And** the status line still reads "Serving normally", with "4 INFO"

### Read a node upgrade off the timeline

- **Given** a node upgrade drained the NATS nodes within the range
- **When** I look at the timeline
- **Then** the window is shaded in the Kubernetes lane with the drains and pod restarts as ticks, the Findings lane shows what fired meanwhile, and the zoom strip below shows the window minute by minute

### See a tile that was not observed

- **Given** the newest epoch has no value for a tile but the range has a chart for it
- **When** I open the Overview
- **Then** the tile shows a dash, its track has no dot and it says "no current value"

### Count assets by replica count

- **Surfaces:** `api`
- **Given** a stream with 3 replicas and a durable consumer on it configured with replicas 0
- **When** I call `GetTimeSeries` for `cluster.assets_replicated`
- **Then** each point counts both: the consumer has its stream's 3 replicas

### Read the meta leader

- **Given** every server names `nats-1` as the JetStream meta leader and the leader did not change in the range
- **When** I open the Overview
- **Then** the Meta leader tile shows `nats-1`, with "0 leader changes in range" under a flat chart

### Read a system watched for less than the range

- **Given** the demo system `example-staging`, which nats-lens began watching nine hours ago, at 05:30 UTC
- **When** I open its Overview over the last 24 hours
- **Then** a tile whose value did not change reads "steady at N since 05:30 UTC", not "all window", and the other tiles show "since 05:30 UTC" between their minimum and maximum

### Get the same data from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API on 127.0.0.1:8080
- **When** I post `{}` to `/api/natslens.v1.LensService/GetOverview`
- **Then** I receive the cluster name, freshness (with whether the collector collects), servers, the tally, the worst finding, the tiles with their series, and the windows to shade

### Read several series in one call

- **Surfaces:** `api`
- **Given** `natslens demo` serves its API on 127.0.0.1:8080
- **When** I post `{"queries": [{"metric": "cluster.connections"}, {"metric": "cluster.no_such_metric"}]}` to `/api/natslens.v1.LensService/GetTimeSeriesBatch`
- **Then** the first result holds the connections series, the second an `error` naming the unknown metric, and `range` the one range both were read for

## Planned

- **Tiles link to their pages.** A box opens the page of its area.
- **Cause evidence.** A table of the affected assets per server in a cause card, "Copy nats CLI check" and "Accept as known…" ([checks.md](checks.md)).
- **One hover across charts.** Hovering a point marks the same moment in every chart and in the lanes. Dragging over the lanes zooms in, and a click on a mark tells its story.
- **More lanes.** Servers (restarts, lame duck), leaders (meta, streams, consumers) and clients (connects, drops) as lanes of their own.
- **Findings by account.** The cause cards follow the account scope.

## Not supported / out of scope

- **No rearranging.** The layout is fixed; tiles and sections cannot be moved or hidden.
- **No average health score.** The Overview leads with what fires instead.
- **One system at a time.** The page shows the system the address's `?system=` names, or the first configured; the Fleet page compares systems ([multiple-systems.md](multiple-systems.md)).
- **No per-subject traffic.** Message rates are per server and for the whole system, not per subject.
- **No stories made up.** The lanes show stored events and windows; they do not infer that a group of events was "a redeploy".
