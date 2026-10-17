# Phase 1 build plan

Working plan for phase 1: several NATS systems in one nats-lens, the new UI shell, the overview cockpit, events and finding detail, servers for 20+ servers, KV and object store pages. Each task is built by a fresh implementer and checked by a fresh reviewer. Decisions marked "proposed" are the planning session's, not yet confirmed by the owner.

When a task lands, its lasting content moves into `docs/features/`, `docs/design.md` or the README; this file then only tracks status.

## Waves

| Wave | Tasks (parallel, separate worktrees) |
|---|---|
| 0 | T1 config, T2 API contract and routing |
| 1 | T3 runtime, T4 Fleet backend and demo, T5 UI shell |
| 2 | T6 series, T7 servers backend, T8 events backend (all after T4's fake refactor) |
| 3 | T9 overview backend, T10 KV and object stores (merge T9 first: both touch `internal/api/fake/jetstream.go`), T11 Fleet page |
| 4 | T12 overview UI, T13 events and finding UI, T14 servers UI |

Two rules keep the waves apart:
- Proto: T2 adds `system` to every request and declares every new RPC with minimal messages. Later tasks add fields only to their own `.proto` file; `service.proto`, `internal/api/provider.go` and `internal/api/handler.go` change only in T2.
- Web: T5 adds a nav registry (`web/src/nav.ts`) and the convention that each feature keeps its hooks in `web/src/api/<feature>.ts`. UI tasks then share only one-line additions in `App.tsx` and `nav.ts`.

Every task updates the README sections and `docs/features/` docs it changes, and writes its own decisions as "proposed, not yet confirmed".

## Status

| Task | Status |
|---|---|
| T1 | not started |
| T2 | not started |
| T3 | not started |
| T4 | not started |
| T5 | not started |
| T6 | not started |
| T7 | not started |
| T8 | not started |
| T9 | not started |
| T10 | not started |
| T11 | not started |
| T12 | not started |
| T13 | not started |
| T14 | not started |

## Tasks

### T1. Configuration: a list of systems; the single-system config keeps working (S/M)

- **Result:** `natslens.yaml` accepts `systems:`; an existing config starts exactly as before.
- **Files:** `internal/config/config.go`, `flags.go`, `kubernetes.go`, `events.go`, new `systems.go`; `natslens.example.yaml`; README Configuration.
- **Shape:**

  ```yaml
  data_dir: /var/lib/natslens        # the root once systems: is used
  archive_dir: ""                    # optional root; else <system data dir>/archive
  systems:
    - id: prod                       # ^[a-z0-9][a-z0-9-]{0,62}$; used in URLs, the API, metric labels, directory names
      name: Production               # display name; default: id
      environment: production        # free text shown on Fleet
      nats: {url, name, creds | user/password, tls}
      accounts: [...]                # as today
      inventory_file: ...
      kubernetes: {enabled, kubeconfig, context, namespaces, nats_selector, server_name, ...}
      retention: 14d
      archive_retention: 90d
      data_dir: ""                   # default <data_dir>/<id>
      archive_dir: ""                # default <archive_dir>/<id> or <system data_dir>/archive
      duckdb_memory_limit: ""        # default: the global budget / number of systems
  ```

  Global: interval, timeout, listen, IAP, events tuning, checks (thresholds, disabled, remediation file), connz_limit, shutdown_grace.
- One code path: `func (c Config) ResolvedSystems() []System`. A config without `systems:` resolves to one system: id from a new optional top-level `system_id` (default `default`), data dir exactly `data_dir`, archive exactly `ResolvedArchiveDir()`, so no file moves. Passwords keep `NATSLENS_NATS_PASSWORD` / `NATSLENS_ACCOUNT_<NAME>_PASSWORD`; in a list config `NATSLENS_SYSTEM_<ID>_NATS_PASSWORD` / `NATSLENS_SYSTEM_<ID>_ACCOUNT_<NAME>_PASSWORD`. `ToolingNames()` moves to `System`.
- **Tests:** the legacy YAML gives one system `default` with `data_dir` unchanged; a list YAML gives per-system defaults; the legacy password variable is still read. Refused: top-level `nats:` together with `systems:`; the `-nats-url` flag together with `systems:`; a duplicate id; id `Prod.1`; two systems resolving to the same data or archive dir; `archive_retention` below `retention`; `kubernetes.enabled` with events off.
- **Risks:** `LoadFile` uses `KnownFields(true)`; the existing `config_test.go` keeps passing; `applyFlags` must not reach into `systems`.

### T2. API contract and routing by system (L)

- **Result:** every RPC takes `system`; one handler serves N providers; `/metrics` carries a `system` label; the new RPCs exist with minimal messages; the demo works as one system `example-prod`.
- **Files:** `proto/natslens/v1/*.proto`, new `systems.proto`, `buckets.proto`; `internal/gen`, `web/src/gen` (`make proto`); `internal/api/handler.go`, `provider.go`, `server.go`, new `systems.go`; `internal/api/storeprov/metrics.go`, `provider.go`, new `accounts.go`; `internal/api/fake/fake.go`, new `fake/systems.go`; `internal/api/apitest`; `cmd/natslens/serve.go` (wraps the one provider in a registry); `web/src/api/requests.ts` (pass `system` through only).
- **Proto, new field numbers only:**
  - `string system` on GetLensInfoRequest 1, GetOverviewRequest 3, ListFindingsRequest 9, GetFindingRequest 2, MuteFindingRequest 6, UnmuteFindingRequest 3, ListMutesRequest 4, GetStatusTimelineRequest 5, ListEventsRequest 8, GetEventRequest 2, ListStreamsRequest 4, GetStreamRequest 4, ListConsumersRequest 5, GetConsumerRequest 5, ListServersRequest 2, GetServerRequest 3, ListConnectionsRequest 8, GetTimeSeriesRequest 5. `ListMetrics` stays global. Check each number against the file; if one is taken, use the next free and say so.
  - `GetLensInfoResponse`: `string system = 9`, `string system_name = 10`.
  - `systems.proto`: `SystemRef {id=1, name=2, environment=3}`; `ListSystemsRequest {range=1, strip_buckets=2}`; `SystemSummary {SystemRef system=1}` (T4 adds the rest); `ListSystemsResponse {systems=1, range=2, default_system=3}`; `Account {account=1, account_name=2, is_system=3, optional int32 connections=4, optional int32 streams=5, optional int32 consumers=6}`; `ListAccountsRequest {system=1, range=2}`, `ListAccountsResponse {accounts=1}`.
  - `buckets.proto`: `ListKeyValueStoresRequest {system=1, range=2, account=3}` with `KeyValueStore {EntityRef entity=1, string bucket=2}`; the same pair for object stores (T10 adds the rest).
  - `common.proto`: `enum PlacementState {UNSPECIFIED, LEADER, IN_SYNC, BEHIND, DISAGREES, MISSING, OFFLINE}`; `ReplicaPlacement {server=1, PlacementState state=2}`.
  - `service.proto`: `ListSystems`, `ListAccounts`, `ListKeyValueStores`, `ListObjectStores`, all `NO_SIDE_EFFECTS`.
- **Routing:** `api.Systems` is an ordered list of `{Ref, Provider, Status func() CollectionStatus}`. `call()` resolves the provider through `interface{ GetSystem() string }`: `""` is the default system (the first configured); an unknown id is `invalid_argument` listing the known ids. `withAccountNames` uses the resolved provider. Providers never check `system` themselves. In T2 `ListSystems` returns refs only; `ListAccounts` is complete (the `accounts` table plus the newest epoch's `account_stats`: conns, js_streams, js_consumers); the bucket RPCs return empty lists.
- **`/metrics`:** `metricsWriter` collects samples per family and writes each header once, with `system="<id>"` on every sample. Note the series identity change in README Metrics.
- **Tests:** `system=""` reaches the default provider; two fake systems with different cluster names return their own `GetOverview.cluster`; `/metrics` with two providers writes each `# HELP` once and labels every sample; `ListAccounts` on the storeprov fixture. Failing case: `system="nope"` is `invalid_argument` and no provider is called.
- **Risks:** touches every proto file, so it merges before wave 1.

### T3. Runtime: one collector, capture, watcher, engine and archive per system (L)

- **Result:** `natslens run` watches every configured system; a system that is down never stops the others; shutdown stays bounded.
- **Files:** `cmd/natslens/main.go`, new `system.go` (`type systemRuntime` with start and stop), `store.go`, `events.go`, `kubernetes.go`, `collectorconn.go`, `ready.go`, `checks.go`; `runSQL` and `runChecksCmd` get `-config` and `-system`; `internal/store/memory.go` (threads per database); `docs/design.md` (a "Multiple systems" section; remove "multi-system support in one process" from out of scope; decisions rows); README Running and Database.
- **Behaviour:**
  1. Stores open in parallel, each with its own rebuild. On first open each system data dir gets `system.json` (`{"id": ...}`); a marker with another id refuses the start; a legacy dir without one is adopted. Warn when `<data_dir>/natslens.duckdb` exists and no system uses it.
  2. Per system: its own `collect.Tracker`, `collectorConn`, `runner.Runner`, `checks.Engine` (sharing the global `checks.Set`), `events.Capture`, k8s watcher (own kubeconfig and context) and `archive.Archiver`; logs carry `system`.
  3. DuckDB `memory_limit` = global budget / N unless set per system; `threads` = `max(2, GOMAXPROCS/N)` per store (proposed).
  4. `/readyz` answers once every store is open and the API serves; NATS being unreachable never affects readiness; a store that fails to open fails the start (proposed). Each runtime exposes `CollectionStatus {state: starting|collecting|not_connected|connection_closed, last_error, since, last_epoch_at}`, fed by `collectorConn`, wired into `api.Systems.Status`.
  5. One shutdown deadline for all systems; they stop in parallel; each writes its own `state.json`; stores close after the API's `wait()`.
  6. `once` runs one epoch per system or only `-system ID`; `sql` and `checks run` take `-system` with `-config`.
- **Tests:** two in-process clusters (`internal/testcluster`), two systems, 2 epochs: each database holds only its own `servers.server_name`; `ListServers{system:a}` returns A's servers. A legacy config opens an existing `data_dir/natslens.duckdb` in place. The existing shutdown and WAL recovery tests pass unchanged. Failing cases: system B unreachable, A still stores epochs, `/readyz` 200, B is `not_connected`; a marker mismatch or two systems sharing an archive dir refuses the start.
- **Note from T1:** each listed system has its own tooling names (`System.ToolingNames()`, from `natslens-<id>` by default), while `checks.Set` is shared: give it the union of every system's tooling names, or keep one list per engine. `Config.Warnings()` is logged by `config.Parse` through the default logger, before the JSON logger exists; T3 may log it with its own logger instead. `Config.ToolingNames()` can go once nothing calls it.
- **Risks:** the highest-risk task: the shutdown order in `main.go` becomes per system without losing a bound. N DuckDB instances compete for CPU. Two systems in one Kubernetes cluster each watch nodes; document it.

### T4. Fleet backend and a three-system demo (M/L)

- **Result:** `ListSystems` returns a full summary per system; `natslens demo` serves `example-prod` (today's scenario), `example-global` (24 servers in clusters `eu`, `us`, `ap` with gateways, one server down, one store at 71%, one older version) and `example-staging` (not collecting, credentials rejected 3 h ago).
- **Files:** `systems.proto`; new `internal/api/storeprov/fleet.go`; `internal/api/systems.go` (providers in parallel, 5 s per system); `internal/api/fake`: a `scenario` struct replaces the package constants, new `fake/global.go`, `fake/staging.go`; `cmd/natslens/serve.go` (`runDemo`).
- **Proto** (`SystemSummary` after 1): `CollectionState collection=2`, `string collection_error=3`, `Timestamp collection_error_since=4`, `Freshness freshness=5`, `Status status=6`, `Finding worst_finding=7`, `FindingTally tally=8`, `optional int32 servers=9`, `optional int32 servers_answering=10`, `optional int32 clusters=11`, `optional int32 streams=12`, `optional int64 connections=13`, `repeated Status strip=14`, `Timestamp strip_start=15`, `Duration strip_bucket=16`, `string error=17`. New `enum CollectionState {UNSPECIFIED, STARTING, COLLECTING, NOT_CONNECTED, CONNECTION_CLOSED, DEMO}`.
- **Data (collected):** newest `epochs` row (expected/observed servers); distinct `servers.cluster`; stream count from `stream_stats`; `server_stats.connections`; tally and worst finding as `GetOverview`; the strip = worst cell per bucket of the system rows of `GetStatusTimeline` (48 buckets); the error and since from the runtime status, else the newest `collector.connection_closed` / `collector.not_connected` event.
- **Tests:** the demo returns 3 systems in config order with stable counts (prod 3 servers; global 24 in 3 clusters; staging `CONNECTION_CLOSED` with an error). The storeprov strip shows a fixture finding in its bucket. Failing case: a provider that blocks past the timeout gives `error` on its own card and the others still return.
- **Risks:** the fake refactor touches all of `fake/`; T6, T7, T8 wait for it.

### T5. UI shell: sidebar, system switcher, header (L)

- **Result:** a left sidebar replaces the tabs, grouped: Fleet, Overview, Findings; Messaging (Connections; Subjects, Micro services, Applications planned); JetStream (Streams, Consumers, KV Stores, Object Stores; Placement, Capacity planned); Cluster (Servers, Events; Topology, Accounts, Users & auth planned). Collapsible, with badges (Fleet: systems not OK; Findings: unmuted firing). A system switcher at the top of the sidebar. The header: page title, account scope (`ListAccounts`), search, the time selector (the existing `RangePicker`), and the theme as one icon button with a Light/Dark/System menu (`menuitemradio`). Planned pages are hidden (proposed).
- **Files:** `web/src/App.tsx`; `components/Header.tsx` rewritten; new `Sidebar.tsx`, `SystemSwitcher.tsx`, `AccountScope.tsx`, `SearchJump.tsx`, `ThemeMenu.tsx` (replaces `ThemeSwitch.tsx`, keeps `lib/theme.ts`); `web/src/nav.ts` (group, path, label, icon, `planned`, badge source); `lib/useTimeRange.ts` becomes `useGlobalQuery` (range + system + account) so every `${to}${query}` link carries the system; `api/queries.ts`, `requests.ts`: every hook takes the system from `useSystem()` and keys on it; `header.css`, new `sidebar.css`, `responsive.css` (drawer at phone width).
- **Search:** a client-side jump over the current system's servers, streams, consumers, KV, object stores and accounts, from the list queries; no new RPC.
- **Tests:** every nav link keeps `?system=` and `?range=`; switching system changes query keys (the mock transport receives the new system); the theme menu sets `data-theme` and the stored value; the account scope writes `?account=`; planned entries are not rendered. Failing case: an unknown `?system=` shows "system not found" with a link to Fleet, not a blank page.
- **Risks:** `Header.test.tsx`, `ThemeSwitch.test.tsx`, `RangePicker.test.tsx` are rewritten, not deleted.

### T6. Series: bands and new metric IDs (M)

- **Proto** (`timeseries.proto`): `Point`: `optional double low = 3`, `optional double high = 4`; `Series`: `EntityRef top = 8`, `repeated Point top_points = 9` (busiest server: highest mean over the range).
- **New metrics** (from collected tables): `cluster.cpu_percent`, `cluster.mem_bytes` as bands (median = value, low = min, high = max over `server_stats` per epoch); `cluster.streams` (not `KV_`/`OBJ_`), `cluster.kv_buckets`, `cluster.object_stores`; `cluster.accounts` (conns > 0 or js_streams > 0); `cluster.meta_leader_changes`; `cluster.assets_replicated`, `cluster.assets_single` (streams by `stream_config.replicas`, consumers by `consumer_config.replicas`, 0 inherits the stream's); `server.slow_consumers` (increase per process); `consumer.delivered_rate` (the leader's `delivered_consumer_seq` increase per second).
- **Files:** `timeseries.proto`, `internal/api/metrics.go`, `internal/api/storeprov/series.go`, new `series_band.go`, `internal/api/fake/series.go`.
- **Tests:** 3 servers at CPU 2, 5, 40 give band 2/5/40 and `top` = the 40% server; replicas 0 on an R3 stream counts as replicated; `delivered_rate` has no rate across a gap above 3 intervals. Failing case: `cluster.cpu_percent` with an entity is `invalid_argument`.
- **Risks:** the band query over 20 000 epochs x 24 servers within the 30 s limit (add a `limits_test.go` case); median by `quantile_cont`.

### T7. Servers backend and VARZ limits (M/L)

- **Migration 0025** (nullable `server_stats` columns with `COMMENT`): `max_payload BIGINT`, `max_pending BIGINT`, `write_deadline_ns BIGINT`, `ping_interval_ns BIGINT`, `max_pings_out INTEGER`, from VARZ. Update the migration golden test.
- **Proto** (`servers.proto`): `ServerSummary`: `optional int32 stream_leaders = 18`, `optional int32 consumer_leaders = 19`, `optional int64 slow_consumers_in_range = 20`, `repeated string tags = 21`. `GetServerResponse`: `repeated RouteView route_views = 11` (`{remote_name=1, Duration rtt=2, optional int64 pending_bytes=3}`), `optional int32 gateways = 12`, `optional int32 leafnodes = 13`, `ServerLimits limits = 14` (`{optional int64 max_payload=1, optional int64 max_pending=2, Duration write_deadline=3, Duration ping_interval=4, optional int32 max_pings_out=5, optional int64 js_max_storage=6, optional int64 js_max_memory=7, optional int32 max_connections=8}`), `repeated string limits_differ = 15` (differs from the majority), `KubernetesPlacement kubernetes = 16` (`{pod=1, namespace=2, node=3, Timestamp scheduled_at=4, int32 restarts_in_range=5}`), `Timestamp config_loaded_at = 17`. Check numbers against the file.
- **Data:** leaders from `server_stats.js_streams_leader`, `js_consumers_leader`; slow consumers increase; `servers.tags`; `routes`; `gateways`, `leafnodes` counts; `config_load_time`; JetStream limits and `max_connections`; pod and node from the newest k8s event for the server (`raw.node`, `raw.pod`, `raw.namespace`, only with the watcher); the VARZ limits are new.
- **Files:** `servers.proto`, `internal/api/storeprov/servers.go`, `internal/api/fake/servers.go` (+ `global.go`), `internal/collect/decode.go`, `snapshot.go`, `internal/store/write_server.go`, the migration, golden and archive tests, README Database.
- **Tests:** decode stores `max_payload` from a VARZ fixture; `limits_differ` names `max_payload` when one of 3 differs; a rebuild from Parquet written before 0025 loads NULL limits. Failing case: with Kubernetes off, `kubernetes` is unset, not empty strings.

### T8. Events backend: folding, routine, facets, histogram (L)

- **Proto** (`events.proto`): `ListEventsRequest`: `bool hide_routine = 9`, `bool fold_repeats = 10`, `int32 histogram_buckets = 11` (0 none, at most 120). `Event`: `int32 repeats = 21`, `Timestamp first_time = 22`, `bool routine = 23`. `ListEventsResponse`: `int32 routine_hidden = 4`, `repeated EventFacet facets = 5` (`{dimension=1 ("source"|"server"), value=2, count=3}`), `repeated RoutineGroup routine_groups = 6` (`{label=1, kind=2, client_name=3, count=4}`), `repeated EventBucket histogram = 7` (`{Timestamp start=1, int32 warning=2, int32 notable=3, int32 routine=4}`), `int32 total_unfiltered = 8`. Check numbers against the file.
- **Rules (proposed):** routine = kinds `client.churn`, `consumer.ack_sampled`, `api.audit`, `account.connections`, and connects/disconnects of tooling clients (the stored `epochs.loopback_tooling` and `ToolingNames`, as the client checks). Folding: consecutive events with the same source, kind, server, account, stream, consumer, client_name and reason, at most 10 minutes apart, become one row showing the newest, `first_time` and the count; a k8s row uses its stored count. A facet counts the filtered set without its own dimension's filter. Paging by the group's newest (time, id).
- **Files:** `events.proto`, `internal/api/storeprov/events.go`, new `events_fold.go`, `routine.go`, `internal/api/fake/events.go`, `internal/api/normalize.go`.
- **Tests:** 448 churn and loopback events plus 5 notable give `routine_hidden = 448` and 5 rows; five `k8s.pod.failed_scheduling` within 6 minutes fold to one row `repeats = 5`; facet counts match an unfolded count; the page token is stable while events arrive. Failing case: two identical kinds 11 minutes apart do not fold.
- **Risks:** gaps-and-islands query over 500 000 events (add a `limits_test.go` case); no repeated or skipped group at a page boundary.

### T9. Overview backend: tiles, placement, busiest consumers (M)

- **Proto:** `Tile`: `string description = 13`, `string value_text = 14`. `StreamSummary`: `repeated ReplicaPlacement placement = 13`. `ConsumerSummary`: `optional double delivered_per_second = 15`. `ListConsumersRequest`: `ConsumerSort sort = 6` (`{UNSPECIFIED (name), DELIVERED_RATE}`), `int32 limit = 7`. Check numbers against the files.
- **Tiles:** servers (N / expected; versions, uptime), meta_leader (`value_text` = majority `server_stats.meta_leader`; series `cluster.meta_leader_changes`), streams (count; R3; lagging from `stream_replicas`), kv (count; R1), object_stores, consumers (durables; firing consumer-002), accounts, clients (`cluster.connections`; distinct apps before "@"; slow), storage (`cluster.js_storage_bytes`; limit = sum of `js_max_storage`; growth by the existing Theil-Sen rule). Window min/max are `Series.min`/`max`.
- **Placement:** one square per replica: LEADER (`stream_stats.is_leader`), IN_SYNC / BEHIND / OFFLINE (the leader's `stream_replicas`), DISAGREES (differing `config_hash`), MISSING (a peer that does not report).
- **Delivered rate:** the leader's `delivered_consumer_seq` increase over the 15 minutes before the range's end.
- **Files:** `overview.proto`, `streams.proto`, `consumers.proto`, `internal/api/storeprov/overview.go`, `streams.go`, `consumers.go`, `internal/api/fake/tiles.go`, `jetstream.go`.
- **Tests:** a lagging follower gives `[LEADER, IN_SYNC, BEHIND]`; `DELIVERED_RATE` with `limit = 6` returns the fastest first; the meta leader tile carries `value_text`. Failing case: a consumer without a leader row has `delivered_per_second` unset, not 0.

### T10. KV Stores and Object Stores pages (M)

- **Proto** (`buckets.proto`): `KeyValueStore` after 1, 2: `optional int32 history=3`, `Duration ttl=4`, `optional int64 max_bytes=5`, `optional int32 replicas=6`, `string storage=7`, `optional int64 keys=8` (`num_subjects`), `optional int64 bytes=9`, `optional int64 messages=10`, `optional int32 watchers=11`, `Status status=12`, `repeated string finding_codes=13`, `repeated ReplicaPlacement placement=14`, `bool present=15`, `bool expected=16`, `string compression=17`, `string mirror=18`, `bool allow_msg_ttl=19`, `string description=20`, `optional double bytes_per_day=21`. `ObjectStore` after 1, 2: `string description=3`, `optional int64 bytes=4`, `optional int64 chunks=5`, `optional int32 replicas=6`, `string storage=7`, `optional int64 max_bytes=8`, `Duration ttl=9`, `bool sealed=10`, `Status status=11`, `repeated string finding_codes=12`, `repeated ReplicaPlacement placement=13`, `bool present=14`, `bool expected=15`, `optional double bytes_per_day=16`.
- **Data:** all collected: `KV_*`/`OBJ_*` streams in `streams`, `stream_stats`, `stream_config`; watchers in `consumer_stats`/`consumer_config`; expected buckets from `inventory` kinds `kv`/`object_store`.
- **Files:** `buckets.proto`, new `internal/api/storeprov/buckets.go`, `internal/api/fake/buckets.go`, `web/src/pages/KvStores.tsx`, `ObjectStores.tsx`, `web/src/api/buckets.ts`, `nav.ts`, `App.tsx`. Rows link to the stream detail of `KV_<bucket>` / `OBJ_<bucket>`.
- **Tests:** the fake `KV_customer-config` appears as bucket `customer-config` with its watchers; an inventory bucket missing from the store is listed `present = false` with its finding; the account scope filters rows. Failing case: a stream `KVX_foo` is not a KV bucket.
- **Note:** `keys` counts subjects including deleted keys; the tooltip says so.

### T11. Fleet page (S/M)

- **Result:** `/fleet`, a card per system, worst first: name, environment, state (colour plus a shape), the headline (worst finding or collection error), servers, clusters, streams, connections, the 48-cell strip, freshness. A card opens that system's Overview. With more than one system `/` lands on Fleet (proposed).
- **Files:** `web/src/pages/Fleet.tsx`, `web/src/api/systems.ts`, `styles/fleet.css`, `nav.ts`, `App.tsx`.
- **Tests:** order critical, warning, info, not collecting; the strip legend draws the cell it explains; `example-staging` shows its error and "not collecting". Failing case: a summary with `error` renders "summary unavailable" and still links to the system.

### T12. Overview cockpit UI (L)

- **Result:**
  - Status line "Serving normally" plus a count badge, following the worst unmuted severity and freshness ("Not collecting" when stale).
  - `MetricBox`: title with an (i) tooltip from `description`; the value top right, coloured by status; a line chart; a range track with min left, max right and a dot at the current value; the limit in the note; a state word only when not OK; "steady at N all window" when min equals max.
  - Grouped-cause cards built client-side from `ListFindings` (by `cause.id`, else check code plus parent entity), with entities, open and mute; a strip of muted findings with their reasons.
  - Trends: servers, connections, slow consumers, msgs/s and bytes/s in and out (out dashed), CPU and memory as band + median + busiest, assets R3/R1 stacked.
  - Story lanes: Kubernetes, findings, events from `GetStatusTimeline`.
  - Busiest consumers: 6 rows with sparklines from `consumer.delivered_rate`.
  - Streams with placement squares.
  - A `Legend` that draws each series' own stroke, dash and fill.
- **Files:** `web/src/pages/Overview.tsx`, new `components/MetricBox.tsx`, `TrendChart.tsx`, `Legend.tsx`, `CauseGroups.tsx`, `BusiestConsumers.tsx`, `PlacementSquares.tsx` (reused by T10, T14), `charts/SeriesChart.tsx` (bands), `web/src/api/overview.ts`, `styles/overview.css`.
- **Tests:** the dot sits at 50% for the midpoint; "steady at 3 all window" when min equals max; the "out" swatch is dashed; four consumer-009 findings on `order-jobs` become one card x 4; chart accessible names include the value and range. Failing case: a tile without a value shows "no data", never 0, and draws no dot.
- **Risks:** about 10 series calls per load; refetch only on the live interval.

### T13. Events page and Finding detail UI (M)

- **Events:** a histogram (warnings, notable, routine); facets "Worth reading N / Everything N", source and server with counts; "Hidden as routine: ..." listing the routine groups with "Show them"; folded rows "x5 in 6 min"; new events held behind a "3 new" pill.
- **Finding detail:** headline; "Condition since X, first seen by nats-lens Y" (with "already true when nats-lens started" at the history start); a per-server evidence table from `GetStream`/`GetConsumer` for stream and consumer findings; remediation and related settings; "N other entities have this finding"; "Around the time it began" (`ListEvents` +-10 min around `condition_since`, else `since`); entity timeline, transitions, links, mute/unmute.
- **Files:** `web/src/pages/Events.tsx`, `FindingDetail.tsx`, `components/EventRow.tsx`, new `EventFacets.tsx`, `EventHistogram.tsx`, `FindingEvidence.tsx`, `web/src/api/events.ts`.
- **Tests:** "Worth reading" sends `hide_routine` and `fold_repeats`; a server facet sets the server entity; a finding with `condition_since` before `history_start` shows both times. Failing case: a client finding renders without an evidence table and without an error.

### T14. Servers page and Server detail UI for 20+ servers (L)

- **Servers:** headline data points (answering N of M with a square per server; versions as a split bar; restarts in 24 h; store median, range and fullest server; slow consumers and the server with most); fleet charts (CPU and memory bands with median and busiest; total connections; msgs in/out); "Every server" grid grouped by cluster, coloured by a chosen metric (CPU, memory, connections, store used, leaders, version), meta leader outlined, down dashed, hover for name and value, click to open; the list grouped by cluster (flat with one cluster), problems first, "Needs attention" default (status not OK, not answering, version not the majority's, or store at 60% or more), "Show N more", a filter by name, version or tag.
- **Server detail:** five tiles with sparklines and ranges; Where it runs (hidden without the watcher); Limits and settings with differences marked; Recent events for the server; endpoints answered.
- **Files:** `web/src/pages/Servers.tsx`, `ServerDetail.tsx`, new `components/ServerGrid.tsx`, `ServerHeadline.tsx`, `FleetChart.tsx`, `web/src/api/servers.ts`, `styles/pages.css`.
- **Tests** (on the `example-global` demo): 3 cluster groups; the down server first in its group; "Needs attention" lists 4, "All" 24; colouring by store marks the 71% server in the top band. Failing case: with one cluster the list is flat, and a server without `cpu_percent` shows "-" with no colour, not 0%.
- **Risks:** grid colours need light and dark values; the colour legend draws each swatch.

## Decisions (proposed, not yet confirmed)

1. An empty `system` in an API call means the default system (the first configured).
2. A single-system config's id comes from an optional top-level `system_id`, default `default`.
3. Planned pages are hidden, flagged in `nav.ts`.
4. Connections stays in the sidebar under Messaging until Applications exists.
5. Landing: Fleet with more than one system, Overview with one.
6. The system is the query parameter `?system=`.
7. A system whose store fails to open fails the start; NATS being down never affects readiness.
8. Per system: NATS, accounts, inventory, Kubernetes, retention, archive retention, directories, memory. Global: interval, timeout, checks, listen, IAP.
9. Routine events are a fixed rule in phase 1.
10. Folding: same subject and kind, gaps of 10 minutes or less.
11. CPU and memory trends are always band + median + busiest.
12. Placement squares: one per replica.
13. No stream-wide mute scope in phase 1.
14. Every `/metrics` series gets a `system` label.
15. Search is a client-side jump; no "ask".
16. The account scope applies to JetStream lists, KV and object stores, findings, events and connections; system-wide tiles and charts stay system-wide and say "all accounts".
17. Migration 0025 adds the VARZ limits, nullable.
18. Demo systems: `example-prod`, `example-global`, `example-staging`.

## What the current data cannot support

- A "slow" server state: no per-reply latency is stored (`epoch_endpoints` has only state and error); it needs a collector change. Dropped from phase 1.
- Zones: not recorded. Show server `tags` (for example `az:`) when present.
- Object count per object store: JSZ does not give it; read-only permissions grant no JetStream API calls. Show size, chunks, subjects.
- KV keys: `num_subjects` includes deleted keys, so it is an upper bound.
- Synthesised story items ("redeploy, 2 reconnects"): no detector exists; lanes show stored events and windows.
- "Open in Placement", "Copy nats CLI check": Placement is not built.
- A collection error after a nats-lens restart lives in memory; T4 falls back to the newest `collector.connection_closed` / `collector.not_connected` event.
