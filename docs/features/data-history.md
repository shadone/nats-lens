# Data history

- **Surfaces:** `ui`, `api`
- **Status:** partial — forecast margins are not built
- **Related:** [history-and-archive.md](history-and-archive.md), [monitoring-snapshots.md](monitoring-snapshots.md), [event-capture.md](event-capture.md), [triage-and-mutes.md](triage-and-mutes.md)

## What it does

Data history shows when a system’s stored history begins, coverage and gaps in the chosen range, history requirements, and retention and archive state. Range pages name a history shorter than the requested range, charts hatch times without collections, and missing growth figures explain the history they need. Forecast margins remain planned.

## Behavior and rules

- **One system and range.** Data history sits under Cluster, after Accounts, at `/data-history`. It reads the chosen system and global range, without an account filter. Coverage and gaps describe the range; history start, retention, archive, and findings already true at the start describe the system. The summary names the collection stop when known and the oldest event when it differs from the oldest collection by more than one interval.
- **Coverage.** The page shows collections against the expected count, partial collections, time without collections, and the share covered. The covered span starts at the later of the range start and history start. Time before history does not lower the covered share. Without stored history, or when the whole range precedes history, the page says it has no collection instead of showing a covered percentage.
- **Six kinds of gap.** The page names times nats-lens was not running, could not reach NATS, had no collections, did not capture events, did not watch Kubernetes, and had not started its history. Collection gaps longer than three collect intervals count as no collections. An event connection’s loss shorter than 10 seconds does not count as a capture gap. Overlapping collection gaps merge; not running takes precedence over not connected, then no collections. Event and watch gaps remain separate, even inside a collection gap.
- **Placement observations.** The [Placement panel](placement.md) stops sampled problem duration at collection gaps and unknown evidence. Its bounded search uses stored collections in the selected range, independently of finding duration.
- **Gap details.** A strip and newest-first table give the times, duration, collections either side, and the recorded event when available. Collection links open Placement at that collection; event links open Events. A gap open at a live range’s end says “still now”; one open at a past range’s end says “after the range”.
- **Gap limits.** The page lists the newest 200 times without collections (including before-history) and, separately, the newest 200 event or watch gaps. It reports how many of each class were left out. Coverage uses all collection gaps. Chart hatches and the page’s strip use the limited list. Charts show a readable, accessible notice when their history response omits older collection gaps. Exactly 200 returned gaps, or truncated event/watch gaps alone, do not trigger that notice. Chart requests use their own time domain; a retained live answer only keeps the notice while recorded omission evidence overlaps the chart.
- **What uses history.** Rows compare a target span with the time between actual stored collections. Growth uses the selected range; other uses read their own bounded windows ending at the latest collection. Bounded uses can meet their target with observed samples on either side of the window start, each within one collection interval. The displayed sample span stays exact; this says “window boundary sampled” when that span is shorter than the target. This allowance requires a positive interval shorter than the window, two distinct in-window samples, and the latest stored collection at the end. Growth keeps its strict six-hour sample requirement. Passing time while collection is stopped adds no samples. An empty window leaves the available span unset. A span can contain gaps and missing entity measurements, so “sample span available” does not establish a result. Forecast targets are configured lookbacks; their minimum spans, process boundaries, positive slopes, and other entity rules determine whether a forecast exists. A shorter retention says the target exceeds retention. Only a fresh growth range longer than six hours can name an earliest target time, conditional on continued collection; passing that time never marks the span available without a fresh answer. Acceptance growth review requires its paired baseline buckets, daily totals can be incomplete, and Replay visits stored moments even when the full target span is unavailable.
- **Retention and archive.** The page shows collection and event retention, its sweep cutoff, the archive location, hours held and their bounds, archive retention, listing time, last export, whether the newest hour was provisional, and the latest export error when present. Before the first listing it says the hours are not listed yet. A system without an archive says so. Archive state does not change when a past range is selected ([history-and-archive.md](history-and-archive.md)).

- **History begins here.** When the chosen range starts more than one collection interval before the oldest collection, a note names the available span and links to Data history. Charts keep the full requested time axis, including the time before history.
- **Gaps are drawn.** Trend and series charts hatch times without collections and the time before history, with a tooltip and accessible name. Event-capture and Kubernetes-watch gaps leave the collection charts unhatched. Charts share cached history by system and time window, including fixed seven-day charts when the page shows one hour. Gap marks stop at the range actually observed; while a range's history loads, a chart says so in a row it already has, so a card keeps its height: “Gaps loading” between a trend chart's dates and in place of a tile's "since" between its track ends, hidden where the row has no room for it whole; “History gaps loading” in place of a series chart's min and max and of a tile's "steady at" or "no data for the window" line. A row shows its data for the first 300 ms of loading, except a steady line, which gives way at once. A tile that draws no chart shows it only where its steady line would be; Meta leader, which has no track, shows it on a line of its own in place of its chart, as it would otherwise claim no leader changes. When the history read fails the rows keep their data, and the page shows one note, “History gaps unavailable: these charts do not mark gaps.”; only a tile's steady line, which the gaps could disprove, reads “History gaps unavailable” instead. A server's page keeps its own note, and its tiles follow the server's read the same way. A screen reader hears the state once per page, “loading” only after 300 ms of waiting (proposed, not yet confirmed). A positive-duration gap remains visible even on a 30-day chart. A page without charts makes no history request for hatching.
- **Server reports.** Server detail charts hatch explicit missing-server records and collections where both STATSZ and VARZ failed or were missing. Either metric source answering prevents the latter hatch. Names span process restarts; unrelated endpoint failures do not hatch these metrics. Each interval stops at the next collection, one configured interval, or the requested end, whichever comes first. Charts break both metric traces across those intervals. Global collection hatches remain separate. One server-scoped history request serves all five charts; range, system, and server changes never reuse another scope’s gaps. Unknown reports do not prove coverage or downtime.
- **Growth needs history.** Capacity, Streams, and the Overview storage tile share the six-hour requirement. Overview’s storage guidance states that requirement without declaring that measurements are missing. Missing-growth notes require samples spanning six hours and explain that entity data may be missing. A shorter range asks for at least six hours. The oldest retained timestamp alone never supplies a countdown. Forecast margins remain planned.
- **Findings say what they know.** A finding whose start nats-lens cannot know says "already true when nats-lens started", never a fresh start time.
- **Tiles for the range.** Overview message and connection trends, connections tiles, and storage name the shorter history behind the range. Missing-history words on Capacity, Accounts, Applications, Replay, and finding details link to Data history with the global query.
- **The API.** `GetDataHistory` returns the system’s history, coverage, gaps and their totals, the newest omitted collection gap when truncated, optional recorded server missing reports, archive state, history requirements, and the count of firing findings already true at the history start. `GetLensInfo` reports the same history start.

## Scenarios

### Open a 24-hour range right after the start

- **Surfaces:** `ui`
- **Given** nats-lens started collecting 31 minutes ago
- **When** I open the Overview for the last 24 hours
- **Then** a banner says that there are 31 minutes of history, and the charts retain the full requested range with the time before history hatched

### See a gap in the data

- **Surfaces:** `ui`
- **Given** nats-lens was stopped for three minutes an hour ago
- **When** I look at a chart that covers that hour
- **Then** the three minutes are hatched and labelled as a gap

### Keep gaps tied to the chart’s observed range

- **Surfaces:** `ui`
- **Given** a stream’s seven-day chart includes a restart five hours ago
- **When** I change the page between one hour and 24 hours
- **Then** the seven-day chart shows the same restart in both views
- **And** a delayed or failed history read never extends an earlier range’s open gap into a later range

### See a short gap on a long range

- **Surfaces:** `ui`
- **Given** a 30-day range contains a 60-second gap
- **When** I look at its chart
- **Then** the gap has a visible hatch and an accessible name

### Ask where the history starts

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `GetLensInfo`
- **Then** it receives the start of the oldest stored collection

### Read coverage before history

- **Surfaces:** `ui`, `api`
- **Given** a chosen range ends before the system’s history starts
- **When** I open Data history or request `GetDataHistory`
- **Then** the API reports zero collections and the page says “No collection in this range: nats-lens holds nothing of it.”
- **And** the page does not present a covered percentage

### Separate event loss from collection loss

- **Surfaces:** `ui`, `api`
- **Given** an event connection was lost for 50 seconds while collections continued
- **When** I inspect Data history for that range
- **Then** the API reports an event-capture gap, the table labels it “Events not captured”, and collection charts leave that time unhatched

### Inspect a bounded gap list

- **Surfaces:** `ui`, `api`
- **Given** the range contains 243 separate times without collections
- **When** I inspect Data history
- **Then** the API returns the newest 200 with a total of 243, and the page says “Showing 200 of 243 times without collections, the newest.”
- **And** coverage includes all 243 times, while charts say “History response: 43 older collection gaps omitted. Hatches may be incomplete.”

### Keep chart gap notices scoped to the chart

- **Surfaces:** `ui`, `api`
- **Given** a large selected range contains more than 200 collection gaps but a narrower chart contains none
- **When** I view that chart
- **Then** its history request uses the chart’s domain and no truncation notice appears
- **And** exactly 200 collection gaps or omitted event/watch gaps alone do not produce a collection-gap notice

### Inspect the archive from a past range

- **Surfaces:** `ui`, `api`
- **Given** the system has an archive and a past range is selected
- **When** I open Data history
- **Then** coverage describes that range, and the archive reports its latest known listing and exports

### Read availability after a long collection outage

- **Surfaces:** `ui`, `api`
- **Given** old collections remain stored, followed by a long outage and four minutes of recent samples
- **When** I request a 24-hour range whose earlier hours contain no collections
- **Then** growth and forecast rows measure their actual sample spans instead of the age of retained history
- **And** stale collection produces no countdown, while coverage reports the missing time separately

### Use samples across a gap

- **Surfaces:** `ui`, `api`
- **Given** an entity has usable growth samples separated by more than six hours with a collection gap between them
- **When** I inspect growth and Data history over that range
- **Then** the growth algorithm can return a rate, and Data history qualifies the global sample span as availability rather than entity readiness

### A bounded window starts between collections

- **Surfaces:** `ui`, `api`
- **Given** over an hour of collections at a 30-second interval with a changed phase and small jitter
- **When** Data history reads the latest five-, ten-, and thirty-minute windows
- **Then** actual samples bracketing each start within one interval establish global window availability without lengthening the displayed sample span
- **And** a missing boundary sample or an edge deficit beyond one interval leaves the target unavailable
- **And** gaps and missing entity measurements still prevent this status from proving complete coverage or a forecast

### See missing server reports while collection continues

- **Surfaces:** `ui`, `api`
- **Given** other servers keep answering and a named server misses a collection, or both its metric endpoints fail
- **When** I open that server’s detail charts
- **Then** bounded hatches name the missing reports and both metric traces break across them
- **And** a successful STATSZ or VARZ answer prevents the metric-report hatch, including after a process restart
- **And** global collection coverage does not treat the server’s absence as an outage

## Not supported / out of scope

- **No backfill.** nats-lens cannot collect for a time when it was not running.
- **No history from the archive in the UI.** Hours older than `retention` are in the archive only ([history-and-archive.md](history-and-archive.md)).
- **No forecast margins.** Forecast confidence margins remain planned.
- **No server downtime inference.** A missing report does not prove a server was down; unrecorded intervals remain unknown.
- **No Fleet overlay.** Fleet’s status strips do not use the collection-history hatches.
