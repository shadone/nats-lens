# Capacity and retention

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [streams.md](streams.md), [stream-checks.md](stream-checks.md), [server-checks.md](server-checks.md), [checks.md](checks.md), [triage-and-mutes.md](triage-and-mutes.md), [kv-and-object-stores.md](kv-and-object-stores.md)

## What it does

The Capacity page shows what each stream keeps, what bounds it, and when the disks fill, in the newest collection of the time range. One card per server shows its JetStream file store: how full it is, how fast it grows, and when it is full at that rate. A table lists every stream, KV bucket and object store with the limits that bound it, how far back its data reaches, the disk it takes over all its replicas, its growth, the limit it reaches next, and the decision on it. A stream that is unbounded on purpose can be accepted as by design, with a reason and a review date. `GetCapacity` answers the same in the API.

## Behavior and rules

- **The newest collection of the range.** The page reads the newest collection at or before the end of the range, and names it in the line under the header with the number of streams and stores and since when growth is read ("growth from 2026-10-20 12:00 UTC"), or "no growth rate yet (6 hours of history needed)" when no card or row has a rate. Growth covers the whole range, as on the Streams page. Without a collection the page says "No collection in this range." and nothing else.
- **One card per server.** "JetStream file store", the use, the limit and the share ("244 MiB of 50 GiB · 0.5%") and a bar. A server whose limit is unset or 0 shows its use and "no limit reported", without a bar. A server that did not answer in the collection reads "not reported in this collection". Servers without JetStream get no card; a line counts them. With more than one cluster, the cards are grouped under each cluster's name. Order: the soonest full first, then the fullest, then by name.
- **Growth and time to full.** The card's growth is the store's growth per day over the range (Theil-Sen, the median of the slopes between pairs of points, so a one-off restore or purge moves it little), and the time to full is the free space divided by that growth: "full in 9 days", "full in 6 months", "full in 3 years", "full in more than 5 years at this rate"; "not growing" at 0 or below; "not enough history for a rate (6 hours needed)" under 6 hours of data. This is the Streams page's growth rule, not the checks' rule (the smaller of two halves' slopes over 6 hours), so a store that grows by day and stands still by night keeps a rate all day.
- **The store's finding.** When `server-014` (store full soon) or `server-007` (store usage high) fires, the card names it with its severity and links it: "store full soon: 14 days by server-014" gives the check's own days, by its own rule, beside the page's. The bar takes the finding's colour, warning or critical; without a finding it is the accent.
- **One row per stream.** KV buckets (`KV_`) and object stores (`OBJ_`) are rows too. The name links to the stream's page. Under it: the replicas and the kind, such as "R3 · limits · 48,211 msgs", "R3 · interest", "R3 · work queue", "R3 · mirror of order-events", "R3 · sources 2 streams", "R3 · KV · 1 per key" (its history), "R3 · object store", with "memory" for memory storage. With more than one account, the account is named under it.
- **Bounded by.** The limits that are set, in the order age, messages, bytes, per subject: "7 days, 1M msgs", "8 GiB", "100M msgs per subject"; "nothing (keeps all)" without one. A KV bucket or object store reads "TTL 48 h", "keys (no TTL)" or "64 MiB, no TTL", and "per-key TTLs allowed" when its writers may set TTLs. An age under 3 days is written in hours ("24 h").
- **Data held.** A bar from the oldest retained message to the collection on a one-year scale, full beyond a year: the accent for a stream bounded by age, messages or bytes, the info colour for one that is not. Its label is the age limit once the oldest message has reached 95% of it ("7 days"), else "since 2026-02-11"; "empty" without messages; a KV bucket's keys ("41 keys").
- **Size × R and /day.** The stream's bytes and its growth per day, both times its replicas: the disk it takes over every copy. The growth is the Streams page's number times R. An en dash when a value is not reported or under 6 hours of data, never 0.
- **Next limit.** "empty"; a limit reached now: "steady at 30 days" (or "at 1M msgs") when the oldest messages are dropped, and "96,500 msgs: reached, refuses new messages" in the critical colour when the stream discards new ones; else the soonest limit at the stream's growth, "7 days: in 2.8 days", "8 GiB: in 41 days, then refuses new messages"; "1M msgs: not reached" when nothing grows towards a limit; "per subject only: not forecast" for a per-subject limit alone, as nats-lens does not see counts per subject; "none: grows until the store is full" without a limit. A limit counts as reached at 95% of the age or 99% of the messages or bytes (proposed, not yet confirmed).
- **Status.** "full in 13 days", linking `stream-006`, in the warning colour, with "muted until …" under it when that finding is muted. "BY DESIGN" with "Accepted by <who>: <reason>. Review on <date>." for an accepted `stream-005`, or with "declared in the inventory" or "bounded by its writers' TTLs" when the inventory makes the stream unbounded on purpose. "muted until <time> UTC", linking the finding, for a muted `stream-005`. The "Accept as by design…" button and a link to `stream-005` for one awaiting a decision. "unbounded" for an unbounded stream without a `stream-005` finding (the check is turned off or did not run). "● bounded" for every other stream.
- **Order and length.** Streams that fill soon first (soonest first), then those awaiting a decision, then those that fill soon under a mute, then by size times replicas, most first. At most 500 rows; when the list is cut, the page says "The first 500 of N streams are listed."
- **Accept as by design.** The button opens a dialog that names the finding ("Accepts stream-005 Stream without any limit for order-archive") and asks for a reason (at most 1000 characters) and a review date: in 3 months (the default), in 6 months, in 1 year (364 days), or on a date between a day and 364 days ahead, the end of that day in UTC; the API allows 365 days, and the dialog keeps a day inside it so a browser clock a little ahead is not refused (proposed, not yet confirmed). It records who accepted it as a mute does ([triage-and-mutes.md](triage-and-mutes.md)). After saving, the row reads BY DESIGN at once. The finding then counts as muted until the review date: it leaves the Overview's status and tally and the Findings badge. nats-lens ends the acceptance early when the stream's growth over a week changes by more than half of the growth recorded at acceptance, or of 1 MiB a day when that is larger, and the finding counts again ([checks.md](checks.md)). The page offers acceptance only for `stream-005`; a stream that fills soon is muted, not accepted (proposed, not yet confirmed).
- **The note under the table** says how to read the bar and what acceptance does.
- **At a phone width** the cards stack in one column; the table scrolls inside its card with the stream names kept in view, so the status and its button are reached by scrolling the table.

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
- **Then** the row reads "BY DESIGN" with "Accepted by <me>: <reason>. Review on <date>." without a reload
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
- **Then** every card reads "not enough history for a rate (6 hours needed)" and every /day cell an en dash

### Ask the API when a stream is full

- **Surfaces:** `api`
- **Given** a stream with `max_bytes` that grows steadily
- **When** a client calls `GetCapacity` for the system
- **Then** it receives the stream's next limit, bytes, and the days until the stream reaches it, with the server cards and their days to full

## Planned

- **Account disk limits as a bound.** An account's JetStream limits, collected now in operator mode, as a bound and an account card.
- **Object counts.** The number of objects in an object store.
- **Memory stores.** A card for each server's memory store.

## Not supported / out of scope

- **No account disk limits on the page.** Operator mode's account JetStream limits are collected and shown on the Accounts page, but the Capacity page does not bound streams by them.
- **No forecast per subject.** NATS servers do not report message counts per subject, so a per-subject limit is not forecast.
- **No object counts.** The servers report an object store's chunks and metadata messages together, so its row shows its size and age, not its objects.
- **No memory store cards.** Memory storage streams are rows, marked "memory", but their bytes are not on the file store cards.
- **No acceptance of other checks on the page.** The page accepts `stream-005` only; a finding that fills a store soon (`stream-006`, `server-014`) is muted from its finding page.
- **No resizing.** nats-lens does not change limits or volumes.
