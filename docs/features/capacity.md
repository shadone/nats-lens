# Capacity and retention

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [data-history.md](data-history.md), [streams.md](streams.md), [stream-checks.md](stream-checks.md), [server-checks.md](server-checks.md), [checks.md](checks.md), [triage-and-mutes.md](triage-and-mutes.md), [kv-and-object-stores.md](kv-and-object-stores.md)

## What it does

The Capacity page ranks measured storage changes and shows what each resource keeps, what bounds it, and when the disks fill, in the newest collection of the time range. One card per server shows its JetStream file store: how full it is, how fast it grows, and when it is full at that rate. Memory store cards follow when anything is held in memory, and in operator mode one card per account limit shows the account's use against its JWT's JetStream limit. A table lists every stream, KV bucket and object store with the limits that bound it, how far back its data reaches, its logical bytes times configured replicas, its growth, the limit it reaches next, and the decision on it. A stream that is unbounded on purpose can be accepted as by design, with a reason and a review date. `GetCapacity` answers the same in the API.

Shared storage charts offer portable [agent investigation context](agent-investigations.md), including known metric and resource IDs.

## Behavior and rules

- [Chart interval selection](chart-interval-selection.md) adds explicit storage-time selection and actions. Selection alone leaves navigation and the page range unchanged.

- **Change in a selected interval.** Resources by measured change lists resource incarnations observed at either endpoint, including streams, KV buckets, object stores and resources absent from the newest collection. Change in range shows signed one-copy logical byte deltas, before/after values and actual collection windows. Short intervals work when distinct fresh observations permit; daily growth is independent.
- **Signed ordering.** The default ranking uses the largest absolute logical change. Change in range switches to largest signed increase, then smallest signed change. Sorting and contributor type filters apply to the complete bounded comparison before paging. Unknown changes stay last. The URL retains the sort and type selection. Decorative bars compare signed changes against the largest known magnitude on the current page; they do not assign severity.
- **Frozen evidence.** Proposed, not yet confirmed: a live selection resolves against the source clock and stays frozen until Refresh comparison or a scope/range change. Open pinned investigation records requested bounds, resolved system and available collection pins. Pagination follows one pinned comparison explicitly; filtering resets its page. Unknown, missing, stale and identical endpoint evidence cannot become a measured zero.
- **Contributor drill-down.** Explore opens before/after measurements, configured-replica changes, process identities and known lifecycle events. Two interval charts separate one-copy bytes from observed copies. Historical configuration links select endpoint windows; event and consumer links retain the resource account and requested range. Consumers show the latest available collection in that interval. Charts read retained history; these reads are not a universal pinned snapshot. Correlation and backlog do not establish causation.
- **Comparison totals.** File and memory totals stay separate and cover every resource type in the account scope, even when rows are filtered. Known and off-page contributions retain unknown-row counts. The unreconciled difference compares system-wide server usage with complete observed-copy change, not physical allocation. The existing bounds table and forecasts remain a separate latest-collection view.

- **Chart descriptions.** Expanded histories and compact resource-column histories use [shared chart help](chart-help.md), with logical resource and observed replica measurements kept distinct.

- **Missing growth history.** One page note explains the six-hour requirement and links to Data history. Server cards say “growth unavailable”; unset table rates remain unknown. The note distinguishes short retained history from missing boundary observations or entity measurements. A shorter range asks for at least six hours. The age of retained history never supplies a countdown. Storage charts remain available independently of growth eligibility and share collection-history overlays. Forecast confidence margins are not shown.

- **The newest collection of the range.** The page reads the newest collection at or before the end of the range, and names it in the line under the header with the number of resources and stores and since when growth is read ("growth from 2026-10-20 12:00 UTC"), or an explanation of the history needed when no card or row has a rate. Growth follows the bounded observation policy on the [Streams page](streams.md). The page discloses the lookback and collection bounds; an individual entity can have a shorter span. Without a collection the page says "No collection in this range." and nothing else.
- **Storage history.** A total JetStream file-store usage chart sits above the server cards. Each card has a server file-store chart. Resource rows have a compact logical-byte chart and an Expand history button for logical and observed-replica charts. All charts use the selected range, including short and custom ranges. Pointer and keyboard inspection names UTC bucket timestamps and values. Readouts use the shared viewport overlay, escape table clipping and allow long content to scroll without shrinking the plot. Charts reuse the collection and retention gap marks described in [Data history](data-history.md). Growth and forecasts need six hours; charts do not.
- **Partial storage buckets.** Each mark sits at the midpoint of the bucket's overlap with the selected range. This position represents a bucket mean, not an exact observation time. Inspection retains the original aligned bucket label and the range-clipped bucket end. A lone first or last partial bucket remains visible. Buckets with no overlap are excluded; the selected end is exclusive. Proposed, not yet confirmed: this midpoint display policy.
- **Three byte measurements.** Server store usage comes from server file-store reports and excludes memory resources. Logical resource bytes use the leader report, or the largest report when no leader answers. Observed replica bytes sum the resource bytes in reported replicas, including memory resources when the resource uses memory. Missing replicas are not estimated. Resource bytes do not measure physical disk overhead. Size × R and /day multiply logical values by configured replicas and remain estimates.
- **Missing storage reports.** Total history is unknown when expected servers are missing, storage reports are absent, or simultaneous processes share a server name. A server history continues across restarts but is unknown for simultaneous processes with that name. A server with JetStream disabled contributes zero. A bucket containing an unknown server-store observation stays unknown. Other byte charts show means of available reports in each bucket; missing replicas can reduce observed-replica bytes. Missing measurements are never filled with zero. Compact and expanded traces break at each absent measurement bucket, even when collections continue. Real zero values and isolated samples remain visible; inspection reports “No sample” in absent buckets.
- **Bounded history reads.** Server cards show pages of 24; resource rows show pages of 16 after the type filter. The page names the displayed row scope and the number of loaded resources. The first 500 resources can be loaded; pagination does not fetch omitted resources. Total history, server cards, and visible resources use separate bounded batch requests with 96 buckets per series. Expanding one resource adds its observed-replica series to the resource batch. Changing system, range, or row page never displays another selection's cached history, and abandoned requests are canceled. The total remains independent of row pages and type filters. A shorter list clamps the current page; Previous acts from that visible page. Empty lists discard old pages. System, range and resource-type changes start at the first page. Growing a list or returning to a filter does not restore an old hidden page. Proposed, not yet confirmed: the page sizes and resetting pages on scope or type changes.
- **One card per server.** "JetStream file store", the use, the limit and the share ("244 MiB of 50 GiB · 0.5%") and a bar. A server whose limit is unset or 0 shows its use and "no limit reported", without a bar. A server that did not answer in the collection reads "not reported in this collection". Servers without JetStream get no card; a line counts them. With more than one cluster, the cards are grouped under each cluster's name. Order: the soonest full first as the card words it (two cards that read alike, such as "full in more than 5 years", tie), then the fullest, then by name, so stores that fill alike keep their order between refreshes.
- **Memory stores.** Under the heading "Memory stores", after the file store cards, one card per server reads "JetStream memory store" with the same words as a file store card: the use, the limit and the share ("38 MiB of 2 GiB · 1.9%") and a bar; "no limit reported" without a bar when the limit is unset or 0; "not reported in this collection" for a server that did not answer; growth and time to full by the rule below, and "growth unavailable" without 6 hours of data. The group appears only when a server that answered holds memory in use above 0, or a stream of the collection uses memory storage; a system that keeps everything on file shows no memory cards. Memory cards have no history chart and name no finding: `server-007` reads both stores, and its finding stays on the file store card. Memory store bytes are not in the file store cards, the total chart or the file store comparison totals. Proposed, not yet confirmed: this layout, a separate group after the file store cards, and when it appears.
- **Account limits.** In operator mode, under the heading "Account limits", one card per account and storage whose account JWT sets a JetStream limit above 0: the account's name, "JetStream file limit" or "JetStream memory limit", the use, the limit and the share ("330 GiB of 500 GiB · 66%") and a bar, then growth and time to full by the rule below. Use is the account total in bytes counted over replicas, the largest any server reports for the account in the collection, as the [Accounts page](accounts-and-auth.md) reads it; growth is the Theil-Sen slope of that largest report per collection over the range. A card whose account no server reported reads "limit 500 GiB; use not reported in this collection". The limit is the newest lookup the Accounts page also reads (at most a day older than the collection). An account without a JWT, or whose limit is -1 (no limit) or 0 (none allowed), gets no card, and its streams read as before. Order: the soonest full first, as the card words it, then the fullest, then by account, file before memory. A note under the heading says the limit is shared by the account's streams of that storage. Proposed, not yet confirmed: the cards, their place after the memory stores, and reading the largest report as the account's use.
- **Growth and time to full.** The card's growth is the store's growth per day over the range (Theil-Sen, the median of the slopes between pairs of points, so a one-off restore or purge moves it little), and the time to full is the free space divided by that growth: "full in 9 days", "full in 6 months", "full in 3 years", "full in more than 5 years at this rate"; "not growing" at 0 or below; an explanation of the missing history under 6 hours of data. This is the Streams page's growth rule, not the checks' rule (the smaller of two halves' slopes over 6 hours), so a store that grows by day and stands still by night keeps a rate all day.
- **The store's finding.** When `server-014` (store full soon) or `server-007` (store usage high) fires, the card names it with its severity and links it: "store full soon: 14 days by server-014" gives the check's own days, by its own rule, beside the page's. The bar takes the finding's colour, warning or critical; without a finding it is the accent.
- **One row per resource.** The Resource column marks Stream, KV bucket or Object store. Buckets use their bucket name and link to their dedicated detail page; their underlying stream identity is shown underneath and remains unchanged in API evidence. Ordinary streams link to their stream page. Links carry the system, account and time range. Under it: the replicas and the kind, such as "R3 · limits · 48,211 msgs", "R3 · interest", "R3 · work queue", "R3 · mirror of order-events", "R3 · sources 2 streams", "R3 · KV · 1 per key" (its history), "R3 · object store", with "memory" for memory storage. With more than one account, the account is named under it.
- **Bounded by.** The limits that are set, in the order age, messages, bytes, per subject: "7 days, 1M msgs", "8 GiB", "100M msgs per subject"; "nothing (keeps all)" without one. A KV bucket or object store reads "TTL 48 h", "keys (no TTL)" or "64 MiB, no TTL", and "per-key TTLs allowed" when its writers may set TTLs. An age under 3 days is written in hours ("24 h"). A resource without an age, message or byte limit of its own whose account JWT sets a limit for its storage names that limit, marked as shared by the account's streams: "account limit 500 GiB (shared)" in place of "nothing (keeps all)", or after a per-subject bound ("keys (no TTL), account limit 500 GiB (shared)"). A resource with a limit of its own shows only its own limits here.
- **Data held.** A bar from the oldest retained message to the collection on a one-year scale, full beyond a year: the accent for a stream bounded by age, messages or bytes, the info colour for one that is not. Its label is the age limit once the oldest message has reached 95% of it ("7 days"), else "since 2026-02-11"; "empty" without messages; a KV bucket's keys ("41 keys").
- **Size × R and /day.** The stream's bytes and its growth per day, both times its replicas: an estimate from the logical value and configured replicas, not an observed physical measurement. The growth is the Streams page's number times R. An en dash when a value is not reported or under 6 hours of data, never 0.
- **Next limit.** "empty"; a limit reached now: "steady at 30 days" (or "at 1M msgs") when the oldest messages are dropped, and "96,500 msgs: reached, refuses new messages" in the critical colour when the stream discards new ones; else the soonest limit at the stream's growth, "7 days: in 2.8 days", "8 GiB: in 41 days, then refuses new messages"; "1M msgs: not reached" when nothing grows towards a limit; "per subject only: not forecast" for a per-subject limit alone, as nats-lens does not see counts per subject; "none: grows until the store is full" without a limit. A limit counts as reached at 95% of the age or 99% of the messages or bytes. When the account JWT sets a limit for the resource's storage, that limit is the next limit where the resource's own does not come first: for a resource without an age, message or byte limit, and for one whose own forecast is later than the account's time to full or has none: "account limit 500 GiB (shared): account full in 35 days". The days are the account's time to full, not a forecast of the resource alone, because the limit is shared by all the account's streams; "account limit 500 GiB (shared): not reached" when the account does not grow or has no rate (only for a resource without an age, message or byte limit); "account limit 500 GiB (shared): reached, refuses new messages" in the critical colour when the account holds 99% of its limit. An empty resource, and one at its own limit now (steady or refusing), keeps its own words. Proposed, not yet confirmed: naming the account's time to full on each of its streams.
- **Status.** "full in 13 days", linking `stream-006`, in the warning colour, with "muted until …" under it when that finding is muted. "By design" with "Accepted by <who>: <reason>. Review on <date>." for an accepted `stream-005`, or with "declared in the inventory" or "bounded by its writers' TTLs" when the inventory makes the stream unbounded on purpose. "muted until <time> UTC", linking the finding, for a muted `stream-005`. The "Accept as by design…" button and a link to `stream-005` for one awaiting a decision. "unbounded" for an unbounded stream without a `stream-005` finding (the check is turned off or did not run), or "account limit only" when its account JWT sets a limit for its storage. "● bounded" for every other stream. An account limit does not change `stream-005`: the check reads the stream's own limits, so a stream bounded only by its account still awaits a decision or reads as accepted, muted or declared. Proposed, not yet confirmed: the words "account limit only".
- **Resource filter.** All (the default), Streams, KV buckets and Object stores filter loaded bounds-table rows and the complete bounded contributor ranking. The `resource` URL parameter retains the selection across reloads and history; unknown values mean All. Counts describe the loaded rows. Server cards and page totals still include every resource type. The bounds table filters locally; contributor filters use the comparison API.
- **Order and length.** Resources that fill soon first (soonest first), then those awaiting a decision, then those that fill soon under a mute, then by size times replicas, most first. At most 500 rows; when the list is cut, the page says "The first 500 of N resources are loaded. Type filters apply only to these loaded resources."
- **Accept as by design.** The button opens a dialog that names the finding ("Accepts stream-005 Stream without any limit for order-archive") and asks for a reason (at most 1000 characters) and a review date: in 3 months (the default), in 6 months, in 1 year (364 days), or on a date between a day and 364 days ahead, the end of that day in UTC; the API allows 365 days, and the dialog keeps a day inside it so a browser clock a little ahead is not refused. It records who accepted it as a mute does ([triage-and-mutes.md](triage-and-mutes.md)). After saving, the row reads By design at once; on a fixed past range, whose answers apply the mutes in force at its end, it keeps reading so while the page stays open. The finding then counts as muted until the review date: it leaves the Overview's status and tally and the Findings badge. nats-lens ends the acceptance early when the stream's growth over a week changes by more than half of the growth recorded at acceptance, or of 1 MiB a day when that is larger, and the finding counts again ([checks.md](checks.md)). That needs a `retention` of at least 8 days and an hour (14 days by default); with less, an acceptance ends only at its review date, and nats-lens warns at start. The page offers acceptance only for `stream-005`; a stream that fills soon is muted, not accepted.
- **The note under the table** says how to read the bar and what acceptance does.
- **At a phone width** the cards stack in one column and compact history charts stay within their resource cells; the table scrolls inside its card with the stream names kept in view, so the status and its button are reached by scrolling the table.

## Scenarios

### Find what fills the disk

- **Surfaces:** `ui`
- **Given** `example-global`'s `nats-ap-2` is 71% full and grows +1.1 GiB a day, and `server-014` fires for it
- **When** I open Capacity
- **Then** its card reads "71 GiB of 100 GiB · 71%", "+1.1 GiB/day · full in 26 days" and "store full soon: 14 days by server-014", in the warning colour
- **And** the streams table shows each stream's size times replicas and growth, `order-archive` the largest at +3.3 GiB a day

### Accept an archive that keeps everything

- **Surfaces:** `ui`
- **Given** `example-prod`'s `order-archive` has no limits on purpose and `stream-005` awaits a decision
- **When** I choose "Accept as by design…", give the reason, keep "in 3 months" and save
- **Then** the row reads "By design" with "Accepted by <me>: <reason>. Review on <date>." without a reload
- **And** the finding reads as accepted on the Findings page and on its own page, and leaves the Overview's status until the review date

### An acceptance without a reason is refused

- **Surfaces:** `ui`
- **Given** the Accept as by design dialog is open
- **When** I leave the reason empty and save
- **Then** the dialog says "Give a reason; it is shown with the accepted finding." and nothing is accepted

### A stream that will refuse messages

- **Surfaces:** `ui`
- **Given** `example-prod`'s `audit-archive` has a `max_bytes` with discard new and grows towards it
- **When** I open Capacity
- **Then** its next limit reads "1.3 GiB: in 13 days, then refuses new messages" and its status "full in 13 days", linking `stream-006`, with the mute under it

### A young system without a rate

- **Surfaces:** `ui`
- **Given** `example-staging` has under 6 hours of collections
- **When** I open Capacity
- **Then** the page explains the short retained history once, cards say “growth unavailable”, and unknown /day cells show an en dash

### Read the memory stores

- **Surfaces:** `ui`
- **Given** `example-global` holds the memory bucket `KV_session-cache` on `nats-eu-0`, `nats-eu-3` and `nats-eu-6`, and each server's memory store limit is 2 GiB
- **When** I open Capacity
- **Then** a "Memory stores" group follows the file store cards, one card per server reading "JetStream memory store"; `nats-eu-0`'s reads "38 MiB of 2 GiB · 1.9%" and the others' "0 B of 2 GiB · 0%"
- **And** `example-staging`, which holds nothing in memory, shows no memory store cards

### An account limit bounds a stream without its own

- **Surfaces:** `ui`, `api`
- **Given** `example-global`'s account APP has a JWT that allows 500 GiB of file storage, and its `order-archive` has no limit of its own
- **When** I open Capacity
- **Then** an "Account limits" card reads APP's use of 500 GiB with its share, growth per day and time to full
- **And** `order-archive` is bounded by "account limit 500 GiB (shared)" and its next limit reads "account limit 500 GiB (shared): account full in" the account's time to full
- **And** `example-prod`, in config mode without account JWTs, shows no account cards and its unbounded streams read "nothing (keeps all)" as before

### Ask the API when a stream is full

- **Surfaces:** `api`
- **Given** a stream with `max_bytes` that grows steadily
- **When** a client calls `GetCapacity` for the system
- **Then** it receives the stream's next limit, bytes, and the days until the stream reaches it, with the server cards and their days to full

### Six hours with complete observations

- **Surfaces:** `ui`, `api`
- **Given** observations for a server and stream span six hours, including an observation within two collection intervals before the selected start
- **When** I select the last six hours
- **Then** the API returns measured growth and Capacity discloses its bounded lookback and collection bounds
- **And** an entity whose own observations span less than six hours keeps its rate unset
- **And** a missing observation at the range end is never replaced with a future observation

### Investigate a bucket from Capacity

- **Surfaces:** `ui`
- **Given** Capacity contains ordinary streams, KV buckets and object stores
- **When** I select KV buckets
- **Then** the table shows only loaded KV rows with bucket names, type labels and underlying stream identities
- **And** the counts say the filter applies to loaded resources while server cards and page totals include all types
- **And** a bucket link retains the system, account and time range and opens its bucket details

### Choose a range long enough for growth

- **Surfaces:** `ui`
- **Given** the system has enough history but the page shows the last hour
- **When** I inspect an unset growth rate
- **Then** the page asks for a range of at least six hours and links to Data history

### Inspect storage before growth is available

- **Surfaces:** `ui`, `api`
- **Given** a five-minute custom range contains stored file-store and resource reports but no six-hour growth estimate
- **When** I inspect Capacity and expand a resource's history
- **Then** total, server, logical-byte, and observed-replica charts cover those five minutes and expose UTC timestamps and values by pointer and keyboard
- **And** memory resource bytes remain outside file-store totals, and missing replicas are not supplied by configured replica counts

### Inspect a short range inside one bucket

- **Surfaces:** `ui`
- **Given** a report at 12:00:20 belongs to the 12:00:00 bucket with a 30-second interval
- **When** I select 12:00:15–12:00:25
- **Then** total, server, compact logical, and expanded resource charts show a mark inside the plot
- **And** inspection names the 12:00:00 bucket and its mean, without claiming an exact observation timestamp
- **And** a report at the exclusive range end contributes no value

### Read bounded history for filtered resources

- **Surfaces:** `ui`, `api`
- **Given** 500 of 812 resources are loaded
- **When** I filter the resource type and move to the next history page
- **Then** at most 16 matching loaded resources appear with their histories, the row scope and truncation remain explicit, and the total chart still covers every server

### Inspect a missing measurement bucket

- **Surfaces:** `ui`
- **Given** a storage series has reports at 12:00 and 12:01 with a 30-second bucket and no report at 12:00:30
- **When** I inspect its compact or expanded chart
- **Then** the trace breaks between the reports and the middle bucket says “No sample” by pointer and keyboard
- **And** zero reports remain zero and isolated reports remain points

### Navigate after a list shrinks

- **Surfaces:** `ui`
- **Given** I am on server cards 73–80 and the next response contains 25 servers
- **When** I select Previous servers from cards 25–25
- **Then** cards 1–24 appear after one click
- **And** resource pages also clamp when their loaded list shrinks, without restoring hidden pages when it grows

## Planned

- **Object counts.** The number of objects in an object store.

### Find a decrease in a short interval

- **Surfaces:** `ui`, `api`
- **Given** a short interval has comparable observations, including a deleted KV bucket, a growing stream and missing replica evidence
- **When** I sort Change in range and explore a contributor
- **Then** increases and decreases remain visible, while unknown measurements stay unknown
- **And** the charts, configuration windows, events and consumers retain scope without claiming causation

## Not supported / out of scope

- **No physical disk measurement per resource.** Observed replica bytes count reported logical bytes, without filesystem overhead or estimates for missing copies.

- **Bounds-table counts remain local.** Its counts cover only the at-most-500 loaded newest-collection rows. The contributor table filters its complete bounded comparison; its totals retain all resource types.

- **No forecast of a stream against its account limit.** An account limit is shared by all the account's streams, so the page gives the account's time to full, not when one stream would reach it.
- **No account tiers on the page.** When an account JWT sets per-replica tiers instead of an account-wide limit, the card compares the account's total use with the sum of the tiers (each tier's limit times its replicas), as the Accounts page reads it; one tier can fill before that sum is reached, and the page does not show each tier.
- **No forecast per subject.** NATS servers do not report message counts per subject, so a per-subject limit is not forecast.
- **No object counts.** The servers report an object store's chunks and metadata messages together, so its row shows its size and age, not its objects.
- **No memory store history.** Memory store cards have no history chart, and no check's finding is named on them.
- **No acceptance of other checks on the page.** The page accepts `stream-005` only; a finding that fills a store soon (`stream-006`, `server-014`) is muted from its finding page.
- **No resizing.** nats-lens does not change limits or volumes.
