# Overview page

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [data-history.md](data-history.md), [navigation.md](navigation.md), [triage-and-mutes.md](triage-and-mutes.md), [checks.md](checks.md), [kubernetes-context.md](kubernetes-context.md), [events.md](events.md), [streams.md](streams.md), [consumers.md](consumers.md), [design.md](../design.md)

## What it does

The Overview answers "is anything wrong, and what happened?" on one page. A one-line status leads, then the system's nine key numbers, the findings that fire grouped by cause, trends over the range, what happened in lanes on one clock, the busiest consumers, and the streams with their replica placement. Everything on it follows the global time range.

## Behavior and rules

- Supported server and aggregate metric titles and selected intervals lead to [metric investigations](metric-investigations.md), preserving system, range and reported server scope. Storage and detections reuse specialist pages.


- [Metric observation summaries](metric-observations.md) expose opt-in pre-bucket metric statistics through the API and read-only client. Existing charts and their title destinations are unchanged by this API foundation.

- [Chart interval selection](chart-interval-selection.md) adds explicit storage-time selection and actions. Selection alone leaves navigation and the page range unchanged.

- **Chart descriptions.** Chart and mini-chart descriptions use [shared chart help](chart-help.md). Consumer history help lives in the Consumer header, leaving Range plots full-width.

The worst-streams table requests one bounded page; its count and totals cover all matching streams, including later pages ([Streams](streams.md)).

- **Replay observed windows.** Each Kubernetes window has a separate "Replay this window" link below the timeline. It fixes A, B and the range around that observation, preserves the system and leaves the lane, Each check and zoom interactions intact. Context and limits follow [Replay](replay.md).

- **Summary and detail.** "System summary" labels the compact cards, and its note says once for all of them that the figures are the latest available counts and reports. Counts come from the latest available bucket, Storage uses newest server reports, and Meta leader names the newest collection's leader; while its chart shows, a caption reads "Leader changes per bucket", since the chart counts changes rather than naming leaders. "Detailed trends" adds traffic rates, event totals, server spread, and replica counts over the selected range. Both sections retain useful history; the summary is not a claim that every figure comes from one snapshot.

- **Inspect chart values.** Compact and expanded charts show UTC bucket times and formatted values on hover or touch. Touch keeps the selected bucket when the chart receives focus. Focus a chart and use arrow keys, Home, or End for the same readout; Escape or blur dismisses it. Missing buckets say "No sample" without interpolation. Bucket descriptions distinguish last counts, mean rates, event totals, and server-band aggregation. The busiest-consumer mini charts use the same inspector. Readouts escape narrow chart, table, and card clipping in a viewport-bounded overlay, with room for normal multiline text. Their position follows scrolling and resizing, including clipped table regions. Long content scrolls inside the overlay and Page Up/Page Down work from the focused trigger. Hovering the overlay keeps it readable. Multiple lines have separate labels, bands show lower and upper bounds, and stacked areas show each component's count. The Meta leader chart reports leader changes per bucket; it does not infer the leader's historical name. Without a majority naming the newest leader, its headline reads "Unknown"; the range total stays in the subtitle.

- **Consumer prompt actions.** In Busiest consumers, the copy action floats in the Consumer cell on row hover or keyboard focus; it stays visible on touch devices. It reserves no Range width and retains the chart’s selected point. Other shared charts place floating actions outside plot pixels without a separate action column. Context and clipboard behavior follow [agent investigations](agent-investigations.md).

- **Chart destinations.** Tile and trend headers link to the relevant list. The link covers the box outside chart inspection and other controls. Metric explanations use the shared viewport overlay, including narrow cards and the second column on phones. Links keep the system and range, and the account on destinations that apply account scope.

- **Acknowledged findings.** Cause cards show the [acknowledgement mark](mobile.md), and the status line counts acknowledged findings. At phone width, cause cards precede the health tiles.

- **History behind the figures.** A short-history note links to Data history. Trend and tile charts retain the requested axis and hatch collection gaps and time before history, with the limits described in [data-history.md](data-history.md). Message and connection trends, the Clients tile, and Storage name the shorter span. Storage growth follows the [Streams bounded observation policy](streams.md), independently of chart resolution. Storage explains missing growth with the six-hour requirement; unresolved cause cards identify findings already true at the history start.

- **The status line.** One line: "Serving normally" while nothing fires or only info findings fire, "Needs attention" when a warning fires, "Critical findings" when a critical one fires; muted findings do not count. In a live range it reads "Not collecting · last data 11:29" while the collector's connection to NATS is down, or just "Not collecting" without a collection in the range, since the header already says since when; "Not collecting" when no collector writes or the newest collection is older than three collect intervals; and "No data yet" without a collection. Beside it, one badge per severity with the number of unmuted findings firing, worst first ("1 CRITICAL", "2 WARNING"). It does not repeat what the cards say.
- **Nine tiles.** Servers (answering out of expected, with the versions and the shortest uptime), Meta leader (the server more than half of the servers name, with the leader changes in the range), Streams (with how many of them are replicated and how many have a replica behind its leader; KV buckets and object stores not counted), KV Stores (with the buckets on one replica; "1 bucket", "2 buckets", and likewise for durables and connections), Object Stores, Consumers (durable, with how many have a growing backlog once `consumer-002` has run), Accounts (with the account other than the system account that has the most connections, counted as the Accounts page counts them: client connections plus the leaf nodes counted against the connection limit, so the tile names the account the page lists first, the first by name of a tie; proposed, not yet confirmed), Clients (connections, with the number of applications, or "no named connections" when the servers list none, and the slow consumer events in the range) and Storage (JetStream storage across all replicas, with the share of the servers' combined limit and the growth per day; with no limit when any server does not report its own).
- **How a tile reads.** Each tile is a box: its title with an (i) button that shows what the number counts and how it is measured; the current value at the top right; a chart over the range with the drain and upgrade windows shaded, when the tile has one (next rule); and a track from the range's minimum (left) to its maximum (right) with a dot at the current value. A value outside the range puts the dot at the nearer end. When the minimum equals the maximum the track reads "steady at N all window". When the data begins more than a bucket and a half after the range's start, as on a system nats-lens has watched for less than the range, the track says so: "steady at N since 05:30 UTC", or "since 05:30 UTC" between the minimum and the maximum, and the track's accessible name ends with the same words in place of the range. The note under the track is the API's subtitle, followed by the limit where the tile has one ("limit 150 GiB"); the limit never moves the track's ends. The Meta leader tile shows the leader's name with a chart of the leader changes, when it has one, and no track, since a name has no minimum or maximum; the chart's accessible name gives the leader's name as its current value, as do other tiles whose value is text.
- **A chart only where something changed.** A tile draws its chart when its value changed in the range (any bucket differs, a count dipped or rose and came back inside one bucket, or the newest report lies outside the buckets; for Meta leader, any bucket holds a leader change), when a warning or critical finding colours it, or when the range is not fully covered: no data, a missing bucket (the bucket holding the range's start or the history start, any bucket between, or the bucket holding the range's end, which a live range may still be filling), or a collection gap inside the range (not running, not connected, no collections, or of a kind the UI does not know). The gaps load with the Overview itself. While they are loading or could not be read, a tile whose values do not vary has no chart and its steady line reads "History gaps loading" or "History gaps unavailable" in place of "steady at N", so it never claims to be steady; when the gaps arrive it reads steady, or draws its chart if a gap overlaps the range. Otherwise the tile is steady: it shows its title, its value, "steady at N all window" (or "since 05:30 UTC") and its note under the heading, without a chart. A row of steady tiles is shorter. A series that begins at the history start is steady, as the track already says since when; a single bucket that covers the range is steady. Storage keeps its chart while its size moves, which is nearly always. Info and muted findings do not keep a chart, as they do not colour the tile.
- **Chart colours mark the change.** A tile's chart draws its line and fill in the neutral text colour and puts the accent only on the segments where the value changes: the dip and recovery of Servers, the spikes of Meta leader. A line that changes in most of its buckets, as Storage's slow growth does, stays neutral with one accent dot on its newest point. That dot stays round on narrow and wide charts, including in Safari. A count bucket whose lowest or highest value lies beyond what the line draws around it (below the lowest, or above the highest, of its own value and its two neighbours' values, its own value standing in for a missing neighbour) by at least a quarter of the span from the chart's lowest to its highest value (lows and highs included), and by more than twice the median of the other buckets' moves, gets a tick from its lowest to its highest value, as a dip that came back inside one bucket does, and as an outage inside a busy count does; a dip the line already reaches, a small move, and a count whose every bucket swings about as far get none. The chart's height spans its values and the ticked buckets' lowest and highest values; the track's ends still give the lowest and highest of every bucket. A bucket whose lowest or highest differs from its value names that range in its readout ("3 (2 to 3 in bucket)"). The tile's track takes the buckets' lowest and highest values into its ends. A warning or critical tile keeps its line and fill neutral too and draws the change, the newest-point dot and the ticks in its severity colour (Consumers' drop in critical, Clients in warning); the value and the range track's dot keep the status colour as well (the dot in the mark amber). The grey line stops just short of each change mark, leaving a thin gap that shows whatever lies behind it, so the mark stands apart from the line by lightness as well as hue. Warning marks, and a warning tile's track dot, use a darker amber than the warning cells, so that in the light theme they reach 3:1 on the card. Both were decided by common practice at the owner's request of 2026-11-07.
- **A tile's colour is a finding.** A tile takes the status of the worst firing finding of the checks behind it, or muted when that finding is muted: Servers `server-001` to `server-006` and `system-001` to `system-006` (`system-007` to `system-009` judge nats-lens itself and colour no tile); Meta leader `system-002` and `system-003`; Streams, KV Stores and Object Stores the stream checks of their own kind of stream; Consumers every consumer check; Clients the client checks, `server-009`, `server-010`, `server-011` and `server-015`; Storage `server-007`, `server-014`, `stream-004` and `stream-006`. Accounts has no checks behind it: the API reports no data as its status, and a tile with a value and no check reads as OK in the UI. A warning or critical tile colours its value and dot and shows the word beside the value; an info tile only tints its dot, with no word and no coloured value, since info is a notice that never changes an OK label.
- **No zeros for unknowns.** A value that was not observed is a dash with no dot, never zero; with a chart but no current value the tile says "no current value". A tile's line leaves out a count it cannot know: the stream, KV and object store lines appear only with their count.
- **Applications are names.** The Clients tile counts the distinct connection names up to an `@`, so `catalog-api@pod-7f` and `catalog-api@pod-2c` are one application; nats-lens's own connections are left out, as the client checks leave them out. Connections without a name are not counted; the [Applications](applications.md) page lists them as `(unnamed)`.
- **Cause cards.** The unmuted findings that fire are grouped into cards: findings that name the same cause event form one card; the others group by check and parent entity, where a consumer's parent is its stream, a stream's its account, and anything else's the system. So four `consumer-009` findings on consumers of `order-jobs` are one card "consumer-009 × 4", "4 consumers on stream order-jobs". A card names a stream or account only when every entity in it has that parent; a cause that reaches consumers of two streams reads "2 consumers on 2 streams". Cards are ordered by their worst severity, then among warnings a card with a finding that is not an expiry before a card of expiry warnings only (`account-003`, `account-004`, `server-016`), then by size. A card with one finding is one compact row: severity, code and name, the detail, the entity and cause beside them, and "Open finding" and "Mute…" (named for the check and entity); a card with several lists each entity with its detail, Open and Mute… (at most 8, then "N more" to the Findings page). A cause names its event kind, time and summary, and links to the event. Text the API sends (check names, details, cause summaries) is shown as sent.
- **Muted findings.** Below the cards one strip lists the muted findings, grouped by reason: the reason in quotes, who muted, and a link to each finding.
- **Trends.** Eight charts over the range on one time axis: Servers answering; Connections; Slow consumers as bars per bucket, with the total in the range; Messages and Bytes per second, in as a solid line and out dashed, with the current values as "in / out" ("1.45 / 4.59 MiB/s", or "in 900 B/s · out 2 MiB/s" when the units differ); CPU and Memory as a band from the lowest to the highest server, the median as a line and the busiest server (highest mean over the range) dashed and named; and Assets, streams and durable consumers stacked as replicated below and one replica above. Each chart starts at zero, except Memory, which spans its own minimum to maximum with a small margin so that its changes show; each breaks its line at a gap of more than two buckets (a lone point between gaps is a dot, a lone band point a short mark however narrow its band), and states its minimum and maximum (or "steady at N"); its legend draws each series' own stroke, dash or fill. A chart's accessible name carries its current value, the time it covers and its range.
- **Timeline mark previews.** Hover or focus event ticks, finding boxes, per-check cells and zoom marks to read labels and full UTC times in a viewport popover. Activation opens a refresh-stable full-screen investigation with Close, Escape and focus return. Switching systems closes the selection. Finding boxes and per-check cells list retained occurrences for their fixed interval in explicit 50-row pages: repeated firings of the same finding remain separate. The Timeline transition boundary excludes later-published occurrences, including when the captured history was empty. “Open this occurrence” opens its exact retained identity and bounded transition history, separately from the latest finding. Retention never substitutes a newer recurrence. Current-only configuration, operator state and condition onset are unavailable in historical evidence. See [triage](triage-and-mutes.md) and the [occurrence contract](../reference/finding-occurrences.md). Recorded event IDs retain event links; inferred windows retain their available explanation. Overlapping-event contributor retrieval, complete-window evidence and bounded captured-group sharing are described below; whole-Timeline acceptance is tracked separately. The bounded occurrence policy is proposed, not yet confirmed.
- **Timeline lanes.** Three lanes on one clock from `GetStatusTimeline`: Kubernetes (events as ticks, deployments wider, drain and upgrade windows shaded), Findings (the worst state of any check per bucket, as coloured runs; observed OK buckets stay empty while buckets with no observed check state have dashed inspectable boxes; under its name, how many returned checks fired in the range, info included: "2 checks fired", or "no observed state" when none is available) and Events (NATS events, warnings and critical ones in their colour). The time axis under the lanes drops labels that would crowd each other or "now", as on a phone. Each lane's name links to its list, and each lane has an accessible name that says what it shows ("Findings: warning 09:15–09:45, …"). A legend draws each mark. "Each check" opens the status timeline in the same card: one row per check, one cell per bucket with the worst state (ok, info, warning, critical, muted, or no data when the check did not run), checks ordered by the returned summary, a "Range summary" column, and "Show as table" for dated state runs, inspectable Kubernetes records and inferred-window rows. Expanded check labels and table check codes open retained occurrence history with the captured range, transition boundary and typed resource; the expanded Kubernetes label opens its fixed captured event feed. Table event/window actions use the same hover/focus and persistent-detail pattern as lane marks, retain source limitations and only load details on activation. The summary uses selected-range evidence with current mute rules; it is not an independent current-state read. Retained occurrence details separately identify the latest linked finding state. On a narrow screen its cells scroll sideways and open at the range end.
- **The zoom strip.** When the range holds a node drain or upgrade window, a strip under the lanes shows the newest window minute by minute: Kubernetes events above the time axis, NATS events and finding transitions below.
- **Caps are stated.** The lanes draw at most the newest 1000 NATS events, 1000 finding transitions and 1000 Kubernetes events of the range, and say so when a range holds more. The cause cards read at most 500 findings and say so when more fire.
- **Busiest consumers.** The 6 consumers with the highest delivery-sequence rate in their newest comparable leader/creation run within the 15 minutes before the range's end, with pending and unacknowledged messages and a sparkline of the delivery rate over the range. A consumer without two comparable reports in that newest run shows a dash. Redeliveries can advance delivery sequences, so this is not unique-message throughput. Observed unknown, conflicting or changed identity stops the run; a return to an earlier identity cannot rejoin its old run. The list follows the account scope.
- **Streams with placement.** The streams of the newest collection, worst first, at most 10 with a link to all: one square per replica (leader, in sync, behind, disagreeing, offline or missing, each a different shape), messages, size and growth per day. A legend draws the states that occur. The list follows the account scope.
- **Trend series in the API.** `GetTimeSeries` serves the series behind the trends and tiles: `cluster.streams` (KV buckets and object stores not counted), `cluster.kv_buckets`, `cluster.object_stores`, `cluster.accounts` (accounts other than the system account with a connection or a stream), `cluster.meta_leader_changes`, `cluster.assets_replicated` and `cluster.assets_single` (streams and durable consumers by configured replicas; a consumer's replicas 0 means its stream's), `cluster.cpu_percent` and `cluster.mem_bytes` as bands, and `consumer.delivered_rate` (from the leader's delivered sequence). A count that shows each bucket's last value also carries the bucket's lowest and highest value in the point's `low` and `high`, which a band metric uses for its servers; the series' minimum and maximum stay those of the bucket values. A count needs a JSZ reply from every server that answered the collection, so a partial answer has no point rather than a dip. Accounts need an ACCSTATZ reply from every server instead; an account's streams count only when JSZ was complete too, so without JetStream accounts are counted by their connections. A tie between servers on the meta leader is no change. A rate has no value after a gap in collection of more than three intervals, and the delivered rate none at a leader move or a recreated consumer.
- **Capacity byte metrics.** [Capacity](capacity.md) reads file-store and resource histories through the same batch API. Its server-store histories preserve missing or ambiguous reports through bucket reduction; observed replica bytes sum reported resource bytes without estimating missing copies.
- **Several series in one call.** `GetTimeSeriesBatch` reads up to 32 `GetTimeSeries` queries, each a metric, an entity and optionally its own `max_points`, over one range resolved once, so every series ends at the same instant. Results come in query order. A query that `GetTimeSeries` would refuse as `invalid_argument` or `not_found` gets the error text in its result and the others are served; any other failure fails the call, as do zero queries or more than 32. The call may take 40 seconds, each query 30 within them, which keeps it under the web's 45-second call timeout; a query that takes longer than its 30 seconds fails the whole call as `deadline_exceeded`. The Overview reads its trends in one such call and the busiest consumers' sparklines in another, so a chart whose query is refused (unknown metric or entity) shows its own error while the others draw.
- **Tiles in the API.** `GetOverview` returns each tile with a one-sentence `description` for its tooltip. A tile whose value is a name sets `value_text`: the Meta leader tile names the leader there, and its `value` is the number of leader changes in the range. The meta leader is the server more than half of the expected servers report (the answering ones when none is expected), the rule `cluster.meta_leader_changes` uses; without such a majority `value_text` is empty.

- **Acknowledged trouble stays visible.** Cause cards show the current acknowledgement's author, time, note, and quiet state. The status line counts acknowledged firing findings, including muted ones. Acknowledging leaves severity counts unchanged; muting still removes a finding from unmuted status. On phones, the status line and cause cards precede the two-column health tiles and timeline.
- **Captured event evidence.** Timeline events open their retained record inside the refresh-stable investigation. Source, kind, reason, observed/received UTC times, named resources and an explicitly requested payload remain separate from the latest linked finding state. Colliding 32-pixel hit regions in lanes become a counted contributor control; zoom marks at the same timestamp and side are grouped. Groups describe visual overlap, never a causal incident. Each explicit page reads at most 50 exact identities; missing rows retain their captured label when available. Mark details and window pages support Copy as prompt.
- **Inferred-window support.** A detected window includes its grouping rule and captured input identities. The complete window route fixes the original query interval and event membership boundary. Reopening reconstructs the exact identity or reports it unavailable; it never substitutes a new window. Supporting records are selected by ID, including inputs outside the displayed interval. The window is system-wide context even when opened with account/resource navigation. A boundary prevents newer event membership; it does not freeze mutable payloads, linked findings, retention or every Timeline fact. See [Timeline event evidence](../reference/timeline-event-evidence.md).

- **Historical lane destinations.** The Findings lane opens a dedicated retained-occurrence list, preserving the fixed selected range and captured transition boundary, including known-empty zero. Per-check interval details offer the same all-results route with typed resource/check scope. Repeated firings stay separate after resolution; exact occurrence links keep that identity. The Events lane includes NATS, finding and collector sources; both event lanes show routine records instead of silently hiding or folding marked evidence. Feed destinations retain event membership and are broader source/range context than one overlap group.

- **Capture limits stay with selection.** Event-lane previews, single/group investigations and both zoom layouts retain the source Timeline's known input-limit warnings. Selected warnings stay as captured when the Timeline refreshes. NATS/collector, finding-transition and Kubernetes limits remain distinct; a source-range cut does not prove that a specific group or zoom window lost members. It does not turn a separately derived window input set or a per-check state row into a truncated one. Selected event prompt annotations retain the same warning. False flags do not claim complete capture.

## Scenarios

### Understand the two chart sections

- **Surfaces:** `ui`
- **Given** Overview shows its compact cards and expanded charts
- **When** I read the section labels and notes
- **Then** System summary's note says once that its figures are the latest available counts and reports, and no tile repeats it
- **And** Detailed trends explains the traffic, event, server spread, and replica-count history

### Inspect a chart bucket

- **Surfaces:** `ui`
- **Given** a chart has samples separated by missing buckets
- **When** I hover, touch, or focus the chart and move with arrow keys
- **Then** the readout names the bucket's UTC time and each series with its unit
- **And** a missing bucket says "No sample" rather than carrying the previous value forward

### Open a chart's area

- **Surfaces:** `ui`
- **Given** Overview shows Streams and Clients for a selected system and range
- **When** I activate a chart header by pointer or keyboard
- **Then** Streams opens Streams and Clients opens Connections with the same system and range
- **And** Connections keeps the account scope, and info buttons remain independent

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

### Read a steady tile

- **Surfaces:** `ui`
- **Given** the demo system `example-prod` over the last 24 hours, where 8 streams were present in every bucket and Servers dropped to 2 for one bucket
- **When** I open the Overview
- **Then** the Streams tile shows 8, "steady at 8 all window" and its note, without a chart
- **And** the Servers tile draws its chart in the neutral colour with the dip and recovery in the accent

### See a dip inside one bucket

- **Surfaces:** `ui`, `api`
- **Given** the demo system `example-prod` over the last 7 days, where Servers dropped to 2 inside one bucket and every bucket's last value is 3
- **When** I open the Overview
- **Then** the Servers tile keeps its chart with a tick from 2 to 3 at that bucket and its track reads from 2 to 3, and `GetTimeSeries` gives the bucket a `low` of 2 and a `high` of 3

### See a steady tile with a collection gap

- **Surfaces:** `ui`
- **Given** the demo system `example-staging`, whose collector stopped collecting at 11:30 UTC, over the last 24 hours
- **When** I open the Overview
- **Then** every tile keeps its chart with the time without collections hatched, though its value did not change

### Open the Overview while its history gaps load

- **Surfaces:** `ui`
- **Given** the range's history gaps have not arrived yet, or could not be read
- **When** I open the Overview
- **Then** a tile whose value did not change shows its value without a chart and reads "History gaps loading" or "History gaps unavailable" where "steady at N" goes, and a tile whose value changed draws its chart
- **And** once gaps arrive and none overlaps the range, the steady tile reads "steady at N all window"

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
- **Then** the Meta leader tile shows `nats-1` and "0 leader changes in range", without a chart

### See which account has the most connections

- **Given** `APP` has 30 client connections, and `SUPPLIERS` has 20 client connections and 15 leaf node connections on `nats-0`, its most on one server
- **When** I open the Overview
- **Then** the Accounts tile reads "most connections: SUPPLIERS (35)", and the Accounts page lists `SUPPLIERS` first with 35 connections

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

- **Cause evidence.** A table of the affected assets per server in a cause card, "Copy nats CLI check" and "Accept as known…" ([checks.md](checks.md)).
- **One hover across charts.** Hovering a point marks the same moment in every chart and in the lanes. Dragging over the lanes zooms in, and a click on a mark tells its story.
- **More lanes.** Servers (restarts, lame duck), leaders (meta, streams, consumers) and clients (connects, drops) as lanes of their own.
- **Findings by account.** The cause cards follow the account scope.

### Read an Overview with short history

- **Surfaces:** `ui`
- **Given** the system has only 31 minutes of history
- **When** I open the Overview over 24 hours
- **Then** the charts keep the full range with time before history hatched, and Storage explains how much history growth needs

### See who is investigating

- **Surfaces:** `ui`
- **Given** an unmuted critical finding has an acknowledgement
- **When** an operator opens Overview
- **Then** its cause card shows the acknowledgement and the status line counts it
- **And** the finding still contributes to the critical status

## Not supported / out of scope

- **No rearranging.** The layout is fixed; tiles and sections cannot be moved or hidden.
- **No average health score.** The Overview leads with what fires instead.
- **One system at a time.** The page shows the system the address's `?system=` names, or the first configured; the Fleet page compares systems ([multiple-systems.md](multiple-systems.md)).
- **No per-subject traffic.** Message rates are per server and for the whole system, not per subject.
- **No stories made up.** The lanes show stored events and windows; they do not infer that a group of events was "a redeploy".

### Inspect colliding event marks and window support

- **Given** multiple captured observations share a lane hit region or an inferred window
- **When** I activate its counted control or window
- **Then** I can page through the exact captured input identities, read a retained record and explicitly open its payload
- **And** missing rows, capture limits, unknown IDs and noncausal grouping are disclosed
- **And** a shared window link keeps its fixed interval and boundary rather than following the live clock

### Open historical all-results evidence

- **Given** the Timeline contains resolved and repeated findings or finding/collector event marks
- **When** I activate its lane label or a per-check interval's all-results link
- **Then** the destination preserves the fixed range and captured membership rather than opening the live compatibility finding list
- **And** repeated finding occurrences stay separate, and all displayed event sources remain discoverable without routine hiding

### Keep capture limitations while investigating

- **Given** the source limits event inputs for a Timeline lane
- **When** I inspect a mark, open its group or zoom detail, and the Timeline later refreshes
- **Then** the selected evidence keeps the original source warning and names the affected input family
- **And** a copied event prompt carries that limitation without claiming which observations are missing

### Share a captured event group

- **Given** overlapping Timeline marks retain exact event IDs and an explicit system
- **When** I choose **Open all captured events**
- **Then** a separate fixed-context page retains the captured membership and source-limit notices, with explicit summary paging and on-demand record detail
- **And** sharing its full URL or copying its prompt retains the selected page without turning the group into a causal incident or complete event feed

This bounded captured-group policy is proposed, not yet confirmed; see [Events](events.md#captured-timeline-selections).

### Inspect alternative Timeline views

- **Given** I expand **Each check** or choose **Show as table**
- **When** I follow a check label or activate a table event/window
- **Then** its captured historical scope or shared persistent evidence is available, just as in the lanes
- **And** UTC dates distinguish midnight crossings, missing record identities remain explicit, and an event-only Timeline uses the resolved visible interval even without check buckets

This alternative-view policy is proposed, not yet confirmed. Table action hit areas stay within their own cells, and read requests begin only when evidence is opened.

### Keep missing evaluation separate from OK

- **Given** the Findings lane contains missing check-state buckets
- **When** I inspect the aggregate lane
- **Then** those runs remain visible and inspectable as no data, including beside observed warnings
- **And** an all-missing/empty response is never called “ok throughout” or presented as a known zero failure count

Proposed, not yet confirmed: Overview requests the existing API's OK rows so observed OK and omitted rows have different inputs. “OK in observed buckets” describes only returned observations, not complete configured-check evaluation or continuous capture. The aggregation still uses the worst observed check state; one missing check does not erase another check's known observation. Retained findings may overlap a no-data interval without proving evaluation in its missing buckets.
