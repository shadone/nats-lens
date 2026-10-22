# Micro services build plan

Working plan for the Micro services page (`docs/features/micro-services.md`, board `Micro.dc.html` of the design canvas). Everything here was proposed by the planning session of 2026-10-22 and is not yet confirmed by the owner. When a task lands, its lasting content moves into `docs/features/micro-services.md`, `docs/features/event-capture.md` (the account connections), `docs/features/monitoring-snapshots.md` and `docs/design.md`; this file then only tracks status.

The board's sample services (`orders`, `pricing`, `inventory`) fit the example shop; use the shop of `inventory.example.yaml` and the demo's systems, and no name from any other board.

## Status

| Task | Status |
|---|---|
| T1 Collect micro service replies | not started |
| T2 API, store and demo | not started |
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

- **Requests.** For each account connection: `$SRV.INFO` every `subscriptions_interval` (default 5 minutes; also on the first epoch and when the epoch's `$SRV.STATS` names an instance not yet described), and `$SRV.STATS` every epoch, each a request with a reply inbox gathered for the collect window, stopping early when no reply came for a short quiet period (as `gather` does for servers; the implementer decides the quiet period and says why). The requests run in the same window as the system account's detail endpoints so the epoch takes no longer.
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
