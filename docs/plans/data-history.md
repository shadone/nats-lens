# Data history build plan

Working plan for Data history (`docs/features/data-history.md`). The owner decided on 2026-11-05 to build it ("all good important points, lets implement"); every choice below was proposed by the planning session of 2026-11-05 and is not yet confirmed. When a task lands, its lasting content moves into `docs/features/data-history.md`, `docs/features/history-and-archive.md`, `docs/features/navigation.md` and `docs/design.md`; this file then only tracks status.

Examples use the online shop of `inventory.example.yaml` and the demo's systems (`example-prod`, `example-global`, `example-staging`).

## Open questions for the owner

1. **Charts before the history starts.** The feature doc plans charts that show only the stored history, with "Show the full 24 h anyway". Proposed instead: every chart spans the range asked for, and the time before the history starts is hatched and labelled "nats-lens was not running yet", like any other gap; no toggle. Recommended: yes, as the Overview's trends share one time axis and a hover, and a chart that starts where its data starts hides how short the history is.
2. **Forecast margins.** The feature doc says a forecast gives "a margin". No forecast computes one today (`RobustPerDay` is a Theil-Sen slope without an interval). Proposed: this version shows the span each forecast used and how long until it has enough, and the margin goes to the leftovers. Recommended: yes; a margin needs its own rule for every forecast.
3. **Where the page sits.** Proposed: "Data history" under Cluster, after Accounts, at `/data-history`. Recommended: yes; it describes what nats-lens holds of this system, as the other Cluster pages describe the system.

## Status

The three open questions above are answered as recommended by the session that built T1: proposed, not yet confirmed. T1 builds on answer 1 (the "before history" gap kind) and leaves the margin out (answer 2); T2 puts the page where answer 3 says.

| Task | Status |
|---|---|
| T1 API, store and demo | built on `feat/data-history-api`, not yet reviewed; see "Where T1 departed from this plan" |
| T2 The Data history page | built on `feat/data-history-page`, not yet reviewed; see "Where T2 departed from this plan" |
| T3 History notes on the other pages | planned |
| T4 Feature docs and audit | planned |

## Rules for every step

- Read-only towards NATS and no collector change. Every value below is stored today: `epochs` (`started_at`, `partial`), `events` of source `collector` (`collector.gap` with `from` and `to` in `raw`, `collector.started`, `collector.not_connected`, `collector.connection_closed`, `collector.disconnected`, `collector.reconnected`, `collector.k8s_gap`, `collector.k8s_not_watching`), `finding_transitions`, `check_runs`, `archive_hours`, the archive's manifests, and the configuration (`retention`, `archive_retention`, `interval`).
- One rule per kind of gap, shared with Replay. Replay already finds gaps (`api.ReplayGap`, `collectionGaps` in `storeprov/replay_events.go`, `api.EventsConnectionGaps`); T1 moves them to one place and both pages use it, so the two pages never disagree about a gap.
- A value not observed is unset, never 0. A range with no collection says so; it is not "0% covered".
- The page reads the system of `?system=` and the page range, as every page does. Lists describe the range; the history start, retention and archive describe the system now.
- Time parameters as Unix microseconds and `make_timestamp(?::BIGINT)` wherever DuckDB cannot type them from a column (AGENTS.md); store tests with a non-UTC `TZ`.
- `docs/design.md`, `docs/features/README.md` and the README are edited only by T4 (see "Order with the other phase 3 plans"); the other tasks record their departures here.

## What backs each part

| Part | Source | Rule |
|---|---|---|
| History start | `min(epochs.started_at)` | `GetLensInfo.history_start` already reports it; the page and the banner read the same value. |
| Newest collection | `max(epochs.started_at)` | With the collection status of the shell ("not collecting since 11:30"). |
| Retention | configuration, per system (`retention`, the system's own or the top-level) | "Collections and events are kept for 14 days; the oldest kept now is from 2026-10-22 09:00 UTC." The sweep runs hourly, so the oldest kept can be up to an hour older than now minus retention. |
| Oldest event | `min(events.time)` | Events follow retention by receive time; shown when it differs from the history start by more than an interval (events written before the first collection, or a database rebuilt from the archive). |
| Coverage of the range | `epochs` in the range | Collections stored, collections expected (the covered span divided by the interval), partial collections, and the time without data (the sum of the gaps). The covered span starts at the later of the range start and the history start. |
| Gaps | `epochs`, collector events | One row per gap in the range, kinds below, newest first, at most 200 with the total. |
| Archive | the archiver's last listing of its manifests, `archive_hours` | Where it is (`archive_dir`), the oldest and newest archived hour, hours archived, `archive_retention`, the last export and whether the open hour is final, and the last export error. See T1 for why not `archive_hours` alone. |
| What uses history | constants in `internal/api`, the check set's lookbacks | One row per use, with what it needs, what the system has, and when it will have enough. Rows below. |
| Findings at the start | `findings.since`, `condition_since`, the history start | Firing findings first seen within a minute of the history start, counted, with a link to Findings: "4 findings were already true when nats-lens started." |

### Kinds of gap

| Kind | From | Label |
|---|---|---|
| Not running | `collector.gap` (`raw.from` to `raw.to`) | "nats-lens was not running" |
| Not connected | the collector connection's `collector.not_connected` or `collector.connection_closed` to its next `collector.reconnected` (or open) | "nats-lens could not reach NATS" |
| No collections | consecutive epochs more than three intervals apart, with neither of the above over that time | "No collections" |
| Events not captured | `api.EventsConnectionGaps` (a connection that captures events lost for 10 seconds or more) | "Events not captured"; collections continue |
| Kubernetes not watched | `collector.k8s_gap` (the span it records, as Replay reads it), or from a `collector.k8s_not_watching` not yet followed by one (open) | "Kubernetes not watched" |
| Before the history | the range start to the history start | "nats-lens was not running yet" (open question 1) |

The first three are times without collections and are hatched on charts; the next two are times with collections but without some events, and are listed on the page but not hatched on charts, which draw collections. A gap that overlaps another of the first three is drawn once, labelled by the first kind in this order.

### What uses history

| Use | Needs | Read from |
|---|---|---|
| Growth per day (Streams, Capacity, the storage tile) | 6 hours of the range (`api.MinGrowthSpan`) | the covered span of the range |
| Store and stream forecasts of the checks (`server-014`, `stream-006`) | their lookback, from `checks.Set` | the history |
| Every other check with a lookback longer than the history now | its lookback | the history; only checks still short are listed, with "until <time>" |
| Accepted findings' growth review | 8 days and an hour (`api.AcceptRetention`), and a retention at least that long | the history and the retention; a retention shorter than that is named, as start already logs it |
| Applications traffic, failed logins (Accounts), drops (Topology) | 24 hours | the history |
| Replay | the database only: A no older than retention | the retention |

A row reads "needs 6 h; has 31 min; enough at 15:02 UTC", or "enough", or "never with retention 7 d" when the retention is shorter than the need.

## T1 API, store and demo

`proto/natslens/v1/history.proto`, RPC `GetDataHistory(GetDataHistoryRequest) returns (GetDataHistoryResponse)` in `service.proto`, read-only (`idempotency_level = NO_SIDE_EFFECTS` as the other reads).

- **Refactor first, in its own commit.** `internal/api/gaps.go` holds the gap kinds, `ReplayGap` and `EventsConnectionGaps` (moved from `replay.go`), and the collections rule; `storeprov/gaps.go` holds the SQL that reads them (moved from `replay_events.go`'s `replayGaps` and `collectionGaps`), taking any span, not only Replay's 24 hours. Replay's tests pass unchanged.
- **Request:** `system`, `range`.
- **Response:** `history_start`, `newest_epoch_at`, `events_start`, `google.protobuf.Duration retention`, `google.protobuf.Timestamp retention_from`, `collect_interval`, `HistoryCoverage coverage`, `repeated HistoryGap gaps`, `int32 gaps_total`, `ArchiveState archive`, `repeated HistoryUse uses`, `int32 findings_at_start`, and, from the review round, `int32 listed_gaps_total` (13).
- `HistoryCoverage`: `covered_from`, `covered_to`, `int64 epochs`, `int64 expected_epochs`, `int64 partial_epochs`, `google.protobuf.Duration without_data`.
- `HistoryGap`: `HistoryGapKind kind` (enum: not running, not connected, no collections, events not captured, Kubernetes not watched, before history), `start`, `end` (unset while open at the range's end), `int64 before_epoch`, `int64 after_epoch`, `string event_id` (the collector event behind it), `string connection` (for events not captured).
- `ArchiveState`: `bool enabled`, `string location` (the directory), `google.protobuf.Duration archive_retention`, `oldest_hour`, `newest_hour`, `int32 hours`, `last_export_at`, `bool newest_final`, `string last_error`, `listed_at` (when the archiver last listed its manifests; unset before the first listing).
- `HistoryUse`: `string id` (stable, for the UI's words and tests: `growth`, `forecast:server-014`, `lookback:consumer-002`, `accept_review`, `day`, `replay`), `string check_code`, `google.protobuf.Duration needs`, `google.protobuf.Duration has`, `bool enough`, `google.protobuf.Timestamp enough_at` (unset when enough, or never within retention), `bool never` (the retention is shorter than the need).
- **Rules** in `internal/api/history.go` (`HistoryFacts`, `FinishDataHistory`: the kinds' order and merge, coverage, the uses table, the 200-row cut), shared by the store (`storeprov/history_page.go`) and the demo, as `FinishCapacity` is. The provider reads facts in at most four queries (the epochs' gaps by a window over `started_at` in the range, the collector events of the range and of the 12 hours before it for a gap that began earlier, the history and event starts, the findings at the start); never one query per epoch. Measure the read over 14 days of epochs at a 30-second interval (40,320 epochs) and say how long it took.
- **The archive's state comes from the archiver, not from `archive_hours`.** `archive_hours` keeps a row for every hour ever exported, also those `archive_retention` has pruned, and after a rebuild it knows only the hours the rebuild loaded. The archiver already lists its manifests once an hour to prune them; T1 makes it list them once an hour whether or not `archive_retention` is set, and keep the result with the last export's time and error (`archive.Status`, `internal/archive/status.go`). `cmd/natslens/system.go` passes the status to the provider as a function (`ArchiveStatus func() api.ArchiveState`), as `api.System.Status` carries the collection status. Without an archiver (`natslens once`, `natslens sql`), `enabled` is false.
- **Handler:** the common range validation and `QueryTimeout` through `call()`. No epoch in the range: the response with its history, retention, archive and uses, `coverage.epochs` 0, and one gap of the whole range, labelled by the collector events when one covers it, else "No collections".
- **Demo.** `example-staging` tells the story: 9 hours of history (`stagingHistory`), a `collector.gap` of 3 minutes 5 hours ago (nats-lens restarted), and its collector connection closed 3 hours ago (an open "not connected" gap to now); its series, epochs and `GetReplay` leave the 3 minutes out (no point inside the gap). `example-prod` has no gap and an archive of 90 days; `example-global` has a short "events not captured" gap where its scenario already has a `collector.disconnected` and `collector.reconnected` pair. The demo's retention must agree with its history: `GetLensInfo` reports 14 days for every system while `example-prod` and `example-global` hold 30 days (`defaultHistory`); proposed: report 30 days for those two and 14 days for `example-staging`, and check that no page reads the 14 days as a rule (Replay's refusal of an old A, the accept retention warning). Archive state in the demo is fixed values per system; `enabled` false for `example-staging` ("no archive" is then shown by the demo too).
- **Conformance rows** in `apitest` (`conformance_history.go`, one line in `conformance.go`): each gap kind found in at least one system; a range without collections; the coverage counts agree with `ListServers`' epoch for the range; `history_start` equals `GetLensInfo.history_start`; no `GetTimeSeries` point of `cluster.connections` lies inside a hatched gap; the uses table has a row not yet enough (staging's `accept_review`) and one enough (prod's `growth`). The store fixture gains a `collector.gap`, a closed collector connection and a 5-minute stretch without epochs.
- **Tests.** The gap rules: overlap and the label order, a gap that began before the range, an open gap, 201 gaps cut to 200 with the total; uses: each row's words for enough, not yet and never; the store read under `TZ=Asia/Kolkata`; the archive status after a prune, after a rebuild (hours on disk beyond what the database loaded), and before the first listing.

### Where T1 departed from this plan

Each choice below was made by the session that built T1: proposed, not yet confirmed.

- **Fields added.** `HistoryGap.before_epoch_at` (8) and `after_epoch_at` (9), the start times of the collections either side, so that T2 links a gap to the server or Placement page at that collection without another call. `ArchiveState.last_error_at` (11), when `last_error` happened.
- **Three queries, not four.** The findings already true at the start are counted in the query of the history and event starts, by a subquery on `min(epochs.started_at)`: no time parameter, so the time zone rule does not apply. The count follows the finding page's rule (`atHistoryStart`): firing, and its condition began before the history start or it was first seen within a minute after it.
- **The epochs' gaps.** `storeprov/gaps.go`'s `collectionGaps` reads, by a window over `started_at`, only the epochs either side of a gap, the first and the last; `api.CollectionGaps` takes those as `EpochPair`s. Replay reads through the same function. 14 days at a 30-second interval (40,320 epochs, five gaps) read in 12 ms (`TestDataHistoryFourteenDays`, on the development MacBook).
- **Gap ends and starts.** A gap keeps its real start, also before the range; a time without collections starts no earlier than the history start. A gap still open at the range's end, or that ends after it, has no `end`, as the events after the range are not read. A no-collections gap with no collection before it is the time before the history, and is not listed as "No collections".
- **Not connected** ends at the collector connection's next `collector.reconnected` or at the next `collector.started` (a new process has new connections), as `EventsConnectionGaps` does. **Kubernetes not watched** from a `collector.k8s_not_watching` ends at the next `collector.k8s_gap` (which then gives the span) or `collector.started`.
- **Events and Kubernetes gaps are listed also inside a time without collections**: staging's close at 11:30 shows "not connected" and "events not captured" from the same moment.
- **The uses table.** `growth` has an `enough_at` only on a range that follows now (its end within an interval of now) and spans at least 6 hours; a shorter or ended range never reaches 6 hours. `replay` needs 24 hours (`api.MaxReplaySpan`) and has the history; `day` needs 24 hours (`api.HistoryDay`). Forecast rows appear only for checks in the set; the other lookback rows, longest first, only while short.
- **The archive status.** `archive.Status` (`internal/archive/status.go`) is kept by the archiver: the listing runs at most once an hour after an export that succeeded, as the prune did, now also without `archive_retention` (one listing and one manifest read); `newest_final` comes from the newest manifest at the listing and from later exports; `last_error` is the error of the newest failed tick (an export or the state write), cleared by the next tick that succeeds. `enabled` is true wherever the provider has an archive status function: `cmd/natslens` sets one for every open system, and only `natslens run` serves the API.
- **The demo.** `example-staging`'s 3 minutes without nats-lens are 09:33:26 to 09:36:26 in the scenario's frame (about 4 h 55 min before now), so that its routine reconnect at 09:31 stays outside and the epochs stay on the 30-second grid. Its connection closed 3 hours ago was already in the scenario. `example-global`'s `collector.disconnected` and `collector.reconnected` pair is a failover of one second, which the shared rule does not count as a gap; changing it would change its Replay story, so the demo's "events not captured" gaps are staging's (the sys connection lost for 50 seconds at 07:30, and from the close). So that every gap kind is in the demo, `example-global` gained 5 minutes without collections on 13 October at 02:10:26 and 4 minutes without the Kubernetes watch on 14 October at 03:10. The demo's Replay now also tells a scenario's times without collections and its `collector.gap` and `collector.k8s_gap` events, as the store reads them, and its status timeline draws a bucket inside such a time as no data. The archive: `example-prod` 90 days held and kept, `example-global` 30 days held (its history) and 90 kept, `example-staging` none.
- **Retention in the demo.** `GetLensInfo`, `GetDataHistory` and Replay's refusal read each system's retention: 30 days for `example-prod` and `example-global`, 14 for `example-staging`. No page reads the 14 days as a rule: the accept retention warning appears only below 8 days and an hour. Tests changed: `TestLensInfo` (14 to 30 days) and `TestGetReplayRefusesAOlderThanRetention` (an A 31 days back, "(30d)").
- **Conformance.** "The coverage counts agree with `ListServers`' epoch" became "with the overview's newest epoch" (`ListServersResponse` carries no epoch): a range has collections exactly when the overview has an epoch for it, and a partial one when the overview's is. The store's rows read a new `history` system (`historyStore` in `storeprov/data_history_test.go`) with a gap of each kind, rather than the main conformance store; the main store gives "a system without an archive". Added rows: an archive with hours, a system without an archive, gaps newest first.

### T1 review round

The review found one issue and four smaller points; the session that built T1 decided each, proposed, not yet confirmed:

- **Losses older than the read.** The collector events are read from 12 hours before the range; the read now adds, as Replay's events read does, each connection's loss still open then, and the `collector.k8s_not_watching` that no `collector.k8s_gap` or `collector.started` followed. The demo has no such events, so its read is unchanged.
- **A gap open at the range's end** has no `end` and no `after_epoch` or `after_epoch_at`: what follows the range is not part of it. That includes a `collector.gap` received after the range.
- **Two classes, cut apart.** The times without collections and before the history (the kinds charts hatch) and the other gaps (events not captured, Kubernetes not watched) are each cut at the newest 200; `gaps_total` counts the first class, the new `listed_gaps_total` (13) the second. T2 and T3 draw hatching from the first class only.
- **Findings at the history start.** `findings_at_start` counts at the oldest stored collection; once the retention sweep has run, that is the oldest kept, not when nats-lens first started. The query takes `api.HistoryStartSlack` as a parameter.
- **The archive listing** runs at most once an hour after an export that succeeded, also without `archive_retention`: one listing of the manifests and one read of the newest manifest. `ArchiveState.location` carries no credentials: a URL loses its user information, query and fragment (`archive.LocationText`).
- **Expected collections** divide the covered span by the collect interval configured now; a range from before an interval change is counted at today's interval.

## T2 The Data history page

- Route `/data-history`, `web/src/pages/DataHistory.tsx`; a `NAV` entry under Cluster after Accounts in `web/src/nav.ts` (open question 3), and `App.test.tsx`'s page list. Header "Data history", subtitle "What nats-lens holds of this system, and where it has gaps."
- **Summary line:** "Collecting since 2026-10-22 09:00 UTC (13 days 6 hours) · kept for 14 days · archived for 90 days", or "Collecting since 14:29 UTC (31 min)". While the collector does not collect, the shell's "not collecting since" appears here too.
- **Coverage of the range** as metric boxes (`MetricBox`): collections (with expected), partial collections, time without data, and the covered share, each saying the span it covers.
- **Gaps:** a strip of the range with the hatched gaps (the same hatch T3 adds to charts) and a table: kind (label), from, to (or "still now"), length, the collections either side (linked to the server or Placement page at that collection, as Replay's evidence links are), the collector event (linked to Events). "Showing 200 of 243".
- **What uses history:** the uses table, with the needs and has in words ("needs 6 h; has 31 min; enough at 15:02 UTC").
- **Archive:** location, oldest and newest hour, hours, `archive_retention`, last export ("last exported 14:35 UTC, the open hour provisionally"), the last error in the warning colour, and "No archive: `archive_dir` is not written in this command" when `enabled` is false. Under it: "The archive holds hourly Parquet files of every table. The UI reads the database only; hours older than the retention are in the archive, for `natslens sql` after a rebuild or for other tools."
- **Findings at the start:** "4 findings were already true when nats-lens started", linking Findings.
- Data: `requests.getDataHistory`, `web/src/api/history.ts` (the only reader of its messages; unset stays undefined), `useDataHistory()` keyed by system and range, refetched every 30 seconds on a live range, keeping the previous answer only for the same system, as `useCapacity`. Words in `web/src/lib/historyView.ts` with tests (gap labels, lengths, the uses' words, the summary line).
- Styles in `web/src/styles/history.css`, every rule on a `dh-` class, checked by `web/src/components/HistoryCss.test.ts` as `CapacityCss.test.ts` does; imported from `index.css`. Phone width: boxes two to a row, the table scrolls inside its card.
- Browser check: in `natslens demo`, at 360, 761, 1000 and 1280 px on the three systems, `document.documentElement.scrollWidth` equals `clientWidth` (the design's rule), and the page's screenshots at 360 and 1280 px are looked at.

### Where T2 departed from this plan

Each choice below was made by the session that built T2: proposed, not yet confirmed.

- **Files.** The cards are in `web/src/components/HistoryParts.tsx`; the page, `web/src/pages/DataHistory.tsx`, only places them. The test fixtures are `web/src/test/fixtures/history.ts`. `requests.getDataHistory` passes the range it sent (`rangeStart`, `rangeEnd` on the view model), so the strip spans exactly the range the answer describes. `isHatched` and `HATCHED_KINDS` in `web/src/api/history.ts` pick the kinds charts hatch, for T3.
- **Coverage boxes without a chart.** `MetricBox` always draws a sparkline and a range track, which a count does not have; the four boxes use its look (`card mbox`, the title with (i), the value, the note) without them. "Time without data" is titled "Without data", so the title fits beside the value. The covered share is the covered span less the time without data, never "100%" with a gap and never "0%" with data ("<1%"). Without history in the range the boxes give way to "No collection in this range: nats-lens holds nothing of it."
- **The summary line.** While the shell says the collector does not collect, the line starts "History since" in place of "Collecting since", and "not collecting since 19:47 UTC" follows in the warning colour. The oldest event is added ("events from 13:50 UTC") when it differs from the history start by more than an interval. Without a history it reads "No collection stored yet". A time on now's UTC day is "13:47 UTC", any other "2026-10-22 09:00 UTC".
- **Open gaps.** "still now" on a live range; on a fixed range "after the range", as the API leaves the end unset also for a gap that ends after the range. The length of an open gap reads "3 h so far" or "at least 3 h".
- **The gaps table.** Collections either side read "before: collection 487, 17:50:44" and "after: …", each linked to Placement over the hour that ends a second after it (Replay's `rangeEndingAt`); a collection without its time is named, not linked. The event links to `/events?id=`. The cut is said per class: "Showing 200 of 243 times without collections, the newest." and "Showing 200 of 230 times without events or Kubernetes, the newest."
- **The strip.** Only the hatched kinds are drawn, oldest left, with `.dh-hatch` (the hatch T3 can share) and each stretch's title and screen reader text "No data 17:50:44–17:53:44 UTC: nats-lens was not running". Its ends read times, or day and time over a day or more, and "now" on a live range.
- **What uses history.** A table of use, needs, has and enough, the plan's sentence as each row's title ("needs 1 d; has 9 h; enough at 2026-11-06 13:47 UTC"). A use that is not enough and has no `enough_at` and is not `never` reads "not within this range" (growth on a short or ended range). The uses are named: growth "Growth per day", `forecast:server-014` "Store forecast", `forecast:stream-006` "Stream forecast", `lookback:<check>` "Check <check>", `accept_review` "Growth review of accepted findings", `day` "The last 24 hours", `replay` "Replay"; an unknown ID shows as it is.
- **Retention and archive in one card.** Its line under the title is the database's retention ("Collections and events are kept for 14 days: the hourly sweep removes what is older than 2026-10-22 22:47 UTC."); then location, hours, kept, last export and when the hours were listed, the error in the warning colour. Hours before the first listing read "not listed yet". Without an archive: "No archive: `archive_dir` is not set for this system, so nothing is archived." The note on what the archive holds is shown with or without one.
- **Findings at the start** reads "1 finding was already true when the history begins, at 13:47 UTC." with a link to Findings, or "No finding was already true …", as `findings_at_start` counts at the oldest kept collection, which after the retention sweep is not when nats-lens started.
- **Browser check.** In `natslens demo`, `scrollWidth` equalled `clientWidth` at 360, 761, 1000 and 1280 px for example-staging (last 24 h, and a fixed day before its history), example-prod (last 24 h), example-global (last 24 h, 7 days and a fixed 30 days).

## T3 History notes on the other pages

What `docs/features/data-history.md` plans outside the page, each linking to Data history.

- **History begins here.** When the range starts before the history start by more than an interval, a line under the header on every page that reads a range: "31 min of history: you asked for the last 24 h; nats-lens started at 14:29 UTC. Data history". It reads `GetLensInfo.history_start` (already read by the shell), so it adds no call. `web/src/components/HistoryNote.tsx`, placed by the page layout in `App.tsx`.
- **Gaps on charts.** `TrendChart` and `SeriesChart` draw the range's hatched gaps (the first three kinds and "before history") with their label as a tooltip and an accessible name ("No data 09:40–09:43 UTC: nats-lens was not running"). The gaps come from `useDataHistory()` for the page's system and range, one call per page shared by every chart through the query cache; a page without charts makes none. Charts span the range asked for (open question 1).
- **Forecasts say how long they need.** Where growth is unset for want of history, the words say how long is left: the Capacity cards and header ("needs 6 h of history; 5 h 29 m to go"), the Streams page's growth cell tooltip, the Overview's storage tile. The time left comes from the history start and the range, in `web/src/lib/historyView.ts`, shared by those pages.
- **Findings say what they know.** The finding page's rule (`atHistoryStart`, "already true when nats-lens started") also marks Findings rows and the Overview's cause cards, in place of a start time that would read as fresh.
- **Tiles for the range.** The Overview's message rate, connections and storage tiles add "over 31 min" when the history inside the range is shorter than the range.
- **Links.** "not enough history" and similar words on Capacity, Accounts (a 24-hour window cut by the history start), Applications (hours before the history), Replay ("A is before the history starts") and the finding page link to `/data-history` with the global query.
- **Files:** `App.tsx`, `components/HistoryNote.tsx` (new), `components/TrendChart.tsx`, `charts/SeriesChart.tsx`, `components/OverviewTiles.tsx`, `pages/Overview.tsx`, `components/CauseGroups.tsx`, `pages/Findings.tsx`, `pages/FindingDetail.tsx` (the link only), `pages/Streams.tsx`, `lib/capacityView.ts`, `components/CapacityServers.tsx`, `lib/accountsView.ts`, `lib/replayView.ts`, `lib/historyView.ts`, `styles/history.css`. Tests for each note's words and its absence when the history covers the range; a chart test with a gap and with the time before history.
- **Browser check:** `example-staging` on the Overview, Capacity and a server page at 360 and 1280 px: the note, the hatched 3 minutes and the time before history are drawn and named.

## T4 Feature docs and audit

`data-history.md` to `Status: shipped` (or `partial`), with the rules and scenarios as built, open question 1's answer, and the margin under Not supported or Planned; `history-and-archive.md` (the page shows the archive's state), `navigation.md` (the page in the sidebar, the history note), `overview-page.md`, `capacity.md`, `triage-and-mutes.md` (the notes), `replay.md` (the shared gap rule), `demo-mode.md` (staging's story, the retention per system). `docs/design.md`: the RPC under "RPCs", the gap kinds and their order, the archive status from the archiver and why not `archive_hours`, the page under "UI", each marked proposed, not yet confirmed. `docs/features/README.md`: the table row and the coverage line ("[x] What history exists, where it has gaps, what the archive holds and how much history each forecast has (data-history.md)"). The audit, `python3 <skill>/scripts/audit_feature_docs.py docs/features/ --map-refs require`; leftovers into `docs/plans/next.md`.

## Order with the other phase 3 plans

Three plans are built together: this one (D), `docs/plans/notifications.md` (N) and `docs/plans/mobile.md` (M). Tasks in one row can run at the same time; each row starts once the rows above it that it depends on have merged.

| Wave | Run together | Shared files and how they are kept apart |
|---|---|---|
| 1 | D T1, N T1 | D T1: proto, `internal/api`, `storeprov`, `fake`, `apitest`, `internal/archive`, `cmd/natslens/system.go`, `store.go`. N T1: `internal/config`, `internal/notify` (new), `cmd/natslens/main.go`, `notify.go`. No file in common. |
| 2 | D T2, M T1, N T2 | D T2 is web only (`nav.ts`, `App.tsx`, `requests.ts`, `queries.ts`, new files). M T1 and N T2 are server only and share no file but the migrations directory (0035 for N T2, 0036 for N T3 delivery, and 0037 reserved for M T1; proposed, not yet confirmed) and one line each in `migration_golden_test.go`. |
| 3 | D T3, M T2, N T3 | D T3 is web only. M T2 and N T3 are server only; see `docs/plans/notifications.md`. |
| 4 | M T3, N T4, D T4 | M T3 changes `CauseGroups.tsx`, `Findings.tsx`, `FindingDetail.tsx`, `App.tsx`, `requests.ts` and `queries.ts` after D T2 and D T3 changed them. D T4 is docs only. |
| 5 | N T5 | docs |
| 6 | M T4 | docs |

D T1 runs alone among the tasks that change `service.proto`, `provider.go`, `handler.go`, the fake provider or `apitest/conformance.go` (M T1, M T2). D T2 and M T3 both change `App.tsx`, `nav.ts` (D only), `requests.ts` and `queries.ts`; D T3 and M T3 both change `CauseGroups.tsx`, `Findings.tsx` and `FindingDetail.tsx`: in that order. `docs/design.md`, `docs/features/README.md` and the README are changed only by the docs tasks, one at a time: D T4, then N T5, then M T4.

## Leftovers

Not in this plan, most valuable first: a margin on each forecast; the archive's size per table and day (the destination lists names, not sizes); reading archived hours into the UI for a range older than the retention; gaps of one server (a server that did not answer) on the server's charts; a Fleet column with each system's history start.
