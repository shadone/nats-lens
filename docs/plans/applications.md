# Applications build plan

Working plan for the Applications page (`docs/features/applications.md`, board `Services.dc.html` of the design canvas). The owner chose on 2026-10-26 to defer "Runs on" and to back the 24-hour traffic with an hourly rollup; everything else here was proposed by the planning session of 2026-10-26 and is not yet confirmed. When a task lands, its lasting content moves into `docs/features/applications.md`, `docs/features/monitoring-snapshots.md` and `docs/design.md`; this file then only tracks status.

The board's sample names come from a sketch of a real deployment, and its footnote names a hosting product. Use the example shop of `inventory.example.yaml` and the demo's systems (`example-global`, `example-prod`, `example-staging`) instead; no name from the board goes into the repository.

## Status

| Task | Status |
|---|---|
| T1 Hourly traffic rollup and the tooling rule | built on `feat/apps-rollup`, under review |
| T2 API, store and demo | built on `feat/apps-api`, under review |
| T3 UI | built on `feat/apps-ui`, under review |
| T4 Feature docs and audit | not started |

## Decisions of the owner (2026-10-26)

- **"Runs on" is deferred.** The Kubernetes watcher keeps no client pods. The page shows the pod names a connection name gives (`service@pod`) and the client addresses; namespace, workload and node go to the leftovers.
- **24-hour traffic from an hourly rollup.** A table filled when an epoch is written holds each application's message and byte increase per hour; the page reads 24 points from it.

## Rules for every step

- Read-only towards NATS. No new request to a server; the page reads the store, so a past range works as a live one.
- A value a server did not report is unset, never 0.
- The page shows the newest collection at or before the end of the page range (the epoch rule of `GetPlacement`); "24 hours" means the 24 hours before that collection.
- **An application is the connection name before the first "@"**, as `client-001` and the Overview's clients tile split it (`split_part(name, '@', 1)`); an empty or missing name is `(unnamed)`. One application per name across accounts: the client checks key by name, and a row lists the accounts it uses.
- **Tooling is left out** by the Connections page's rule: a name in the tooling names (`nats.name`, its `-events` connection, the account connections), or a loopback address while the epoch's loopback decision holds. `include_tooling` on the request shows them, marked.

## What backs each part

| Part | Source | Rule |
|---|---|---|
| Rows | `connections` of the epoch | One row per application. |
| Instances | `connections` of the epoch | Distinct pods (the part after "@") where the name gives one, else one per (server, cid). "4 instances on 3 servers". |
| Pods | `connections` of the epoch | The pod names, at most 10 per row, with the count. |
| Servers | `connections`, `servers` | The servers the application's connections are on, with a count each. |
| Accounts | `connections` | The accounts (named as `AccountName` names them; `$G` for an empty account). |
| Library | `connections.lang`, `version` | Each (lang, version) with its connection count; more than one reads "mixed". |
| Round trip p50 | `connections.rtt_ns` | The median over the application's connections that report one. |
| Subscriptions, pending | `connections` | Sums; the largest pending of one connection beside the sum. |
| Reconnects 24 h | `events` | `client-004`'s rule over 24 hours per application: drops are `client.disconnect` with a reason other than "Client Closed" and "Server Shutdown", connects are `client.connect` plus the connects folded into `client.churn`; the count is the smaller of the two. |
| Slow consumers 24 h | `events` | `client.disconnect` whose reason contains "slow consumer" (case-insensitive), as `client-003`. |
| Stale 24 h | `events` | `client.disconnect` whose reason is "Stale Connection". |
| Short-lived 24 h | `events` | Connects folded into `client.churn`, as `client-005`. |
| Findings | `findings` | The open client findings whose entity key is the application or one of its connection names (`client-002` to `client-005` key the whole name, `client-001` the application). The row links to them. |
| Messages in and out 24 h | the rollup (T1) | 24 hourly points, in and out; totals beside them. Hours without data are unset, not 0. |
| Spread summary | the rows | The server with the most of the listed connections and its share; when one server holds more than half and the applications run on more than one server: "A restart of `<server>` moves `<n>` of `<m>` connections at once." |
| Libraries summary | the rows | Libraries and versions by application count: "nats.js 3.0.2 in 6 applications, go 1.54.0 in 2". |
| Last 24 h summary | the rows | Sums of slow consumers, stale connections, and the applications with reconnects. |

Order: applications with an open client finding first, then by connections, descending, then by name. At most 500 applications, with the total. The UI sorts by any column.

## T1 Hourly traffic rollup and the tooling rule

- **Migration `0034_client_app_hours.sql`.** Table `client_app_hours` (`hour` TIMESTAMP, `account`, `app`, `loopback` BOOLEAN (the connections were from a loopback address), `in_msgs`, `out_msgs`, `in_bytes`, `out_bytes` (increases within the hour), `epochs` (collections that added to it)), unique on (`hour`, `account`, `app`, `loopback`), comments on every column. `app` keeps the full name for tooling names: a tooling name has no "@", so the name is the application.
- **Filled when an epoch is written,** in the same transaction, as `micro_endpoint_hours` is (`store/write_micro_hours.go`): each connection's increase over its previous row, paired by (`server_id`, `cid`, `start_time`), within the hour before; a counter that fell counts whole; a connection's first row counts its value only when it started after the previous epoch, so a system seen for the first time adds no history. Time parameters as Unix microseconds (`make_timestamp`); test with a non-UTC `TZ`.
- **Retention and archive.** Archived by hour, swept with the hours wholly before the window. A rebuild from hours archived before 0034 must still work.
- **Lost traffic.** A connection that closed between two collections loses its last interval; a server whose list was cut at `connz_limit` adds only the listed connections. The docs say so.
- **The tooling rule in one place.** One Go helper and one SQL fragment for "is tooling" (name in the tooling names, or loopback while the epoch's decision holds), used by the Connections page, the Overview's clients tile, Replay's events and the new page. A refactor commit of its own before the rollup.
- **Size.** Measure the write cost for 4,096 connections on 3 servers, and the read of 24 hours for 500 applications.
- **Tests.** Increases over two epochs, a reconnect (new cid), a counter that fell, a connection's first row, the hour boundary, a gap of more than three intervals, loopback, the migration on a store with data, archive and rebuild, a non-UTC `TZ`.

### Where T1 departed from this plan

Decided in the session that built T1; proposed, not yet confirmed.

- **A name with nothing before the "@"** (`@pod-1`) is `(unnamed)` too, not an application with an empty name.
- **A row that adds nothing is not written.** A connection's first row that does not count (it started before the previous epoch) does not count in `epochs` and makes no row, so an hour in which no connection could be measured reads unset, not 0, as the page's rule asks.
- **A counter that fell makes all four count whole,** for that connection in that epoch, as `micro_endpoint_hours` does for a new run.
- **Gaps.** Rows up to an hour apart pair, and the whole increase goes to the later row's hour, also across an hour boundary; rows more than an hour apart do not pair, and the later row follows the first-row rule. The test covers 5 minutes (more than three intervals), 55 minutes across the hour, and 80 minutes.
- **The tooling rule's copies.** Besides the Connections page, the clients tile and Replay's reconnects, the routine events (`storeprov/routine.go`) had a fourth copy; all four use `storeprov/tooling.go`. The check engine's prelude macros stay as they are: they read the engine's reserved variables and the decision being made, which the pages do not have.
- **Loopback per row, decision at read time.** `client_app_hours.loopback` marks the connections from a loopback address; the rows do not know the loopback decision. T2 has to choose which decision applies to the 24 hours: the proposal is the page's epoch's, for every hour, as the Connections page judges its epoch.
- **The lookback query is shared** with `micro_endpoint_hours` (`writer.lookback`).
- **Measurements** (`TestClientAppHoursSize`, laptop): the rollup's query for 4,096 connections on 3 servers takes 15 to 40 ms; a whole epoch write of those connections takes 2 to 4.5 s, nearly all of it the connection rows' one-by-one inserts, before T1 as after. The 24 hours of 500 applications in two accounts read in 1 to 3 ms from 14 days of rows.

## T2 API, store and demo

`proto/natslens/v1/applications.proto`, RPC `GetApplications(GetApplicationsRequest) returns (GetApplicationsResponse)`.

- Request: `system`, `range`, `account` (the scope), `include_tooling`.
- Response: `epoch`, `epoch_at`, `repeated Application applications`, `int32 applications_total`, `int32 connections_total`, `int32 tooling_hidden`, the three summaries as fields (the UI words them), `bool connz_cut` (a server's list was cut).
- `Application`: `name`, `int32 connections`, `int32 instances`, `repeated string pods` (at most 10) with `pods_total`, `repeated ServerCount servers`, `repeated string accounts` with names, `repeated LibraryCount libraries`, `optional google.protobuf.Duration rtt_p50`, `optional int64 subscriptions`, `optional int64 pending_bytes`, `optional int64 pending_bytes_max`, `optional int32 reconnects_24h`, `slow_consumers_24h`, `stale_24h`, `short_lived_24h`, `repeated FindingRef findings`, `repeated HourTraffic traffic` (24, unset hours unset), `bool tooling`.
- Rules in `internal/api/applications.go` (`ApplicationFacts`, `FinishApplications`) shared by the store and the demo, as `FinishMicro` is.
- **Demo.** `example-prod`: `resize-worker` named `resize-worker@<pod>` with four pods on three servers; `callback-dispatcher` reconnecting six times in the last day with a `client-004` finding; a "Stale Connection" disconnect; one server holding most connections so the spread summary speaks. `example-global`: two libraries and versions. `example-staging`: stale data as today.
- Conformance rows in `apitest` for the store fixture and the demo alike: grouping at "@", unnamed, instances by pod and by connection, tooling hidden and included, the 24-hour counts, the rollup's points, a past range.
- Docs: `docs/design.md`, `demo-mode.md`.

### Where T2 departed from this plan

Decided in the session that built T2; proposed, not yet confirmed.

- **Traffic is the 24 whole hours before the epoch's hour.** The epoch's own hour is partial and would read as a drop at the end of the sparkline; the page lags by up to an hour instead. The totals sum the set hours.
- **Reconnects** (changed in fix round 1): per connection name the smaller of drops and connects, as `client-004` pairs them, summed over the application's names. Names seen only in events count for their application, a pod gone by the epoch among them.
- **The event window is `time`,** after the start of the 24 hours and at or before the epoch, as Replay counts reconnects; the checks window by `received_at`. The four counts are unset when no event is stored up to the epoch (`events_captured` in the response), as the Accounts and auth page does.
- **"Open client finding" is an unmuted firing one.** A muted finding is listed on its row, marked, and does not lift the row. A finding matches when `ApplicationOf` of its entity key is the application, which covers `client-001`'s key, the whole names of `client-002` to `client-005`, and names seen only in events.
- **Tooling rows.** An application is `tooling` when every connection of it is; one with both kinds lists only the others while tooling is hidden. Events and rollup rows follow the same rule: tooling ones count only with `include_tooling`.
- **Account scope.** An empty account reads as `$G`, on connections, events and rollup rows alike; `accounts` names it `$G`.
- **More in the response than planned:** each row's client addresses (at most 10, with the total), as the owner's decision on "Runs on" asks the page to show them; the in and out totals in messages and bytes; `events_captured`. The summaries are messages of their own (`ApplicationSpread`, `LibraryUse`, `ApplicationsLastDay`), counted over every application before the limit.
- **A pod is the part between the first "@" and the next,** as `client-001` splits it.
- **Demo.** The demo's connection counters now grow at the rates its hourly traffic sums (they were fixed), and its go clients report go 1.54.0 (they reported nats.js's 3.0.2). `example-prod` holds 100 of its 187 connections on `nats-0` (61, 60 and 61 of 182 before), with `resize-worker`'s four pods and an unnamed script as extra connections; the Connections page, the Overview's clients tile (now 9 applications, counted from the connections), `ListAccounts`, the connection series, the restart reconnects among the events and the finding counts changed with it, and their tests say so. `example-global`'s `label-printer` slow consumer disconnects now carry their reason, which the 24-hour counts read.
- **Fix round 1** (from the review of 4f24651..18f62e3):
  - Reconnects pair per connection name, then sum: pairing the application's sums counted a rolling deploy (old pods dropping with "Read Error", new pods connecting) as reconnects with no finding behind them, and paired unrelated unnamed scripts.
  - The demo lists an extra connection with a start only from its start, and counts it so on the Connections and Applications pages, in the server and system counts, the accounts and the connection series.
  - `partial` and `missing` (fields 12, 13) say that the epoch is partial and which servers did not answer, as `ListServers` names them; the spread is never `concentrated` on a partial epoch. This follows `epochs.partial`, which an endpoint failure also sets, not only a missing server.
  - `capture_gaps` (field 14) says, by the Accounts and auth page's rule, that event capture stopped for part of the 24 hours. The counts stay set as lower bounds rather than unset: unsetting would hide the reconnects and slow consumers that were seen, and the UI can say the counts may be low. A boolean as on the Accounts page, not the gap's span.
  - A connection that reports neither library language nor version counts for no library.
  - Conformance rows for a partial epoch, capture that stopped, no captured event, and a muted client finding that does not lift its row; the demo's `example-global` has `client-001` on `shipping-service` (its `ap` instances at 140 ms), muted.
- **Fix round 2:** each row carries its connection names as `MicroService` does (`connection_names`, distinct, sorted, at most 3, an unnamed connection left out, and `more_names`, fields 26 and 27; the coordinator asked for `connection_names_total`, and the Micro services field names were followed instead), so the UI can tell an application whose connections are all named exactly like it, which the Events page's exact name filter finds, from one named with pods or an empty pod.
- **Fix round 3:**
  - `partial` and `missing` follow CONNZ, not `epochs.partial`: `missing` names the servers whose CONNZ request did not end ok and those that answered nothing, and the page is partial when it names one. A failed JSZ alone leaves the page whole, and a server that answered VARZ but not CONNZ is named.
  - Reconnects pair per name across accounts, as `client-004` does; a page scoped to an account counts each name of the application that has an event in that account. A name whose events all lie in other accounts does not count there. The store reads the day's events for every account.
- **Measurement** (`TestApplicationsSize`, laptop): the store's read for 4,096 connections of 500 applications, with a day of 30-second epochs, 20,000 client events and 14 days of the rollup, takes about 25 ms, and `FinishApplications` 4 ms.

## T3 UI

- `/applications` under Messaging (remove `planned` in `nav.ts`), with the account scope.
- The board: title and line ("N connections, M applications"), the three summary cards (one column at phone width), the table with the board's columns minus "Runs on", which becomes "Pods" (the first pod names and the count), the sparkline of messages in and out, the footnote (the "@" rule, instances by pod or by connection, tooling hidden, traffic lost for connections that closed between collections).
- Links: the application to the Connections page filtered by its name; a server to its page; the reconnect count to the application's `client-004` finding, else the Events page filtered by its name; findings to the finding page.
- Sorting by any column, kept in the address.
- Empty and partial states: no epoch, no application, tooling only, a cut list, less than 24 hours of rollup.
- Phone width, light and dark, accessibility as on the Micro services page; the previous answer stands in only for the same system and account.

### Where T3 departed from this plan

Decided in the session that built T3; proposed, not yet confirmed.

- **The sub-line under an application** is its connection count, its pod count where the names give pods, and its accounts when the page is not scoped and the rows span more than one account; the board's description line has no source. Its findings follow as links, a muted one marked "(muted)".
- **Pods lists the client addresses where no name gives a pod,** set apart in a quieter colour and named "client addresses" for screen readers, as the owner's decision on "Runs on" asks for both. Two pod names or one address show; the rest open behind a "+N more" disclosure, and a count the server did not list reads "N not listed".
- **Servers are chips with the server's name and the application's connections on it,** at most three, the rest behind a disclosure: the board's numbered chips do not fit 23 servers.
- **The reconnect count links to the Events page only when every connection is named exactly as the application** (`connection_names` is the application's name alone and `more_names` is 0, since T2's fix round 2). The Events page filters by the whole connection name, so `resize-worker@<pod>` or an empty pod `foo@` would find nothing; such a count stays plain text when no `client-004` finding is there to link to.
- **The application links to the Connections page with `?name=`,** which matches names that contain it, so `order-service` also lists an `order-service-v2`; a tooling row adds `tooling=1`. `(unnamed)` links nowhere.
- **A row with an unmuted finding has the warning background,** as rows that warn do on the Micro services page; reconnects above 0 are in the warning colour.
- **Library** shows one version, or "mixed" in the warning colour with each version and its connections beneath. **Pending** shows the sum, and "max" of one connection beneath it when the application has more than one connection and the sum is above 0.
- **The round trip keeps its fraction:** `durationMs` of the protobuf runtime rounds to whole milliseconds, so 0.69 ms read as "1 ms"; the page reads the duration itself. The Connections page still rounds (`adapt-entities.ts`, `rttMs: dur(c.rtt)`), outside this task.
- **Each sparkline has its own scale,** its row's busiest hour in or out; the footnote says so.
- **Messages in and out:** the sparkline draws in as a solid accent line and out as a dashed muted line on one scale; an hour without a measurement breaks the line, a lone measured hour is a dot, and no measured hour is a dashed baseline. The message totals stand beneath it; the accessible name gives the hours, both totals with their bytes, and "N of 24 hours measured" when not all were.
- **Partial traffic** is said once above the table when no row has all 24 hours measured, with the most hours any one row has ("No application has all 24 hours ... measured; the most any has is 6."); a row's own gaps are in its sparkline's name. Rows that together cover the day but none of them alone still get the note.
- **Cards.** The Spread card has the info edge when the spread is concentrated; otherwise it says "No server holds more than half", and with one server that a restart moves all connections. Client libraries names at most four versions and counts the rest. Last 24 h says "N applications reconnecting", and "Not known" when no event was captured.
- **Notices:** a cut connz list (warning), events not captured (info), the tooling hidden line as on the Connections page; the empty state says when only tooling connections are in the scope. The line under the title adds "the first 500 listed" when the list was cut.
- **Tooling toggle** in the address as `?tooling=1`, as on the Connections page, replacing the history entry; the previous answer stands in while the other answer loads, since the system and account are the same.
- **Width.** The table's cells have 8 px side padding and its headers wrap, so the ten columns fit 1440 px with the sidebar for the demo's systems; at phone width the table scrolls in its own box.

- **Following T2's fix rounds** (merged into `feat/apps-ui`):
  - A partial collection gets a warning notice naming the servers that did not answer ("nats-1 did not answer in this collection: its connections are not counted, and the counts, the spread and the traffic cover the servers that answered"), or, with none named, saying that part of the collection did not answer. The line under the title adds "over the servers that answered". The Spread card says it counts only the servers that answered and never says what a restart moves, also with one server left.
  - Event capture that stopped within the 24 hours (`capture_gaps`) gets an info notice; the Last 24 h card reads "at least N" for each count and a reconnect cell "≥N", spoken as "at least N". Events not captured at all keep their own notice and dashes.
  - The no-traffic note no longer gives a cause ("the hourly rollup holds none"), and the cut-list notice says the cut connections' traffic is not counted rather than "missing".
  - The footnote's reconnect sentence says the count pairs per connection name, so a rolling deploy is not a reconnect.
  - A library with neither language nor version no longer arrives; "library not reported" is gone from the words.

### API gaps seen while building T3

- The Events page filters by an exact connection name: no link can show the reconnect events of an application whose names carry pods. A prefix or application filter on `ListEvents` would let every reconnect count link there.
- The Connections page's name filter is "contains": no exact or application filter.
- Each row's slow consumers, stale connections and short-lived connections of the last 24 hours are in the response but not on the board's table; the page shows only their sums in the Last 24 h card.

## T4 Feature docs and audit

`docs/features/applications.md` to shipped, the README table and coverage map, `connections.md` (its "Planned" section), `client-checks.md`, `monitoring-snapshots.md` (the rollup), `demo-mode.md`, `navigation.md`, `docs/plans/next.md`. Then the audit.

## Leftovers

Recorded as they come up; moved to `docs/plans/next.md` item 5 when the work merges.

- "Runs on": the watcher would list client pods and store namespace, owner workload, node and IP (deferred by the owner, 2026-10-26).
- `example-prod`'s slow consumers exist only as counters (the Overview's clients tile says 7) without disconnect events, so its Applications page counts none and its Events page lists none. Adding the events would make `client-004` fire for `media-worker` during the upgrade, which the demo's findings would then need (seen while building T2).
- The writer inserts connections one row at a time: 4,096 connections take 2 to 4.5 s of an epoch's write on a laptop. Batching them as JSON, as `subscriptions` are, would cut that (seen while measuring T1; not part of it).
