# Capacity build plan

Working plan for the Capacity page (`docs/features/capacity.md`, board `Capacity.dc.html` of the design canvas). Everything here was proposed by the planning session of 2026-10-21 and is not yet confirmed by the owner. When a task lands, its lasting content moves into `docs/features/capacity.md`, `docs/features/triage-and-mutes.md` and `docs/design.md`; this file then only tracks status.

The board's sample names come from a sketch of a real deployment. Use the example shop of `inventory.example.yaml` and the demo's systems (`example-global`, `example-prod`, `example-staging`) instead; no name from the board goes into the repository.

## Status

| Task | Status |
|---|---|
| T1 Accept a finding as by design | built on `feat/capacity-accept`, in review; departures below |
| T2 API, store and demo | planned, after T1 is merged |
| T3 UI | planned, after T2 is merged |
| T4 Feature docs and audit | planned |

## Rules for every step

- No collector change. `server_stats` (`js_storage`, `js_max_storage`, `js_enabled`), `stream_stats` (`bytes`, `messages`, `first_ts`, `num_subjects`, `is_leader`), `stream_config` (`retention`, `storage`, `replicas`, `max_msgs`, `max_bytes`, `max_age_ns`, `max_msgs_per_subject`, `discard`, `mirror`, `sources`, `allow_msg_ttl`), `findings`, `mutes` and `inventory` hold every value below. One migration, `0031_accepted_findings.sql` (T1), adds the acceptance columns to `mutes`.
- The page shows the newest collection at or before the end of the page range (the epoch rule of `GetPlacement`). Growth covers the whole page range, as on the Streams page.
- A value a server did not report is unset, never 0. An empty stream (no messages) has no `first_ts`: the writer already stores the zero time as NULL (`internal/store/values.go`), and the page reads it as "empty", not as a date.
- One growth rule on the page: `api.RobustPerDay` (Theil-Sen over the range, unset under 6 hours). See "Growth and time to full" below for why it is not the checks' rule.
- Acceptance is a kind of mute. Every place that reads mutes today (the Overview tally and status, the Fleet badges, `/metrics`, the status timeline's SQL, the archive's `mute_changes`) then treats an accepted finding as muted without a change of its own; the UI words it as accepted.

## What backs each part

| Part | Source | Rule |
|---|---|---|
| Server cards | `server_stats` of the epoch, servers in `epochs.expected_names` with `js_enabled` | One card per server: "JetStream file store", `js_storage` of `js_max_storage` and the share. A server whose JSZ did not answer in the epoch: "not reported in this collection". `js_max_storage` unset or 0: the use alone, "no limit reported". Servers without JetStream get no card; one line counts them. |
| Server growth | `server_stats.js_storage` over the range, per server name | `RobustPerDay`. Unset under 6 hours of data. |
| Time to full | the card's use, limit and growth | (limit − use) / growth when growth is above 0. Under 60 days: "full in N days"; up to 5 years: "full in N months" or "N years"; beyond: "full in more than 5 years at this rate"; growth at or below 0: "not growing"; growth unset: "not enough history for a rate (6 hours needed)". |
| Server finding | `findings` firing for the server: `server-014`, else `server-007` | The card links it, in the finding's severity colour, with the check's own days for `server-014` ("store full soon: 9 days by server-014"). |
| Stream rows | `stream_config` and `stream_stats` of the epoch | One row per (account, stream), from the leader's row (else the first reporting server by name, as `stream-005` picks one). KV buckets (`KV_`) and object stores (`OBJ_`) by the prefix rule of `ListKeyValueStores`. |
| Kind line | `stream_config` | "R3 · limits · 48,211 msgs", "R3 · interest", "R3 · work queue", "R3 · mirror of order-jobs", "R3 · sources 2 streams", "R3 · KV · 1 per key" (history is `max_msgs_per_subject`), "R3 · object store"; "memory" is added for memory storage, whose bytes are not on the file store cards. |
| Bounded by | `stream_config` limits | The limits that are set, in the order age, messages, bytes, per subject: "7 days, 1M msgs", "8 GiB", "100M msgs per subject". None: "nothing (keeps all)". A KV bucket: "TTL 48 h" or "keys (no TTL)", plus "per-key TTLs allowed" when `allow_msg_ttl`. |
| Data held | `stream_stats.first_ts` of the leader, the epoch time | A bar from the oldest retained message to the collection on a one-year linear scale, full beyond a year. Label: the age limit ("7 days") when the oldest message is at 95% of `max_age` or more, else "since 2026-02-11"; "empty" with no messages; a KV bucket "214 keys" (`num_subjects`). Bar colour: the accent for a bounded stream, the info colour for an unbounded one, as the board. |
| Size × R | leader `bytes`, `replicas` | The disk the stream takes over all its replicas. |
| /day | leader `bytes` over the range, times `replicas` | `RobustPerDay`, the same number as the Streams page's growth times R. Unset under 6 hours: an en dash. |
| Next limit | limits, leader `bytes`, `messages`, `first_ts`, their growth | See "Next limit" below. |
| Status | `findings`, `mutes` (with T1's acceptance), `inventory` | See "Status" below. |

Order of the streams: a firing `stream-006` first (soonest first), then unbounded rows awaiting a decision, then every other row by Size × R descending, then by name. At most 500 rows, with the total. Server cards: the soonest full first, then the fullest, then by name; for more than one cluster, grouped by cluster in name order.

### Growth and time to full

The feature doc says forecasts use the checks' rule (the smaller of the two halves' slopes over a 6-hour lookback, both positive). The page uses `RobustPerDay` over the page range instead. Proposed, not yet confirmed, because:

- The card shows a rate and a date from it. With the checks' rule over 6 hours, a store that grows by day and stands still by night has no rate half the time, and "full in 3 years" turns into nothing an hour later; a page range of a day or a week smooths that.
- The Streams page and the storage tile already show `RobustPerDay`; the same stream would otherwise show two different growth numbers on two pages.
- Theil-Sen is the median of pairwise slopes, so a one-off step (a restore, a purge) moves it no more than the halves rule is meant to stop.
- The checks keep their rule for firing. When `server-014` or `stream-006` fires, the card or row shows the check's own days beside the link, named as the check's, so a person sees both numbers and where each comes from.

### Next limit

Per row, in this order:

1. No messages: "empty".
2. A limit reached now: the oldest message at 95% of `max_age` or more, or `messages` at 99% of `max_msgs` or more, or `bytes` at 99% of `max_bytes` or more. With discard old (or a limits stream): "steady at 30 days" (or "at 1M msgs"); with discard new: "1M msgs: reached, refuses new messages", in the critical colour.
3. Else, for each limit that is set, the days until it is reached: age `max_age − held`; messages and bytes from the leader's growth (`RobustPerDay` of `messages` and of `bytes` over the range), when above 0. The soonest: "7 days: in 3 days", "8 GiB: in 41 days" ("then refuses new messages" with discard new). A limit without a forecast reads "1M msgs: not reached" (the first in the order age, messages, bytes).
4. Only a per-subject limit: "per subject only: not forecast" (nats-lens does not see per-subject counts).
5. No limit: "none: grows until the store is full".

The thresholds of 95% and 99% are proposed, not yet confirmed: age is measured at collections up to one interval apart, and a counter limit is enforced exactly.

### Status

| State | When | Shown |
|---|---|---|
| Fills soon | `stream-006` fires for the stream | A warning (or critical) tag "full in 9 days", linking the finding. |
| Accepted | `stream-005` fires and an acceptance (T1) covers it | "BY DESIGN"; under it "Accepted by <who>: <reason>. Review on <date>." |
| Declared | `stream-005` does not fire because the inventory declares the stream unbounded (`max_age` 0 and `max_bytes` -1), or lists the KV bucket with `allow_msg_ttl` | "BY DESIGN", with "declared in the inventory" or "bounded by its writers' TTLs" under it. |
| Muted | `stream-005` fires under a mute | "muted until <time>", linking the finding; no button while muted. |
| Awaiting a decision | `stream-005` fires, not muted or accepted | The "Accept as by design…" button, and a link to the finding. |
| Unbounded, no finding | no limit, not declared, and no `stream-005` finding (the check is turned off, or did not run in this epoch) | "unbounded" in the muted colour, no button. |
| Bounded | any other row | "● bounded". |

A row with both `stream-006` and an accepted `stream-005` cannot occur (`stream-006` needs `max_bytes`, `stream-005` its absence).

Not backed, so not shown: object counts of an object store (the board's "3 objects"; JSZ gives chunks and metadata messages together, so the row shows its size and age only); what a bucket's keys grow with (the board names the business entity behind the keys, which is a person's knowledge, not data); a "test bucket: delete?" hint; account JetStream limits as a bound (now collected for operator mode in `account_details`, but not on this page; a leftover).

## T1 Accept a finding as by design

The first part of "Accept a finding with a review date" of `docs/features/checks.md` and `docs/features/triage-and-mutes.md`, scoped to what the board needs.

- **Scope.** One finding of one entity: a reason (at most 1000 characters), who accepted it (IAP identity or the typed name, as a mute), and a review date between one day and 365 days ahead. The acceptance applies when findings are read, as a mute does, and ends early when the entity's growth changes by more than 50% (below). The API accepts any check for one entity; the Capacity page offers it only for `stream-005`. Not in this task: acceptance of related entities or of a check everywhere, a "Review accepted findings" list, accepting from the finding page; each goes to the leftovers.
- **Why `stream-005` only on the page.** `stream-005` is the finding of an unbounded stream, which is what "by design" means on the board. `stream-006` is a forecast that the stream will refuse or drop messages soon, and `server-014` that a whole store fills; accepting either hides an outage on its way, and a mute with an end in at most 90 days is the right tool for those. Proposed, not yet confirmed.
- **Storage.** Migration `0031_accepted_findings.sql`: `mutes.accepted BOOLEAN DEFAULT false` (NULL read as false, for rows from before), `mutes.growth_per_day DOUBLE` (the entity's growth at acceptance, bytes per day; NULL when it had no rate), `mutes.end_reason VARCHAR` (why nats-lens ended it; NULL when a person lifted it or it ran out), with comments, and a new `COMMENT ON TABLE mutes` saying that an accepted row is an acceptance. An accepted row has an entity; the API refuses an acceptance without one, in Go, rather than by a table check. `mutes` is in the archive: the implementer checks that a rebuild from Parquet written before 0031 still loads (`internal/store/archive.go`), and says how.
- **API.** `MuteFindingRequest` gains `bool accept` (next free number, 7) and `Mute` gains `bool accepted`, `optional double growth_per_day` and `string end_reason` (10, 11, 12). With `accept`: the entity is required, `expires_at` (the review date) is at most 365 days ahead (`MaxAcceptLength` beside `MaxMuteLength` in `internal/api/normalize.go`), and the server, not the client, records `growth_per_day`: for a stream the leader's bytes, for a server `js_storage`, by `RobustPerDay` over the 24 hours before now; other entities record none. Lifting stays `UnmuteFinding`; `ListMutes` lists acceptances with the mutes. `Finding.mute` carries the acceptance, so every reader of mutes sees it.
- **Growth review.** After each check run (`Runner.check` in `internal/runner/runner.go`), every acceptance in force with `growth_per_day` set is compared with the entity's growth over the 24 hours before the epoch, by the same rule. When both are known and they differ by more than half of the larger of the recorded rate's size and 1 MiB per day, nats-lens ends it: `unmuted_at` the epoch's time, `unmuted_by` "nats-lens", `end_reason` "growth changed from +2.1 MiB/day to +4.0 MiB/day". The 1 MiB floor keeps a bucket accepted at "+0" from returning on a few KiB; it is proposed, not yet confirmed. A growth that cannot be measured now (under 6 hours of data, the stream gone) ends nothing. At the review date the acceptance runs out as a mute does, and the finding counts again if it still fires.
- **Demo.** `fake` MuteFinding and UnmuteFinding keep `accept`, `growth_per_day` (from the stream's `perDay` in the scenario) and `end_reason`; the growth review is not simulated, but the seeded mutes gain one acceptance in force and one ended by nats-lens with its `end_reason` (T2 picks the streams). `TestDemoUsesRealChecks` covers the seeded acceptances.
- **Tests.** Normalize: accept without an entity, a review date of 366 days, 365 days accepted; growth recorded by the server and a client value ignored; the runner ends an acceptance on a change of 60%, keeps it on 40%, keeps it under the 1 MiB floor, keeps it when growth is unset; an accepted finding reads as muted in `Findings`, the tally, the timeline and `/metrics`; the migration applies on a store with mutes, and an archive rebuild from old Parquet loads. A conformance row: a finding under an acceptance, in both providers.
- **Docs.** `docs/design.md` (acceptance as a kind of mute, the growth review and its floor), `triage-and-mutes.md` and `checks.md` stay planned for the parts not built; T4 moves what shipped. `checks/stream-005.sql`'s remediation adds "or accept it as by design on the Capacity page" to the inventory and mute advice.

### Where T1 departed from this plan

Each point was decided by the session that built T1: proposed, not yet confirmed.

- **Migration column order.** `accepted BOOLEAN DEFAULT false` is added last in `0031_accepted_findings.sql`: DuckDB refuses to commit a transaction in which another `ALTER` follows `ADD COLUMN ... DEFAULT` on a table that holds rows (seen on a store with mutes; `TestMigrationsFrom30OnMutes`). Rows from before the migration and rows loaded from older Parquet files get `false` from the default, not NULL; readers still read NULL as false.
- **System entity refused.** An acceptance needs an entity with a key, so the system entity is refused as invalid. A plain mute of the system entity fails today as an internal error (its NULL key breaks the table's check that type and key are both set or both NULL); that is older than T1 and is left for its own fix.
- **No client growth to ignore.** `MuteFindingRequest` has no growth field, so a client cannot send one; the tests show the recorded value is the rate of the stored data instead.
- **Growth window.** The server records and the review compares the growth of epochs started in (t − 24 h, t], buckets of 1/200 of the window and at least one collect interval, as `streamGrowth` does for the Streams page. The review runs only after a check run that succeeded, and skips an acceptance created after the epoch started, so it never ends one at a time before its creation.
- **`stream-005` remediation.** It says "or accept it as by design with a review date" without "on the Capacity page", as the page arrives with T3; T3 adds the page's name.
- **Demo.** The fake records the growth of an accepted stream by the store's rule over its scenario series (`scen.growth`), which is the stream's `perDay` up to the series' shape. Seeded: in `example-prod` a new `stream-005` finding on `KV_customer-config`, accepted in force by `ops@example.com` with a review date six months later; in `example-global` an acceptance of `stream-005` on `KV_feature-flags` ended by nats-lens ("growth changed from +1.6 MiB/day to +2.0 KiB/day"). The fake timeline has no row for the new finding: it does not apply mutes to its rows at all, so T2 adds the row if the page needs it. `fake_test.go`'s counts of the prod scenario (muted 2, 7 findings with muted, 3 mutes) changed with it.

## T2 API, store and demo

`proto/natslens/v1/capacity.proto`, RPC `GetCapacity(GetCapacityRequest) returns (GetCapacityResponse)` in `service.proto`.

- Request: `system`, `range`.
- Response: `epoch`, `epoch_at`, `repeated ServerStore servers`, `int32 servers_without_jetstream`, `repeated CapacityStream streams`, `int32 streams_total`, `google.protobuf.Timestamp growth_from` (the first point the growth reads, so the UI can say a history is short).
- `ServerStore`: `server_name`, `cluster`, `bool reported`, `optional int64 used`, `optional int64 limit`, `optional double per_day`, `optional double days_to_full` (set only when `per_day` is above 0 and the limit is known), `string finding_id`, `string finding_code`, `Severity finding_severity`, `optional double finding_days`.
- `CapacityStream`: `account`, `account_name`, `stream`, `kind` (enum: stream, KV, object store), `retention`, `storage`, `int32 replicas`, `mirror`, `int32 sources`, `optional int64 messages`, `optional int64 bytes` (one copy), `optional int64 keys`, `optional google.protobuf.Timestamp first_ts`, `optional int64 max_msgs`, `optional int64 max_bytes`, `optional google.protobuf.Duration max_age`, `optional int64 max_msgs_per_subject`, `discard`, `bool allow_msg_ttl`, `optional double bytes_per_day` (one copy), `optional double msgs_per_day`, `NextLimit next_limit`, `CapacityStatus status`, `string finding_id`, `optional double finding_days`, `Mute mute` (the mute or acceptance in force), `string declared_reason`.
- `NextLimit`: `limit` (enum: none, empty, age, messages, bytes, per subject), `state` (enum: steady, reached refusing, reaches, not reached, not forecast), `optional double days`, `bool discard_new`.
- `CapacityStatus`: the states of the Status table.
- Rules in `internal/api/capacity.go` (`CapacityFacts`, `FinishCapacity`: next limit, time to full, status, order, the 500-row cut), shared by the store (`internal/api/storeprov/capacity_page.go`) and the demo, as `FinishAccounts` is. The store reads the epoch's rows and the range's points in SQL and gives `RobustPerDay` the points (the bucket medians of `bucketMedians`), never one query per stream.
- Handler: the common range validation and `QueryTimeout` through `call()`. No epoch in the range: the empty response with `epoch` 0.
- **Demo.** `streamSpec` gains the limits, retention, storage, mirror and the age of the oldest message; one story shows every state. `example-prod` (three servers, as the board): `order-jobs` (7 days, 1M msgs, "1M msgs: not reached"), `catalog-changes` (30 days, steady), `order-callbacks` (work queue, 1 day, 1M msgs, empty), `order-archive` (nothing, `stream-005` awaiting a decision, since about 10 months), `audit-archive` (100M per subject and a `max_bytes` with discard new, so its `stream-006` finding is coherent: "full in 9 days"), `KV_customer-config` (keys, no TTL, `stream-005` accepted by `ops@example.com`, "customer records are kept for the account's life", review in 6 months), `KV_order-jobs-cache` (TTL 48 h, bounded), `OBJ_image-input` (TTL 48 h, 8 GiB). Server cards a few percent full, full in more than 5 years. `example-global`: `nats-ap-2`'s card with its `server-014` finding; `order-archive` (52 GiB, +1.1 GiB/day) muted until a date; an acceptance on `KV_feature-flags` ended by nats-lens with its `end_reason`; `KV_session-cache` shrinking ("not growing"). `example-staging`: no growth (under 6 hours), so "not enough history" on every card and an en dash in /day; `catalog-changes` awaiting a decision. Its "Unbounded, no finding" state is in the store fixture only, unless the implementer finds a place for it.
- Conformance rows in `apitest` (`conformance_capacity.go`): a card with time to full, one without enough history, one not reported; each kind line (limits, work queue, mirror, KV, object store, memory); each next-limit state; each status; a cut list; and two that hold in every system: a stream's `bytes_per_day` equals `ListStreams`' growth for the same range, and the rows are the streams `ListStreams` lists for the epoch. The store fixture and the demo pass alike.
- Docs: `docs/design.md` (the RPC, the growth rule and why it is not the checks', the next-limit thresholds), `demo-mode.md`.

## T3 UI

- Route `/capacity`, `web/src/pages/Capacity.tsx`; the `NAV` entry in `web/src/nav.ts` loses `planned`, and `App.test.tsx`'s list of planned pages drops Capacity. Header "Capacity", subtitle "What each stream keeps, what bounds it, and when the disks fill.", and a line naming the collection, as Topology and Accounts do. Without a collection: "No collection in this range." and nothing else.
- Server cards as the board: name, "JetStream file store", "244 MiB of 50 GiB · 0.5%", a bar (warning colour from `server-007`'s warning ratio, critical over its critical ratio, taken from the finding when one fires), and the growth line ("+17 MiB/day · full in more than 5 years at this rate"). Growth and time-to-full words from the "What backs each part" table; the finding link under it.
- The streams table with the board's columns: Stream (name linking the stream page, kind line under it), Bounded by, Data held (bar and label), Size × R, /day, Next limit, Status. Numbers in `fmtBytes` and `fmtGrowth` (`web/src/lib/format.ts` already writes KiB, MiB and GiB, as the board does); dates in `isoDate`.
- "Accept as by design…" opens `AcceptDialog` (beside `MuteDialog`, sharing `lib/muteForm.ts` and `api/identity.ts`): the finding it accepts named in the dialog ("Accepts stream-005 Stream without any limit for order-archive"), a reason (required, the mute dialog's refusal text adapted), a review date (in 3 months, the default; 6 months; 1 year; or a date), and the identity line of the mute dialog. After saving, the row shows BY DESIGN without a reload.
- The note under the table, as the board, made true: "Data held spans from the oldest retained message to now on a one-year scale. Accept as by design records a reason and who accepted it; the finding then leaves the Overview until its review date, and comes back early if its growth changes by more than 50%."
- **An accepted finding reads as accepted wherever it shows.** The Findings page row reads "Accepted by <who>: <reason>. Review on <date>." in place of "Muted by"; the finding page's mute section says "Accepted as by design" and its lift button reads "End acceptance…"; the mutes list shows Accepted, and for one nats-lens ended, "Ended by nats-lens: growth changed from … to …". The Overview needs no change (an acceptance counts as muted there; see the decisions).
- Phone width: cards stack; the table scrolls inside its card; the status cell keeps the button reachable.
- Styles in `web/src/styles/capacity.css`, every rule on a `cap-` class, checked by `web/src/components/CapacityCss.test.ts` as `AccountsCss.test.ts` does; imported from `index.css`.
- Data: `requests.getCapacity`, `web/src/api/capacity.ts`, `useCapacity()` keyed by system and range, refetched with the page's live interval, keeping the previous answer only for the same system (as `useAccountsAndAuth`). View helpers in `web/src/lib/capacityView.ts` with tests (time-to-full words, next-limit words, bound text, data-held label and bar width).
- Docs: `navigation.md`, the UI section of `design.md`.

## T4 Feature docs and audit

`capacity.md` to `Status: shipped` (or `partial`), with its rules, scenarios and Not supported list, and its "Time to full uses the checks' rule" line replaced by the rule built; "No account disk limits yet" rewritten (operator-mode limits are collected now, the page does not show them). `triage-and-mutes.md` and `checks.md`: accepted findings as shipped for one entity, the rest still planned. `stream-checks.md` (the `stream-005` remediation). `docs/features/README.md` table and coverage map. `demo-mode.md`. The audit, `python3 /Users/denis/dev/info.ddenis/dotfiles/agent-rules/packs/ddenis-core/skills/feature-docs/scripts/audit_feature_docs.py docs/features/ --map-refs require`; leftovers into `docs/plans/next.md`.

## Leftovers

To move to `docs/plans/next.md` when the page ships, most valuable first: acceptance of the same finding on related entities and of a check everywhere; the "Review accepted findings" list; accepting from the finding page and the Findings page; an Overview tally that counts accepted apart from muted; account JetStream disk limits as a bound and an account card (operator mode); object counts of an object store; a forecast per subject; memory stores on the cards.
