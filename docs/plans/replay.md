# Replay build plan

Working plan for the Replay page (`docs/features/replay.md`, board `Replay.dc.html` of the design canvas). Everything here was proposed by the planning session of 2026-10-19 and is not yet confirmed by the owner. When a task lands, its lasting content moves into `docs/features/replay.md` and `docs/design.md`; this file then only tracks status.

The board's sample names come from a private deployment. Use the example shop of `inventory.example.yaml` and the demo's systems instead.

## Status

| Task | Status |
|---|---|
| T1 API and store | built on `feat/replay-page`, awaiting review |
| T2 Demo story and conformance | open |
| T3 UI | open |
| T4 Feature docs and audit | open |

## Rules for every step

- No guesses: a step is shown only when a collection, event or finding backs it; a period without collections is a gap step.
- No causal claims ("because"); order alone shows that two steps are adjacent.
- Counts that were not observed are unset, never 0.
- No migration is needed; every column below exists.

## What backs each part

| Part | Source | Query sketch |
|---|---|---|
| Restart | `servers` (one row per process: `server_id`, `server_name`, `start_time`, `first_epoch`, `last_epoch`, `first_seen_at`, `last_seen_at`, `version`) | the `procs` CTE of `checks/server-004.sql`: `lag(server_id, version, last_seen_at, last_epoch) OVER (PARTITION BY server_name ORDER BY first_epoch)`, `WHERE prev_id IS NOT NULL AND coalesce(start_time, first_seen_at) > $a AND … <= $b`. Span `prev_last_seen` to `first_seen_at`. A version change says "restarts for an upgrade from X to Y". |
| Assets after a restart | `stream_stats`, `consumer_stats` at the new process's first epoch with `epoch_endpoints` `endpoint='JSZ' AND state='ok'`, against the old process at `prev_last_epoch` | Body: "Back at 09:18:30 with 4 streams and 9 consumers, 2 consumers fewer than before the restart." Never "from disk". |
| Lame duck, shutdown | `events` kinds `server.lameduck`, `server.shutdown` (source `sys`, `server_name`) | `time BETWEEN prev_last_seen - restart_window AND started`, `restart_window` from `p.Checks.RestartWindow()`. Without one: "No lame duck or shutdown event was captured". |
| Leaders taken | `stream.leader_elected`, `consumer.leader_elected` events (new leader in `server_name`); old leader from `is_leader` rows of `stream_stats`/`consumer_stats` at the restarting server's `prev_last_epoch` | Elections between the announcement (or last seen) and `first_seen_at + interval`, matched on stream/consumer and account as `consumer-013` does; grouped by new leader. |
| Quorum loss | findings `stream-001`, `consumer-001`, `system-002` and their `finding_transitions` (`time`, `kind`, `severity`, `detail`); advisories `stream.quorum_lost`, `consumer.quorum_lost` as evidence | Transitions before `$b`, plus the last one before `$a` ("already without quorum at A"). Firings of one check within 2 collect intervals fold into one span step; the span ends at the last resolve or says "still at B". |
| Findings | `finding_transitions` (also events of source `finding`) | Step links to `/findings/<finding_id>`. Info severity only as evidence. |
| Gone assets | `stream_stats`/`consumer_stats` at `epA` and `epB`, `servers`, `epoch_endpoints`, `consumer_config`; `stream.deleted`/`consumer.deleted` events | `epA` = newest `epochs.started_at <= $a` within 3 intervals, `epB` likewise. Gone = present at `epA`, absent from every row at `epB`. Holders at A = `list(DISTINCT server_name)`; `not_answering_at_b` = holders without JSZ ok at `epB`. Consumers follow `consumer-013`: `durable_name<>''`, `coalesce(inactive_threshold_ns,0)=0`, name `NOT LIKE 'JS\_%'`. A gone stream's consumers count under its row. Delete match copies `consumer-013`'s `cause` CTE, bounded by `<= $b`. Related findings `consumer-013`, `stream-014`, `consumer-010`, `stream-011`. |
| Kubernetes windows | `storeprov.k8sWindows` (from `k8s.node.drained`, `k8s.node.upgrade`, NATS pod events); `GetStatusTimeline.windows` | Reuse as is. |
| Kubernetes steps | `source='k8s'`: `k8s.node.drained`, `k8s.node.upgrade`; NATS pod `k8s.pod.failed_scheduling`, `backoff`, `oom_killed`. Pod `killing`/`started` go into restart steps as evidence. | |
| Node pools upgraded | node events' `raw`: `node`, `pool` (GKE label, else empty), `kubelet_version` | `count(DISTINCT raw->>'pool') FILTER (kind='k8s.node.upgrade' AND pool<>'')`; also distinct nodes drained and upgraded. Without pool labels the tile says "Nodes upgraded". |
| Leaders moved | leader election advisories | An election whose leader differs from the previous election of the asset, or from the `is_leader` row at `epA` for the first one. All elections counted separately; an election with an unknown previous leader is never a move. `meta.leader_elected` counted separately. |
| Client reconnects | `client.disconnect`, `client.connect`, `client.churn` events | Copy `client-004`'s `drops`/`connects`/`named`: `sum(least(dropped, connected))` over `[A,B)`, tooling left out by `ToolingNames` and the loopback rule of `storeprov/connections.go`. "Server Shutdown" disconnects count here, unlike `client-004`. |
| "NO quorum" reports | advisory count `kind IN ('stream.quorum_lost','consumer.quorum_lost')` in `[A,B)` | Labelled as advisories; server logs are not read. |
| Gaps | `epochs` spacing over 3 intervals (the series rule), `collector.gap` (`time` to `received_at`), `collector.k8s_gap` | `lead(started_at) OVER (ORDER BY epoch)` over `[a - 3i, b + 3i]`; leading and trailing gaps count. |

Not backed, so not shown: "from disk"; why a server restarted beyond the k8s event's summary; replica catch-up time (only as `stream-003`/`server-012` finding steps); which applications reconnected.

## Where T1 departed from this plan

Decided in the session that built T1, not confirmed.

- Matching is shared, not per provider: providers return the raw events (`api.ReplayEventKinds`, from A minus the restart window to the lookahead), and `FinishReplay` matches announcements, pod events and elections to restarts and stops, and adds a gone asset's findings from the transitions. `FinishReplay(facts)` takes everything from `ReplayFacts`, which also carries `Until`, `RestartWindow`, `Collected`, `EventsCaptured`, `LeadersAtA`, `MetaLeaderAtA` and `GoneRead`.
- Counts: `streams_gone_without_delete` and `consumers_gone_without_delete` leave out assets with a holder not answering at B, which `assets_unknown_at_b` counts. Gone counts need `GoneRead` (the demo reads no gone assets yet, so they stay unset there); event counts need a stored event up to B; `findings_raised` counts warning and critical findings that fired.
- A server that stops answering is a `RESTART` step with warning severity; a gone asset without a delete event is critical, with a delete or a holder not answering warning. Gone steps fold by stream and class.
- Quorum spans of one check fold when they start within the fold window, not while one is open, so a long loss does not swallow later ones. A span from before A starts its step at A and says since when.
- A restart step's time is the old process's last answer, even when that is before A.
- Steps between A and B include both ends.
- The store reads at most 20,000 events and 20,000 transitions, the earliest; a cut sets `EventsTruncated` or `TransitionsTruncated` with the time of the last row read, which mark the steps truncated, unset the counts taken from the cut rows and give `complete_until` (new field 16). A quorum loss whose resolve may be in cut transitions is not called open at B.
- A stop is a server that answered in the collection at A or later and not in the one at B; the demo reads stops by the same rule.
- A process that started by B but was first collected after B is no restart: its server is a stop that answered again after B. `servers_stopped` needs the collection at B.
- A `collector.gap` overlapping A to B (starting at B included) unsets the event and node counts; a `collector.k8s_gap` unsets the node counts. "No lame duck or shutdown event was captured" needs capture over the step's whole announcement window, so both providers read events and collector gaps from the restart window before the earlier of A and the collection at A (`api.ReplayReadFrom`), and a window starting before that read claims nothing.
- A step between A and B takes in nothing from after B.
- A loss already open at A starts at the firing that began it: the providers return that firing as well as the newest transition before A.
- A gone stream takes in the gone findings of the durables that went with it (`ReplayGone.DurablesWithIt`).
- The demo leaves leaders at A unset without a collection at A, watches Kubernetes when its scenario has a Kubernetes lane, and counts nodes as the store does: a node event that names no node leaves the node counts unset. Its node events name no node yet, so its node counts are unset; T2 should add `node` and `pool` to them.
- The delete-event match once wrote `e.received_at >= ? - INTERVAL 30 SECOND` with a bound `time.Time`. A bound `time.Time` binds as TIMESTAMP WITH TIME ZONE unless its type is inferred from a column, so that expression stays TIMESTAMPTZ and the comparison casts the TIMESTAMP column in the host's time zone: no rows in Europe/Berlin, correct in UTC (found by the T1 review). The match now subtracts from the epochs' TIMESTAMP column inside the gone query. Plain `col >= ?`, `?::TIMESTAMP - INTERVAL ...` or subtracting in Go are correct; see `internal/store/archive.go`.
- The demo leaves the meta leader at A unknown (the scenario names only the current one), and its restarts have no versions or asset counts; T2 can add them.

## Proto

The strip needs no new RPC: `GetStatusTimeline` with `include_ok: true` over the page range gives one row per check plus `windows`; the UI takes the worst cell per bucket, the rule of `storeprov.strip()`. If the payload proves heavy, a `GetReplayStrip` is a leftover.

New file `proto/natslens/v1/replay.proto`; in `service.proto`: `rpc GetReplay(GetReplayRequest) returns (GetReplayResponse) { option idempotency_level = NO_SIDE_EFFECTS; }`. Check that `Severity`, `EntityRef`, `EventRef` and `TimeWindow` exist with these names and adjust if not.

```proto
message GetReplayRequest {
  string system = 1;
  google.protobuf.Timestamp a = 2;   // required
  google.protobuf.Timestamp b = 3;   // required, after a
}
enum ReplayStepKind {
  REPLAY_STEP_KIND_UNSPECIFIED = 0;
  REPLAY_STEP_KIND_KUBERNETES = 1;  // drain, node upgrade, NATS pod event outside a restart
  REPLAY_STEP_KIND_RESTART = 2;     // a process replaced, or a server that stops answering
  REPLAY_STEP_KIND_LEADERS = 3;     // meta leader change; leader moves not tied to a restart
  REPLAY_STEP_KIND_QUORUM = 4;
  REPLAY_STEP_KIND_GONE = 5;
  REPLAY_STEP_KIND_FINDING = 6;     // other warning/critical firings and changes
  REPLAY_STEP_KIND_RESOLVED = 7;    // a finding resolving, a server answering again
  REPLAY_STEP_KIND_GAP = 8;         // no collections, nats-lens not running, k8s watch gap
}
enum ReplayEvidenceKind {
  REPLAY_EVIDENCE_KIND_UNSPECIFIED = 0;
  REPLAY_EVIDENCE_KIND_EVENT = 1;
  REPLAY_EVIDENCE_KIND_FINDING = 2;
  REPLAY_EVIDENCE_KIND_COLLECTION = 3;
}
message ReplayEvidence {
  ReplayEvidenceKind kind = 1;
  string id = 2;                      // event ID, finding ID, or epoch number (decimal)
  google.protobuf.Timestamp time = 3;
  string label = 4;                   // "pod nats-1 Killing", "stream-001 Stream quorum lost", "collection of 09:18:30"
  EntityRef entity = 5;               // collection: the entity to open at that moment; unset = system
}
message ReplayStep {
  string id = 1;                      // stable in a response: kind + first evidence
  google.protobuf.Timestamp time = 2;
  google.protobuf.Timestamp end = 3;  // unset for a moment
  bool open_at_b = 4;                 // the span had not ended at B
  ReplayStepKind kind = 5;
  Severity severity = 6;              // unset = neutral dot
  string title = 7;
  string body = 8;
  repeated EntityRef entities = 9;    // at most 50
  int32 entity_count = 10;
  repeated ReplayEvidence evidence = 11; // at most 10
  int32 evidence_count = 12;
  bool outside_range = 13;            // after B, closes a step in the range
}
message ReplayCounts {   // unset = not observed, never 0
  optional int32 servers_restarted = 1;
  optional int32 servers_stopped = 2;           // stopped answering, not back by B
  optional int32 streams_gone_without_delete = 3;
  optional int32 consumers_gone_without_delete = 4;
  optional int32 assets_gone_with_delete = 5;
  optional int32 assets_unknown_at_b = 6;       // a holder did not answer JSZ at B
  optional int32 leaders_moved = 7;
  optional int32 leader_elections = 8;
  optional int32 meta_leader_changes = 9;
  optional int32 quorum_lost_reports = 10;
  optional int32 node_pools_upgraded = 11;      // unset without Kubernetes or pool labels
  optional int32 nodes_upgraded = 12;
  optional int32 nodes_drained = 13;
  optional int32 client_reconnects = 14;
  optional int32 findings_raised = 15;
}
message GoneAsset {
  EntityRef entity = 1;                         // stream (KV_/OBJ_ included) or durable consumer
  repeated string servers_at_a = 2;
  repeated string not_answering_at_b = 3;       // holders without JSZ at B
  google.protobuf.Timestamp last_reported = 4;
  int64 last_epoch = 5;
  EventRef delete_event = 6;                    // unset when none captured
  string delete_client = 7;                     // "analytics-worker (10.20.0.12)"
  int32 consumers_with_it = 8;                  // a gone stream's durables
  repeated string finding_ids = 9;
}
message GetReplayResponse {
  google.protobuf.Timestamp a = 1;
  google.protobuf.Timestamp b = 2;
  int64 epoch_a = 3;
  google.protobuf.Timestamp epoch_a_at = 4;     // unset: no collection at A
  int64 epoch_b = 5;
  google.protobuf.Timestamp epoch_b_at = 6;
  repeated ReplayStep steps = 7;                // time order; outside_range steps last
  bool steps_truncated = 8;
  int32 steps_omitted = 9;
  ReplayCounts counts = 10;
  repeated GoneAsset gone = 11;
  bool gone_truncated = 12;
  repeated TimeWindow windows = 13;             // k8s windows overlapping [A,B]
  bool kubernetes_watched = 14;
  google.protobuf.Timestamp history_start = 15;
}
```

The handler fills `account_name` in every `EntityRef` through `nameAccounts`.

Evidence links in the UI: an event opens `/events?id=<id>`; a finding opens `/findings/<id>`; a collection opens the entity's page with a fixed range ending just after `epoch_at` (`/servers/:name`, `/streams/:account/:stream`, `/consumers/...`), or with no entity `/placement?from=<epoch_at-1h>&to=<epoch_at+1s>`. A gone asset also gets "Compare in Placement": `/placement?from=<B-1h>&to=<B>&cmp=<A ISO>`.

## Server side

- `internal/api/replay.go`: Go structs `ReplayFacts` (`Restarts` with name, old and new ID, last and first seen, versions, announcement `EventRef`, pod events, leaders taken per new leader, assets before and after; `Stops`, `Elections`, `QuorumEvents`, `Transitions` to `B + lookahead`, `K8sEvents`, `Windows`, `Gone`, `Gaps`, `ServerReturns` after B; raw counts only SQL gives cheaply: reconnects, pools, nodes) and `FinishReplay(facts, a, b, interval) *v1.GetReplayResponse`, which writes titles and bodies, folds, sorts, caps and counts, so the store and the demo share the wording. Same split as `GetPlacement`/`FinishPlacement`. `Provider` gains `Replay(ctx, req) (*ReplayFacts, error)`.
- Handler `GetReplay`: A and B required, A < B; B − A at most 24 h; B clamped to `p.Now()`; the 30 s `QueryTimeout` through `call()`. The provider refuses `a < now − Retention` with `ErrInvalid`: "A is older than retention (14d); Replay reads the database only". The demo uses its own `Info` retention.
- Folding: steps with one fold key (kind plus check code, server, or k8s kind plus server) within `max(2 min, 2 intervals)` merge into one span step with a count ("nats-us-4 cannot be scheduled (5 times, 09:12–09:18)"). Quorum findings of one check fold across entities ("N consumers have no quorum for M minutes"). `server-004` transitions and pod `killing`/`started` events are absorbed into the restart step as evidence.
- Order: time, then kind order (k8s, restart, leaders, quorum, gone, finding, gap), then ID. At most 200 steps, keeping critical, warning, restart, gone and gap steps first; at most 10 evidence and 50 entities per step; at most 200 gone assets.
- After B: the first resolve after B of a finding that has a step, looking ahead to `min(now, B + 6h)`, and a stopped server's next process `first_seen_at`; at most 10, `outside_range=true`.
- `internal/api/storeprov/replay.go`: one sequential set of queries as above; reuses `k8sWindows`, the tooling rule of `connections.go` and `p.Checks.RestartWindow()`. `storeprov.Provider` gains `Kubernetes bool`, set in `cmd/natslens/system.go` from `kubernetes.enabled`, which feeds `kubernetes_watched`; when false the pool, node and k8s counts stay unset.
- Demo, `internal/api/fake/replay.go`: builds `ReplayFacts` from the scenario: restarts from `serverSpec` (`oldID`, start time) and `serverDown`; elections, quorum, k8s and delete events from `events`; transitions from `findings` and `transitions`; windows from `windows`; gaps from `stale` (`example-staging` shows "No collections since …"); gone assets from a new `scenario.gone` list.

## Demo story

`example-global`'s pool-us upgrade, 08:40:12–09:20:30, is the default A–B: `nats-us-1`, `-2`, `-3` restart; `inventory-changes` has no quorum 08:58:40–09:01; `nats-us-4` announces shutdown and stops answering, so `KV_label-templates` is held by 1 server at A and "none answering (nats-us-4)" at B; leaders move to `nats-us-2` and `nats-us-5`. Additions: `server.lameduck` events for us-1, us-2, us-3 at kill time plus 1 s; a few `consumer.leader_elected` events after each; two R3 durables on `inventory-changes` (`restock-alerts`, `stock-audit-export`) held by all three of `nats-us-0..2` at A and by none after `nats-us-2`'s restart, without a delete event, with `consumer-013` critical findings firing about 08:59:30 and resolving after the 1 h lookback (the "Find assets that vanished" scenario and an outside-range step; the 24 h resolved tally and some pinned demo tests change). `example-prod`'s existing story (09:14–09:50, `analytics-aggregator` deleted by `analytics-worker`) gives the gone-with-delete case.

Conformance rows in `internal/api/apitest/conformance.go` (A and B from the system's newest timeline window, else the last 2 h): a restart step with event and collection evidence; a quorum loss as one span step; an asset gone without a delete event with servers at A; a gone asset with a delete event naming the client; a step outside the range; a gap step; in every system the counts agree with the steps and the gone list; A older than retention is `invalid_argument`. The store fixture is `storeprov/fixture_test.go`.

## UI

- Route `/replay`, `web/src/pages/Replay.tsx`, in `App.tsx`; `NAV` entry in group Cluster after Events. h1 "Replay", line "Pick two moments; nats-lens tells you what changed in between, and in what order."
- URL: `?a=<ISO>&b=<ISO>` plus the global range and `system`; parsing, defaults and clamping in `web/src/lib/replayUrl.ts`. Missing `a` or `b`: the newest k8s window in the range ± 5 min, else the last hour of the range, written with `replace`. A or B outside the page range still loads; the scrubber says "A is before this range".
- Data: `requests.getReplay`, `api/replay.ts` (adapter to view models), `useReplay(a, b)` keyed `["replay", sys, a, b]`, `keepPreviousData`, no refetch interval, key debounced 300 ms while a handle moves. The strip uses `getTimeline` with a new `includeOk` argument, keyed apart from the Overview's.
- `components/ReplayScrubber.tsx`: strip cells by status, no-data cells dashed as in the Fleet strip, `aria-label` from `runsText(cellRuns(...))`; window bars as buttons ("Replay GKE node upgrade 09:14–09:50") that set A and B; two handles `role="slider"` with `aria-valuetext` "Moment A, 09:14 UTC 17 Oct", arrows ± 1 bucket, Shift+arrows ± 10, Home/End; A at least one interval before B; drag and click move the nearer handle; two `datetime-local` inputs "A (UTC)", "B (UTC)" as in Placement's compare input.
- `ReplaySteps`: `<ol aria-label="What happened, in order">`, each step a `<time>` or span, a dot (`--k8s` for k8s, the ok colour for resolved, else the severity badge), title, body, evidence links; gap steps dashed; an "After B" divider before outside-range steps, each tagged "outside the range"; footer "Built from collections, NATS advisories and Kubernetes events; every line links to its evidence."
- `ReplayCounts`: tiles in the style of `MetricBox`; an unset count shows "—" with "not observed".
- `ReplayGone`: table with Asset (link to its page at A), Kind, Servers at A, At B ("none", or "none answering: nats-us-4"), Delete event (link or "none captured"), Last reported.
- "Copy link to A–B": a live range becomes fixed (`pauseRange`), the absolute URL goes to `navigator.clipboard`, a `role="status"` confirms. "Export as incident log": text built in `lib/replayLog.ts`, downloaded as `replay-<system>-<A>-<B>.txt`: header (system, A and B in UTC, "Built from …"), "What happened, in order" one line per step with time, kind, title, indented body and evidence as absolute URLs, "After B", "Between A and B", "Gone between A and B", the gaps.

## Tasks

- **T1 API and store.** `replay.proto`, the service RPC, `make proto` with generated code committed; `internal/api/replay.go` with `replay_test.go` (folding, ordering, caps, outside-range rule, counts); handler validation tests; `storeprov/replay.go` with `replay_test.go` over fixture rows (restart with lame duck, leaders taken, quorum span, gone with and without delete, a holder not answering, a gap, an outside resolve, refusal past retention); a fake `Replay` from the existing scenario data so the demo compiles and serves; `docs/design.md` (the RPC, limits, timeouts).
- **T2 Demo story and conformance.** The `example-global` additions, `scenario.gone`, `example-staging` gaps, updated demo tests, the conformance rows passing for store and demo; `demo-mode.md` and the Demo section of `design.md`.
- **T3 UI.** `nav.ts`, the route, `api/replay.ts`, `requests.ts`, `queries.ts`, `lib/replayUrl.ts` and `lib/replayLog.ts` with tests, the four components, `pages/Replay.test.tsx` (keyboard handles, window buttons, the After B divider, unset counts, copy link, export text); `navigation.md` and the UI section of `design.md`.
- **T4 Feature docs and audit.** `replay.md` to `Status: partial` with its Not supported list, `docs/features/README.md` table and coverage map, the audit, leftovers into `docs/plans/next.md`.

## Leftovers, most valuable first

1. "Replay this window" links from the Overview's windows and the finding page.
2. A catch-up step ("replicas current again after N min") from `stream_replicas.current` across epochs.
3. `GetReplayStrip` if the timeline payload is too heavy.
4. A span longer than 24 h.
5. Leader moves from snapshot diffs when advisories are missing.
6. Per-application reconnect steps, after Applications ships.
7. Markdown or JSON export.

Not supported: moments older than `retention` (no archive reads), causal claims, "from disk" wording, quorum reports from server logs.
