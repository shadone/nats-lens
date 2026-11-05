# Data history

- **Surfaces:** `ui`, `api`
- **Status:** partial — forecast margins and per-server missing-answer gaps are not built
- **Related:** [history-and-archive.md](history-and-archive.md), [monitoring-snapshots.md](monitoring-snapshots.md), [event-capture.md](event-capture.md), [triage-and-mutes.md](triage-and-mutes.md)

## What it does

Data history shows when a system’s stored history begins, coverage and gaps in the chosen range, history requirements, and retention and archive state. Range pages name a history shorter than the requested range, charts hatch times without collections, and missing growth figures explain the history they need. Forecast margins remain planned.

## Behavior and rules

- **One system and range.** Data history sits under Cluster, after Accounts, at `/data-history`. It reads the chosen system and global range, without an account filter. Coverage and gaps describe the range; history start, retention, archive, and findings already true at the start describe the system. The summary names the collection stop when known and the oldest event when it differs from the oldest collection by more than one interval.
- **Coverage.** The page shows collections against the expected count, partial collections, time without collections, and the share covered. The covered span starts at the later of the range start and history start. Time before history does not lower the covered share. Without stored history, or when the whole range precedes history, the page says it has no collection instead of showing a covered percentage.
- **Six kinds of gap.** The page names times nats-lens was not running, could not reach NATS, had no collections, did not capture events, did not watch Kubernetes, and had not started its history. Collection gaps longer than three collect intervals count as no collections. An event connection’s loss shorter than 10 seconds does not count as a capture gap. Overlapping collection gaps merge; not running takes precedence over not connected, then no collections. Event and watch gaps remain separate, even inside a collection gap.
- **Gap details.** A strip and newest-first table give the times, duration, collections either side, and the recorded event when available. Collection links open Placement at that collection; event links open Events. A gap open at a live range’s end says “still now”; one open at a past range’s end says “after the range”.
- **Gap limits.** The page lists the newest 200 times without collections (including before-history) and, separately, the newest 200 event or watch gaps. It reports how many of each class were left out. Coverage uses all collection gaps. Chart hatches and the page’s strip use the limited list; charts have no notice when older gaps are omitted.
- **What uses history.** The page lists the six-hour growth requirement, configured store and stream forecast lookbacks, and other check lookbacks while short of history. It also lists acceptance growth review (8 days and 1 hour), the 24-hour traffic, failed-login and drop windows, and Replay’s 24-hour window. Rows name the needed and available span, whether it is enough, and when it will be enough when that time is known. A retention shorter than a system-history requirement says it can never be enough with that retention. Growth uses the selected range; other rows use the system’s history, so “enough” does not guarantee that every measurement exists.
- **Retention and archive.** The page shows collection and event retention, its sweep cutoff, the archive location, hours held and their bounds, archive retention, listing time, last export, whether the newest hour was provisional, and the latest export error when present. Before the first listing it says the hours are not listed yet. A system without an archive says so. Archive state does not change when a past range is selected ([history-and-archive.md](history-and-archive.md)).

- **History begins here.** When the chosen range starts more than one collection interval before the oldest collection, a note names the available span and links to Data history. Charts keep the full requested time axis, including the time before history.
- **Gaps are drawn.** Trend and series charts hatch times without collections and the time before history, with a tooltip and accessible name. Event-capture and Kubernetes-watch gaps leave the collection charts unhatched. Charts share cached history by system and time window, including fixed seven-day charts when the page shows one hour. Gap marks stop at the range actually observed; switching ranges shows “History gaps loading” while waiting for that range’s history. Failed history reads say “History gaps unavailable”. A positive-duration gap remains visible even on a 30-day chart. A page without charts makes no history request for hatching.
- **Growth needs history.** Capacity, Streams, and the Overview storage tile share the six-hour requirement. A live range long enough for growth counts down the missing history. A shorter range asks for at least six hours; a fixed range names its available history. Forecast margins remain planned.
- **Findings say what they know.** A finding whose start nats-lens cannot know says "already true when nats-lens started", never a fresh start time.
- **Tiles for the range.** Overview message and connection trends, connections tiles, and storage name the shorter history behind the range. Missing-history words on Capacity, Accounts, Applications, Replay, and finding details link to Data history with the global query.
- **The API.** `GetDataHistory` returns the system’s history, coverage, gaps and their totals, archive state, history requirements, and the count of firing findings already true at the history start. `GetLensInfo` reports the same history start.

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
- **And** coverage includes all 243 times, while chart hatches can omit the older 43

### Inspect the archive from a past range

- **Surfaces:** `ui`, `api`
- **Given** the system has an archive and a past range is selected
- **When** I open Data history
- **Then** coverage describes that range, and the archive reports its latest known listing and exports

## Not supported / out of scope

- **No backfill.** nats-lens cannot collect for a time when it was not running.
- **No history from the archive in the UI.** Hours older than `retention` are in the archive only ([history-and-archive.md](history-and-archive.md)).
- **No forecast margins.** Forecast confidence margins remain planned.
- **No per-server gap overlay.** A server’s missing answer does not create a server-specific chart hatch.
- **No Fleet overlay.** Fleet’s status strips do not use the collection-history hatches.
