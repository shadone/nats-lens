# Micro services build plan

Working plan for the Micro services page (`docs/features/micro-services.md`, board `Micro.dc.html` of the design canvas). Everything here was proposed by the planning session of 2026-10-22 and is not yet confirmed by the owner. When a task lands, its lasting content moves into `docs/features/micro-services.md`, `docs/features/event-capture.md` (the account connections), `docs/features/monitoring-snapshots.md` and `docs/design.md`; this file then only tracks status.

The board's sample services (`orders`, `pricing`, `inventory`) fit the example shop; use the shop of `inventory.example.yaml` and the demo's systems, and no name from any other board.

## Status

| Task | Status |
|---|---|
| T1 Collect micro service replies | built on `feat/micro-collect`; fix round 1 done, awaiting its review |
| T2 API, store and demo | built on `feat/micro-api` (with `feat/micro-collect` merged in, up to T1's fix round 1); fix round 1 in progress |
| T3 UI | not started |
| T4 Feature docs and audit | not started |

## Rules for every step

- Read-only towards the services: nats-lens sends only the framework's `$SRV.INFO` and `$SRV.STATS` requests, never a request to an endpoint, and never `$SRV.PING` more often than needed.
- `$SRV` requests are answered only inside the service's own account. The collector's system account connection cannot reach them. nats-lens already opens one connection per account listed under `accounts` (event capture, `cmd/natslens/events.go`); the `$SRV` requests use those connections. An account not listed there is not asked.
- Two levels of knowledge, and the page says which applies:
  - **Seen in subscriptions** (every account, no new permission): the micro framework subscribes each instance to `$SRV.{PING,INFO,STATS}`, `$SRV.{PING,INFO,STATS}.<name>` and `$SRV.{PING,INFO,STATS}.<name>.<id>`. The subscription collection of the Subjects page (SUBSZ) therefore shows each service's name, its instance IDs, and the connection (name, server) of each instance. Endpoints are not inferred from other subscriptions.
  - **Asked** (accounts listed under `accounts`): `$SRV.INFO` and `$SRV.STATS` give versions, descriptions, endpoints with subject and queue group, request and error counts, processing time and the last error.
- A value a service did not report is unset, never 0.
- The page shows the newest collection at or before the end of the page range (the epoch rule of `GetPlacement`).

## What backs each part

| Part | Source | Rule |
|---|---|---|
| Services and instances | `$SRV.INFO` replies (asked accounts), else the subscription collection | One service per (account, name). An instance is one service ID. Asked: the instances that answered the epoch's `$SRV.STATS`. Seen in subscriptions: the IDs of `$SRV.*.<name>.<id>` subscriptions in the newest subscription collection. |
| Version, description | `$SRV.INFO` | The instances' versions; more than one: "mixed versions: 3 on 2.0.0, 1 on 1.9.3". |
| Instance missing | stats of the epoch, instances of the last hour | An instance ID that answered (or was seen in subscriptions) within the hour before the epoch and is not in this epoch's answers, while its service has other instances or its connection is still listed: "instance missing: `<id>` since 08:12". An instance whose connection closed (gone from CONNZ and subscriptions) after a scale-down is not missing once it has been gone for the hour. |
| Requests per second, errors | `$SRV.STATS` counters per instance and endpoint | Counters are cumulative from the instance's start (`started`); the rate is the increase over the 15 minutes before the range end, per instance run, summed over instances, as `deliveredRates` reads a delivered sequence. Errors as a share of requests over the same span. |
| Average time | `$SRV.STATS` `processing_time` and `num_requests` | The increase of total processing time divided by the increase of requests over the 15 minutes; unset without requests. |
| Slow endpoint | the average time, its 7-day level | The 7-day level is the median of the hourly averages of the 7 days before the 15 minutes; the endpoint is slow when its average time is at least 3 times that level and at least 5 ms above it; "avg time 10× its 7-day level since 08:12", with the first epoch of the current slow run. Under 24 hours of history: no level, no flag. |
| Last error | `$SRV.STATS` `last_error` | The newest non-empty last error of the endpoint's instances, with the instance. |
| Note | the facts above | First that applies: instance missing, mixed versions, slow endpoint, errors above 1% ("errors 2.4% on orders.create"), not asked ("add the account under `accounts` for versions, endpoints and timings"), asked but no answer ("no answer to $SRV.STATS: check the account user may publish to $SRV.> and subscribe to _INBOX.>"), else "healthy". |
| Endpoints | `$SRV.INFO` and `$SRV.STATS` | Per service: name, subject, queue group, requests per second, average time, error share, last error; a slow endpoint's row in the warning colours. |

Order of the cards: services with a warning note first, then by requests per second, descending, then by name; the system account's services last. The endpoint table shows the chosen service, by default the first card; a card is chosen by click and kept in the address (`?service=<account>/<name>`).

## T1 Collect micro service replies

- **Requests.** For each account connection: `$SRV.INFO` every `subscriptions_interval` (default 5 minutes; also on the first epoch and when the epoch's `$SRV.STATS` names an instance not yet described), and `$SRV.STATS` every epoch, each a request with a reply inbox gathered for the collect window, stopping early when no reply came for a short quiet period (as `gather` does for servers; the implementer decides the quiet period and says why). The requests run in the same window as the system account's detail endpoints, within the collect window (an epoch whose other requests end sooner now waits the quiet period; see the departures).
- **Where it lives.** A component that owns no connection: it receives the account connections the event capture opened (or the runner opens them when event capture is off; the implementer reads how `cmd/natslens/events.go` and the runner start them, and shares one connection per account). The snapshot carries the replies; the runner writes them with the epoch.
- **Failures.** No responders, a permissions violation (the server's `-ERR` arrives as an async error on the connection), a timeout with no reply: recorded per (epoch, account, request) so the page can say "asked but no answer", not "no services".
- **Storage.** Migration `0033_micro_services.sql`:
  - `micro_instances` (`epoch`, `account`, `service`, `instance_id`, `version`, `description`, `started`, `metadata` JSON): one row per instance per INFO collection;
  - `micro_endpoints` (`epoch`, `account`, `service`, `instance_id`, `endpoint`, `subject`, `queue_group`, `num_requests`, `num_errors`, `processing_time_ns`, `average_processing_time_ns`, `last_error`): one row per instance and endpoint per STATS collection; the subject and queue group from the newest INFO;
  - `micro_requests` (`epoch`, `account`, `request` (INFO or STATS), `state` (ok, no responders, timeout, error), `replies`, `error`): one row per account and request asked.
  - Comments on every column; epoch tables for retention and the hourly archive. A rebuild from hours archived before 0033 must still work.
- **Config.** No new key: an account listed under `accounts` is asked. Its user needs publish on `$SRV.>` and subscribe on `_INBOX.>`; `running-nats-lens.md` and the example configuration say so. A per-account `micro: false` turns asking off for one account.
- **Size.** A service with many instances and endpoints: one STATS reply per instance; measure 100 instances × 20 endpoints.
- **Tests.** An in-process server with a service built on `github.com/nats-io/nats.go/micro` in an account: INFO and STATS stored, counters, two instances, an instance stopped (no reply), a user without `$SRV` permission (state recorded, the epoch not failed), the interval of INFO, the migration on a store with data, archive and rebuild.
- **Docs.** `docs/design.md`, `monitoring-snapshots.md`, `event-capture.md` (the account connections now also ask `$SRV`), `running-nats-lens.md`, the README table list.

### Where T1 departed from this plan

Each point was decided in the session that built T1: proposed, not yet confirmed.

- **A fourth table, `micro_stats`.** `$SRV.INFO` carries no `started`; `$SRV.STATS` does, with the version. `micro_stats` (`epoch`, `account`, `service`, `instance_id`, `version`, `started`) has one row per instance that answered STATS, so an instance without endpoints still counts as answering, and T2 reads an instance run from it. `micro_instances` has no `started`, and keeps the INFO endpoint list (name, subject, queue group, metadata) as JSON in `endpoints`.
- **Subject and queue group from STATS.** The framework's STATS reply names each endpoint's subject and queue group, so `micro_endpoints` takes them from there and from the newest INFO only when STATS left them empty. `micro_endpoints.data` keeps the endpoint's custom stats (leftover: not shown).
- **`micro_requests.label`.** Each row has the account's label from the configuration besides `account`, with a unique index on (`epoch`, `label`, `request`): two entries for one account would otherwise collide.
- **The account's name.** `account` is the account as the servers name it (the public key in operator mode), from `$SYS.REQ.USER.INFO` asked once per connection with the first `$SRV` requests, so that it joins `subscriptions.account`; the label when the user may not ask or no answer came. With event capture on, the capture asks too: two small lookups per connection.
- **Grants.** The README asks for publish on `$SRV.INFO` and `$SRV.STATS`, the only subjects sent, rather than `$SRV.>`; `$SRV.>` works too. The test cluster's scoped account user has them, and `TestMicroScopedUser` fails without either.
- **The quiet period and the window.** A request ends 250 ms after its last reply: instances answer from their own subscription as the request arrives, a few milliseconds apart on one server and tens of milliseconds across routes. STATS, the INFO due by time, the account lookup and an INFO for a new instance share one window per account from the start of the requests, so they end within the collect window; an INFO for a new instance that finds no time left is asked again next epoch. They do add to an epoch whose other requests end sooner: in a one-server test an epoch of 2 to 3 ms took about 252 ms, and a STATS with neither a reply nor a no responders status waits the whole window.
- **`subscriptions_interval` 0.** SUBSZ is off then; INFO goes out only on the first epoch and for new instances.
- **How a refusal is read.** nats.go reports a permissions violation only as the connection's last error. The collector flushes after subscribing the inbox and after each publish, so a violation has arrived by then, and takes the last error when it is new and names the request's subject; fix round 1 adds a watch on the connection's error handler (below). A refused inbox sends nothing. A refusal is state `error` with the server's text, as the plan's four states have no fifth; T2 can tell it by the text.
- **No responders per request.** Each request has its own reply subject under one inbox, read through a callback subscription: a synchronous subscription's `NextMsg` turns the no responders status into an error without saying which request it answered.
- **Undecodable and repeated replies.** A reply of another type or without service name and instance ID is counted; the request is `error` when nothing else decoded, else `ok` with "n of m replies did not decode" in `error`. A second reply of one instance is dropped.
- **`natslens once`.** It opens a connection per asking account for its one epoch; an account that does not connect is recorded as "account connection not connected".
- **Tables without `server_id`.** The four tables are a list of their own (`accountEpochTables`), swept and archived with the epoch tables; the store test that every epoch table has `server_id` does not apply to them.
- **Sizes.** 100 instances × 20 endpoints on one server: 100 STATS replies, about 335 KB, and 100 INFO replies, about 175 KB, gathered within the 250 ms after the last reply, decoded in about 2 ms (`TestMicroManyInstances` in `internal/collect`); writing them adds about 40 ms to an epoch in memory (2,000 endpoint rows in JSON batches of 5,000, 100 rows each in `micro_instances` and `micro_stats`; `TestMicroManyInstances` in `internal/store`). At a 30-second interval that is 5.8 million endpoint rows a day.
- **Docs beyond the list.** `natslens.example.yaml`, the README's grants table and permissions block, the coverage lines in `docs/features/README.md`, and `micro-services.md` (what is built, one STATS per account, the grants).
- **Fix round 1 (review of T1).**
  - Refusals of the collector's own requests are taken by a watch in front of each account connection's error handler (`$SRV.INFO`, `$SRV.STATS`, the collector's reply inboxes, which end in `.srv`, and its account lookup for 5 seconds after it is sent). Before, the capture's handler recorded a `collector.subscription_error` event for every refused request, every epoch, and without capture the default handler logged a warning every epoch. The exchange reads refusals from the watch as well as from the last error, which a later error can overwrite. A refusal is logged once per account, again when a request's refusal changes, and once at Info when it ends.
  - Without event capture, an account connection whose first dial fails is logged and dialled again with the redial backoff (`eventConn.keep` now dials a connection that has none), its requests recorded as not connected; before, it failed the system's setup and no epoch was taken. With event capture, a failing account connection still fails the setup, as before T1.
  - Two `accounts` entries whose users are in one account ask it once: in the epoch that finds out, the first entry's answers are kept and the later entry's dropped; afterwards the later entry does not ask while the first one's connection is up. Each such entry is logged once. An entry whose lookup is refused keeps its label as account and cannot be matched.
  - A request waits for every instance that answered the account's last answered STATS before its quiet period starts, within the window. An instance that stopped makes the next epoch wait the whole window once. An instance never seen that answers more than 250 ms after the others is still missed until it once answers in time.
  - The micro requests start with the epoch, alongside discovery: a first epoch, whose discovery waits its whole window, took a window plus the quiet period before.
  - `micro_endpoints.data` keeps at most 1 KB; more is stored as `{"truncated": true, "bytes": n}`.
  - A fifth table, `micro_endpoint_hours` (`hour`, `account`, `service`, `endpoint`, `requests`, `errors`, `processing_time_ns`, `epochs`, unique on the first four), for T2's 7-day level. Each epoch's write adds the increase of each STATS row over the instance's previous row within the hour before to the epoch's hour; a counter that fell or a new `started` counts whole, and an instance's first row counts its value only when it started after the previous epoch, so a service seen for the first time adds no history. Filled at write time rather than by the runner, so it is in the same transaction as its rows and an hour's rows are whole by the hour's final export; archived by hour, swept with the hours wholly before the window. At 7 days of 100 instances × 20 endpoints the median of hourly averages reads in 1 ms from it against 159 ms from `micro_endpoints` (2 ms with 50 services), adding one epoch's sums takes 18 ms, and `micro_endpoints` takes about 9 bytes a row on disk (`TestMicroSizeAtScale`, `MICRO_SIZE_DAYS=7`). Migration 0033 was changed in place, as it was applied nowhere outside this branch.
  - A refused account lookup is asked again after 10 minutes instead of never; the collector forgets labels no longer configured.

## T2 API, store and demo

`proto/natslens/v1/micro.proto`, RPC `GetMicroServices(GetMicroServicesRequest) returns (GetMicroServicesResponse)`.

- Request: `system`, `range`, `account` (the scope).
- Response: `epoch`, `epoch_at`, `repeated MicroService services`, `int32 services_total`, `repeated AccountState accounts` (per account with services: asked or seen only, the last request states), `optional subscriptions_at`.
- `MicroService`: `account`, `account_name`, `name`, `bool asked`, `repeated VersionCount versions`, `description`, `int32 instances`, `repeated MissingInstance missing`, `optional double requests_per_second`, `optional double error_share`, `repeated MicroEndpoint endpoints`, `repeated string connection_names` (at most 3, `more_names`), and the note's facts as fields (the UI words them).
- `MicroEndpoint`: `name`, `subject`, `queue_group`, `optional double requests_per_second`, `optional double average_ms`, `optional double error_share`, `last_error`, `last_error_instance`, `optional double level_ms` (the 7-day level), `optional google.protobuf.Timestamp slow_since`.
- At most 200 services and 100 endpoints per service, with totals.
- Rules in `internal/api/micro.go` shared by the store (`storeprov/micro_page.go`, SQL aggregates) and the demo, as `FinishCapacity` is.
- **Demo.** `example-global` with `APP` asked: `orders` (1.4.2, 3 instances, `get` on `orders.get.*` 10 times its 7-day level since a time in the story, `create` with a last error "409 duplicate order id"), `pricing` (4 instances, 3 on 2.0.0 and 1 on 1.9.3), `inventory` (2 instances, healthy, one instance missing for part of the story). Their instances are connections on the Connections page and their `$SRV` subscriptions on the Subjects page. `example-prod`: an account not listed under `accounts`, so its services are seen in subscriptions only. `example-staging`: none.
- Conformance rows: asked and seen-only services, mixed versions, missing instance, rates over an instance restart, average time, the slow rule and its start, last error, no answer, the store fixture and the demo alike.
- Docs: `docs/design.md`, `demo-mode.md`.

### Where T2 departed from this plan

Each point was decided in the session that built T2 and is proposed, not yet confirmed.

- **The missing rule.** "Its connection is still listed" would warn after an in-process restart (a new ID on the same connection) and say nothing a count cannot. An instance gone within the hour is missing when its `$SRV` subscriptions are still in a subscription collection taken after it went missing (asked accounts: registered, not answering), or when the service has fewer instances now than in the collection that last listed it. A rolling restart is therefore not missing; a scale-down is, for the hour, as the plan says. `MicroMissingInstance.still_subscribed` replaces a "connection listed" field.
- **The rate.** Per instance of the epoch, its newest run (one instance ID and start time, no counter falling) with two reports in the 15 minutes, increase over the run's time, summed. A gone instance is left out, so a restart is not counted twice; an instance with one report so far adds nothing yet.
- **The slow start and the level.** The slow run is the run of collections, each slow on its own average (the instances' increases ending in that collection, summed), that ends at the newest slow one; a slow 15-minute average implies one. "Under 24 hours of history" reads as fewer than 24 hours with an average in the 7 days.
- **The last error.** STATS gives `last_error` no time, so "newest" is not knowable; the endpoint shows the first by instance ID among the epoch's instances that report one.
- **Fields the plan did not list.** `MicroService.note` (`MicroNote`, the first note that applies, so both providers and the UI agree), `answered`, `instance_list` (ID, version, start, connection and server; at most 100), `endpoints_total`, `system_account`; `MicroEndpoint.slow` and `errors_high`; `MicroAccountState` (the plan's `AccountState`) with `services` and `system_account`, its requests as `MicroRequest` with a `MicroRequestState`. The accounts list holds every account asked, also one without a service, so the page can say "asked but no answer".
- **Endpoint order.** Slow endpoints first, then those with errors above 1% (highest first), then by name: the endpoint a note names is the first, within the cap of 100.
- **Service order.** A note warns for instance missing, mixed versions, slow endpoint, errors and no answer; those first, then the system account's last, then by requests per second (unset last), name and account.
- **The seen level in the store.** An instance last held on a server whose list in the newest collection was cut at the limit or did not come is not missing: its absence there says nothing. An instance on a connection CONNZ does not list still counts, without a connection name.
- **The demo.** The instances run on existing client connections of `eu` servers: `orders` on `order-service`, `pricing` on `checkout-service` (no client is named for pricing), `inventory` on `inventory-service`, so the Connections and Accounts pages keep their counts. A `pricing` instance restarts in place at 14:21 on the same connection; an `inventory` instance hangs from 10:05 to 10:47, still subscribed. `SUPPLIERS` is asked and refused (a permissions violation), for "asked but no answer". `example-prod` runs `catalog` (on `catalog-api`) and `ledger` (on `ledger-service`), seen only. Every instance's nine `$SRV` subscriptions and its endpoints' subjects in queue group `q` are rows of the Subjects page.
- **Conformance.** The Micro services rows are part of the shared table; the store runs them over a system of its own (`micro`, 26 hours of 5-minute epochs), as the demo has none with 7 days of history to spare.
- **The store's asked level.** Every rule runs in SQL and Go reads one row per instance and per endpoint. The level groups each instance's reports by hour first (an hour's increase is its last report less the last report before it), so 7 days are grouped, not walked; the per-collection averages for the slow run's start are read only for endpoints that are slow. `TestMicroServicesAskedFollowTheRules` holds the SQL to api's Go rules over the same reports at seven page ends, with a restart under a new ID, a reset under the same ID, a hang and an earlier slow spell. The level still reads raw `micro_endpoints`; T1's review asks for an hourly rollup table (`micro_endpoint_hours` or similar), and `microLevels` should read it once it lands.
- **Labels for accounts.** When the account lookup is refused, T1 records the configured label, which in operator mode is not the key the subscription collection names. `FinishMicro` reads an asked account none of whose services is seen under it, and whose answered instances are all seen under one other account, as that account, so a service is listed once.
- **The merge.** `feat/micro-collect` was merged into `feat/micro-api` before T1's fix round was announced; that round's changes (the rollup table, one ask per account for two labels) still need merging, and the level query moving to the rollup.

## T3 UI

- `/micro-services` under Messaging (remove `planned` in `nav.ts`), with the account scope.
- The board: title and line, the service cards (three a row at desktop width, one at phone width) with name, version, description, instances, req/s, errors and the note in the warning colours when it warns, the endpoint table of the chosen service with the board's columns and the footnote. A seen-only service's card says what is known and how to see more.
- Links: an instance's connection to the Connections page, the endpoint subject to the Subjects page's tester (`/subjects?subject=` if that page reads it; else plain text).
- Empty and partial states: no epoch, no service, an account asked without answer, no subscription collection.
- Phone width, light and dark, accessibility as on the Subjects page.

## T4 Feature docs and audit

`docs/features/micro-services.md` to shipped, the README table and coverage map, `navigation.md`, `event-capture.md`, `monitoring-snapshots.md`, `running-nats-lens.md`, `demo-mode.md`, `subjects.md` (its "service endpoints" leftover), `docs/plans/next.md`. Then the audit.

## Leftovers

Recorded as they come up; moved to `docs/plans/next.md` item 5 when the work merges.

- Endpoint metadata and the service's custom stats (`data` in STATS replies) are stored but not shown.
