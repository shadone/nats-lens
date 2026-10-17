# nats-lens design

nats-lens observes a NATS deployment and answers the questions an operator asks when something goes wrong: what failed, when, on which server, and what else happened at the same time. It is read-only towards NATS.

Status: draft; the decisions in "Decisions awaiting confirmation" are proposed, not yet confirmed by the owner.

## Questions it must answer

These come from real incidents on JetStream clusters in Kubernetes and are the acceptance test for the design.

1. A durable consumer disappeared. When, from which server's view, and which client or event removed it?
2. Clients timed out during a GKE node upgrade. Which servers restarted, when did replicas lose quorum, how long until they caught up?
3. Is any server or stream running out of disk? When will it fill at the current rate?
4. Is any consumer's backlog growing, that is, filling faster than it drains?
5. Do all servers agree on each stream's and consumer's configuration, creation time and peers?
6. Which client connections are slow, stalled or reconnecting, and which service owns them?

## Overview

| Topic | Decision | Why |
| --- | --- | --- |
| Language | Go | nats-server exports typed structs for every monitoring payload (`server.Varz`, `server.Connz`, `server.JSInfo`, ...); jsm.go has open-source checks to reuse; the NATS Go client is the reference client |
| Storage | Embedded DuckDB, plus hourly Parquet files as the durable history | One process, no infrastructure; analytic SQL over snapshots; the database is a cache that can be rebuilt from Parquet |
| Checks | SQL files with a metadata header, run after every snapshot | Easy to read, test and add; an agent can run the same SQL |
| API | Connect (connectrpc.com) with protobuf in this repo | Typed end to end; the API is private between the binary and its own UI |
| UI | React and TypeScript built with Vite, embedded into the Go binary | One deployable; Node only at build time |
| Auth | None in the tool; deploy behind IAP or a similar proxy | An identity-aware proxy already decides who may pass; the tool verifies its signed header for mutes |
| Alerting | Findings exported at `/metrics`; alert rules in the deployment's Prometheus or Cloud Monitoring | The deployment has an alerting system already; the tool delivers no alerts itself |
| Deployment | One replica on GKE with `Recreate` and a PVC, preferring a node without a NATS server pod | DuckDB has one writer |

One process, `natslens run`, collects, captures events, runs the checks, archives, and serves the API and the UI, because DuckDB lets one process hold a database file. It does so for every configured system, each with a database of its own.

## Multiple systems

A system is one NATS deployment with its own system account. The configuration lists systems (`systems`), or describes one (a file without `systems`, which is one system with the id `system_id`, default `default`, in `data_dir` unchanged). `Config.ResolvedSystems()` is the one path to per-system settings; `Config.ForSystem` gives each system the configuration a single-system file would, so the code that runs one system knows nothing of the others.

- **What a system owns.** Its database and archive, its collector with its own connection and server tracker, its event capture and connections, its Kubernetes watcher (own kubeconfig and context), its check engine and its archive export. Log lines carry `system`. Systems share the process, the API, the check definitions (`checks.Set`) and the global settings: interval, timeout, events tuning, checks, listen, IAP, `connz_limit`, `shutdown_grace`.
- **Start.** The stores open in parallel, each with its own WAL recovery and rebuild, before the API serves. A store that fails to open fails the start: it cancels the other opens, so a refusal does not wait for another system's rebuild, and the stores already open are closed again. A stop signal during the opens is a clean exit. Each data directory gets `system.json` with the system's id on first open; a directory marked for another id refuses the start, and a directory without the file (a single-system nats-lens's) is adopted. A database in the top-level `data_dir` that no listed system uses is warned about.
- **Start check.** Before any system starts, one pass checks every system: its data directory's marker and every file the command reads: the `nats` credentials, TLS CA, certificate and key and the inventory file always, the account files and the kubeconfig only in `run` with events on, since only the event connections and the watcher read them. Any problem refuses the start with one error that names every broken system, before a database opens.
- **Isolation.** After the start check, a system whose NATS is unreachable, whose connection nats.go closed for good, or whose setup fails (an events connection, the Kubernetes client) skips only its own epochs; the others, the API and readiness go on. A failed setup sets the status to `connection_closed` with the error, is logged as an error, and is tried again with the redial backoff (2 seconds doubling to a minute), so a file that is fixed later is picked up. A system whose store fails while running (reading its known servers, recording its start) still stops the process, as a store that fails to open fails the start.
- **Collection status.** Each system reports `CollectionStatus` (`starting`, `collecting`, `not_connected`, `connection_closed`, the newest connection error, when the state began, the start of the newest stored epoch) through `api.Systems`. The collector connection's handlers feed it, chained after the capture's; a connection closed for good stays `connection_closed` while it is dialled again, until it connects.
- **Checks.** One `checks.Set` serves every system; each engine leaves out its own system's nats-lens connections (`System.ToolingNames()`, `natslens-<id>` by default in a list) and logs the loopback guard once for its system. A union of every system's names would also have hidden another system's connection names in a system, and the provider's tooling flag on connections is per system too.
- **Resources.** DuckDB `memory_limit` is the system's own `duckdb_memory_limit` or its share of the budget; `threads` is `max(2, GOMAXPROCS / number of systems)` per database, so that N databases do not each plan for every core. With more systems than half the CPUs the floor of 2 adds up to more threads than CPUs.
- **Shutdown.** One deadline, `shutdown_grace + events.flush_interval` from the signal, for all systems; they stop in parallel, each in the order of [Shutdown](#shutdown) and with its own last `state.json`; the stores close after the API's shutdown returned, in parallel, by the same bound as the last `state.json`; a close still running then is abandoned and the next start replays the write-ahead log.
- **Commands for one database.** `natslens once` collects every system or the one `-system` names; `natslens sql` and `natslens checks run` read the database of `-system` from `-config`, required when the file lists several systems, or `-data-dir`.
- **Kubernetes.** Two systems in one Kubernetes cluster each run a watcher, so node changes are recorded in both databases.

## Collection

Every `interval` (default 30s) the collector takes one snapshot, called an epoch:

1. Discover servers with `$SYS.REQ.SERVER.PING.STATSZ`.
2. Request from every server, through `$SYS.REQ.SERVER.PING.<X>` with a collect window: VARZ, CONNZ (with subscription counts), ROUTEZ, GATEWAYZ, LEAFZ, ACCOUNTZ, JSZ (accounts, streams, consumers, config, raft), HEALTHZ (details), RAFTZ, IPQUEUESZ. Account statistics come from `$SYS.REQ.ACCOUNT.PING.STATZ`.
3. Keep every server's view. Never collapse to one server: an R1 asset (a KV watcher's ordered consumer) appears only on its host, and disagreement between servers is itself a finding.
4. Write the epoch's rows in one transaction. An epoch with missing servers or endpoints is stored and marked partial; `epoch_endpoints` records the outcome of every request per server.

Expected servers are tracked by name, because a server ID changes on every restart. An epoch expects every name seen within `server_forget_after` (default 1h), every server that answered, and every route peer and JetStream meta peer they report.

Discovery stops early once every expected name has answered; otherwise it waits the full collect window (`timeout`, default 3s). Detail requests stop early once every discovered server ID has answered. Replies are counted per distinct server, never by number.

RAFTZ without a filter returns only the system account's groups (the meta group). Stream and consumer groups are listed under the asset's account, so the collector sends one RAFTZ per JetStream account seen in JSZ, recorded as endpoint `RAFTZ:<account>`. The requests for the accounts of the previous epoch go out with the detail requests; only an account seen for the first time needs a second pass. An account with no streams left is dropped from the requests in the first epoch in which every server answered JSZ without it, so it is never recorded missing. An epoch therefore takes at most three windows, and `timeout` must be under a third of `interval`. RAFTZ names leaders by raft peer ID; `raft_stats.leader_name` resolves the name from the peer IDs reported in the same epoch.

Followers report `num_pending`, `num_waiting` and `push_bound` as 0 or false for a consumer (nats-server 2.15 tracks the backlog, pull requests and interest on the leader only), so follower rows store NULL for them and the view `consumer_leader_stats` holds the leader's row. Account JetStream usage in JSZ is the account total across replicas, not one server's share.

Account names: the readable name of an operator-mode account (its JWT name tag) comes from JSZ account details and account STATZ, from the name tag CONNZ gives with each connection, and from the name tag of connect and disconnect events. An account that ACCOUNTZ lists and none of these name is looked up with an ACCOUNTZ request filtered to that account (`$SYS.REQ.SERVER.PING.ACCOUNTZ` with `{"account": <id>}`), in the window of the second RAFTZ pass, so the epoch takes no longer; the name tag, else the decoded JWT's name, is kept and the JWT is not. A lookup that finds no name is tried again after 10 minutes, and a learnt name is looked up again after 24 hours; at most 32 accounts are looked up in one epoch, accounts without a name before rechecks and the ones asked longest ago first, and at most 8 at a time. A config-mode account, whose identifier is its name, is never looked up. A name, once known, is never replaced by the identifier. Only connect and disconnect events name an account: in API audits and snapshot or restore advisories the client's name tag is the user's. A name from the snapshots (JSZ, STATZ, CONNZ, ACCOUNTZ) replaces one learnt from events; an event fills in a name only for an account that has none (proposed, not yet confirmed).

`raft_stats.wal_error` cannot carry the error text: nats-server 2.15 encodes the Go error value as JSON, which gives `{}` for a plain error (a file error keeps only `Op` and `Path`). A non-null value means the group stopped on a critical write error; the text is in the server log.

`natslens run` retries a failed first connect and skips epochs while the collector connection has no server; the API, the event capture and the Kubernetes watcher keep running, and the first connect after failed attempts is recorded as `collector.reconnected`. `natslens once` fails at once.

## Events

Events are what a snapshot misses: short leader changes, a consumer deleted between two polls, who deleted it. Every event is one row: time, source, kind, type, server, account, stream, consumer, client name, client IP, a short summary and the raw JSON.

### Subjects

The system account connection subscribes to:

- `$SYS.ACCOUNT.*.CONNECT` and `$SYS.ACCOUNT.*.DISCONNECT` (with the reason, including slow consumer);
- `$SYS.SERVER.*.LAMEDUCK`, `$SYS.SERVER.*.SHUTDOWN` and `$SYS.SERVER.*.CLIENT.AUTH.ERR`;
- `$SYS.ACCOUNT.CLIENT.AUTH.ERR`;
- `$JS.EVENT.ADVISORY.>`: the JetStream advisories that concern no account (meta leader elected, server out of storage, server removed, API limit reached) are published in the system account;
- `$SYS.ACCOUNT.*.SERVER.CONNS` only with `events.account_connections`, because every server sends it for every account every 30 seconds.

Each JetStream account listed in `accounts` has its own connection, with its own credentials, subscribed to `$JS.EVENT.ADVISORY.>`, `$JS.EVENT.METRIC.>` and `$SYS.ACCOUNT.CLIENT.AUTH.ERR` (account-scoped authentication errors, sent with auth callout, are published in that account). This covers stream created, updated and deleted, consumer created and deleted (nats-server 2.15 sends no advisory for a consumer update), leader elected, quorum lost, max deliveries, NAK, terminated, pause and sampled acks. The account connection also requests `$SYS.REQ.USER.INFO` to learn the account name the servers use for its user.

Cluster advisories (stream and consumer leader elected, quorum lost) arrive in both the asset's account and the system account with the same payload ID; the capture stores the first copy. An account-scoped authentication error is stored once with the server-wide event of the same failure.

### Attribution

JetStream events from an account connection carry the account name the server reports for the connection's user (`$SYS.REQ.USER.INFO`), the name advisories and connection events use. The configured label stands in until it is known, so events stored before that keep the label. A user refused that request, or refused the subscription to its reply inbox, keeps the label for the rest of the process, with one Info line and no stored event; the request is made at most five times per label per process. On other connections a refused inbox stays an error.

Stream and consumer action advisories do not name a client. The API audit advisory of the same call does (`$JS.EVENT.ADVISORY.API`), so audits are always read. An audit and an action match only when received within 2 seconds of each other, in either order, and an audit older than the previous action on the same asset never matches: an idempotent create sends an audit but no advisory, and must not name its client on a later create by someone else. Audits are stored as events only with `events.api_audit`. An action without a matching audit (an inactive consumer removed by the server) has no client. nats-server 2.15 sends no advisory for a consumer update, only for create.

A server that enters lame duck mode sends a lame duck event and, in tests, no shutdown event when it then exits; a plain stop sends a shutdown event. Lame duck and shutdown payloads have no type string, so `events.type` is null for them.

### Connections and writing

Event capture uses its own system account connection (`<name>-events`), so that a burst of events never delays the replies of an epoch. The event connections retry a failed first connect instead of stopping the collector. nats.go closes a connection for good after repeated authorization errors; such a connection is dialled again, waiting 2 seconds doubling to a minute. A collector connection without a server for 30 seconds is recorded as `collector.not_connected`, again every 10 minutes.

Short-lived connections that close themselves are collapsed (`events.churn`, on by default): a connect is held for `pair_within` (10s); when its disconnect (same server and client ID) arrives in that time with reason "Client Closed", neither is stored, and one `client.churn` event per client name, client host, account and `window` (1m) counts the pairs, so a loopback CLI's summary is still tooling, for example "jetstream-controller connected and disconnected 28 times in 1 min", with the counts per server in `raw`. A connect not closed in time is stored as it is, so connect events reach the database up to `pair_within` late; a disconnect for any other reason is stored with its connect. `client-004` counts the connects of `client.churn` events. On one development cluster, NACK's controller made about 280 of 411 events in 20 minutes this way (proposed, not yet confirmed).

Events are written in batches, every `events.flush_interval` (1s) or `events.batch_size` (500) events. After a failed write the next attempt waits for the flush ticker, doubling up to 30 seconds. Invalid UTF-8 in an event is replaced, as the driver cannot bind it and one bad client name would otherwise fail every batch.

Sources are `sys` and `jetstream` (NATS), `k8s` (the Kubernetes watcher), `finding` (a finding transition) and `collector` (the collector's own records: gap, started, connection lost and reconnected, not connected, messages dropped). A capture gap (a collector restart) is recorded as `collector.gap`, so that the UI never shows silence as health. User JWTs are removed from `raw`: any key named `jwt`, at any depth.

Events follow the same retention as epochs, by receive time.

### Shutdown

Shutdown is bounded by `shutdown_grace` plus `events.flush_interval` from the signal, and that sum plus a margin to exit is the only figure the stop timeout needs:

- the last epoch write gets `shutdown_grace`;
- in parallel the event connections drain together for at most one flush interval, and the last events are written in batches of 500 until the deadline, with a `collector.messages_dropped` event counting any that do not fit;
- an interrupted archive export is waited for until the same deadline;
- the last `state.json` write may take 2 seconds past it;
- the API's shutdown runs in parallel from the signal: it waits 5 seconds for calls, then closes their connections, and returns once every call returned; the store closes after that.

So the stop timeout must exceed `shutdown_grace + events.flush_interval + 2s` or 5 seconds, whichever is longer, plus a margin to exit.

## Storage and archive

### Database

The DuckDB file `natslens.duckdb` lives in the system's data directory (`data_dir` for a file without `systems`, `<data_dir>/<id>` by default in a list). Tables follow one pattern:

- identity tables hold one row per thing with the first and last epoch it was seen: `servers`, `accounts`, `streams`, `consumers`, `raft_groups`;
- configuration tables hold one row per server per run of consecutive epochs with the same configuration (`first_epoch`, `last_epoch`, `config_hash`): `stream_config`, `consumer_config`. `config_hash` covers the configuration only; a new creation time also starts a new run, so a recreated asset counts as changed while replicas built at different times show the same hash, and the disagreement views list a creation time apart from the configuration (proposed, not yet confirmed). The first epoch after upgrading to this rule starts new configuration rows for every asset, because the old hashes included `created`; a server that misses that epoch keeps its old hash and shows "config differs" until it answers again. A run ends when the server reports nothing for the asset in an epoch, so it never covers an epoch without a report. Agreement checks read only the rows current at the epoch (`first_epoch <= E AND last_epoch >= E`);
- stats tables hold one row per server per epoch and always carry `epoch` and `server_id`: `server_stats`, `routes`, `gateways`, `leafnodes`, `ipqueues`, `health`, `connections`, `account_stats`, `stream_stats`, `stream_replicas`, `consumer_stats`, `consumer_replicas`, `raft_stats`.

The other tables are `epochs`, `epoch_endpoints`, `events`, `findings`, `finding_transitions`, `check_runs`, `mutes`, `inventory`, `archive_hours` and `migrations`, and the view `consumer_leader_stats`.

Unique indexes cover `epoch_endpoints`, `server_stats`, `health` and the two configuration tables. The per-entity stats tables have none: the writer keeps one row per entity, server and epoch, and an index there would cost memory and insert time every epoch.

Migrations are numbered SQL files, applied once each. Never edit an applied migration; add a new one. The `migrations` table stores each file's SHA-256 and start fails on a mismatch.

Retention is 14 days in DuckDB by default (`retention`), for epochs and events.

DuckDB's `memory_limit` is set at open: `duckdb_memory_limit`, or 60% of the cgroup memory limit, or 1 GiB. DuckDB's own default is 80% of the node's memory, not the pod's.

`natslens sql` runs a query against a database file that no collector holds.

### Archive

The archive is a directory of hourly Parquet files, reached through a `Destination` interface (stage, put, list, fetch); a bucket destination is left for later.

- Each table's rows for an hour go to `archive_dir/<table>/date=YYYY-MM-DD/hour=HH.<export>.parquet`. Events are filed by receive time, epoch tables by the epoch's start time. Identity and configuration rows are exported in every hour their epoch range overlaps, as they are at export time; a rebuild keeps the copy with the highest `last_epoch`. `inventory` is not archived.
- An hour closes when it ended two intervals ago, two event flush intervals ago, and at least a minute ago. Only hours wholly inside the database retention are exported, because the sweep may already have removed part of an older hour.
- `archive_hours` records each exported hour with its epoch and event counts. A closed hour whose counts change later (an event written late) is exported again. The open hour is exported provisionally every `archive_provisional_every` (5m) and finally once closed, so a lost database loses at most that much. Missed hours are caught up after downtime.
- Each export of an hour writes files named by its export id (the export's start time), then the manifest `manifests/date=YYYY-MM-DD/hour=HH.json` listing exactly those files with their row counts, then removes the hour's files the manifest does not list that belong to an earlier export (a lower id, or none) and are at least 10 minutes old. A crash before the manifest leaves the previous manifest and its files intact. An export id never repeats one already present for the hour. Files and their directories are synced before and after the rename.
- The archive directory expects one writer at a time. When two natslens processes export the same hour at once (a rollout overlap), the age rule keeps each one's fresh files until its manifest lists them, so the overlap leaves unused files but never a manifest that names a removed file.
- The export runs on its own goroutine every interval, at most six hours per tick, so catching up never delays an epoch.
- Every export tick writes `state.json` with the highest epoch number, event ID and mute ID; shutdown writes it once more after the last events. A rebuild continues numbering after the higher of those and the loaded rows; only numbers handed out after the last tick before a database was lost are used again.
- `findings` and `mutes` are written whole with every hour; a rebuild keeps each finding's copy with the newest `updated_epoch` and each mute's newest copy (the lifted one once it was lifted). `archive_hours.mute_changes` records mutes plus lifted mutes at export; when the current number differs, the newest hour with epochs or events is exported again (provisionally while it is open), so the archive's newest hour always holds the current mutes and older hours are not rewritten. A mute changed while the database has no epoch or event inside the retention window is archived with the next hour that has one. Transitions and check runs are archived and swept with their epoch.
- Archive retention: after a successful export, at most once an hour, every archived hour that ended more than `archive_retention` (default 90d) ago is removed: its data files of every export first, then its manifest, so an interrupted prune leaves a manifest the next one finds again, then the day directories left empty. `0` keeps every hour. The setting must be `0` or at least `retention`, so a rebuild never misses an hour. Apart from that and the superseded files of an hour exported again, the tool never deletes archive files.
- Start warns when an `archive_dir` set in the file is inside the system's data directory, as one lost volume would then lose both. The default, `<data dir>/archive`, is not warned about: leaving `archive_dir` unset is the documented choice of one volume (proposed, not yet confirmed).

### Rebuild

A rebuild runs when the database file is missing or holds no epoch and no event, and the archive has complete hours inside the retention window. It loads only hours with a manifest, exactly the files it lists, and stops when a file's row count differs from the manifest (removing that manifest rebuilds without the hour). It writes `natslens.duckdb.rebuild` and renames it into place only when the load succeeded, so a failed rebuild is tried again on the next start. A table whose rows are updated in place keeps one copy per key: the identity and configuration tables the one with the highest `last_epoch`, findings the newest `updated_epoch`, mutes the lifted copy, and events the copy received last (a Kubernetes Event moved to a later hour). Rows are matched to columns by name: a column added by a later migration is null in rows loaded from older files, so a migration that adds a NOT NULL column to an archived table must give it a default.

## Checks

### Files and engine

A check is a file `checks/<code>.sql`: a YAML header inside a `/* ... */` comment at the top, so the file stays one runnable query, and one SELECT. Header keys: `code`, `name`, `entity`, `category`, `severity`, `severity_rule` (in words, when a row gets another severity), `kind`, `lookback`, `thresholds` (each with `name`, `default`, `unit`, `description`), `description`, `remediation`, `related_config`, `value_name` and `value_basis` (what the `value` column measures, with its unit, and how; every check that returns a value sets both), and `suppressed_by` (checks of the same entity type whose finding for an entity makes this one's redundant: stream-009 is quiet for a stream while stream-001 fires for it).

The query returns one row per failing entity with `entity_key`, `detail` and optionally `severity` (else the header's), `value`, `cause_event_id`, `related_config` (a JSON object of setting to value), `related_source` and `condition_since` (when the condition began, where the check knows it: `server-004` the process start, `stream-008` and `consumer-009` the newer creation time). A wrong column, key shape, severity or a repeated entity makes the run an error. It reads its inputs with DuckDB's `getvariable()`, set on the connection before it runs: `epoch`, `epoch_time` (the epoch's start), `prev_epoch`, and for a check with a lookback `lookback`, `lookback_text`, `window_start` and `window_epoch`, plus its thresholds (a duration threshold is an INTERVAL). Load fails for a variable that is neither reserved nor declared, and for a declared threshold the query never reads. Threshold values are plain finite decimals.

The prelude defines macros on the checks' connection: `ns_text(ns)` writes nanoseconds as `7d`, `30s` or `1.5ms`; `restarting(lo, hi)`, `recovering(lo, hi)`, `planned_down(e)` and `not_ready(e)` describe servers that cannot be taken at their word (see Restarts); `gap_text(names, whys)` names the not-ready servers that may hold a missing asset.

After every stored epoch, outside the epoch's transaction, the engine runs every check on one connection, each within `checks.timeout`, all within a budget of half an interval; a check that would start after the budget is an error run. The order of the checks rotates by one each epoch, so a budget overrun does not leave out the same checks every time. Measured on 7 days of synthetic data (20,000 epochs, 3 servers, 30 streams, 60 consumers, 90 raft groups, 500,000 events) all checks together took about 170 ms.

The engine then writes, in one transaction, a `check_runs` row per check (`ok`, `error` or `disabled`), so the status timeline can tell no data from OK, and the changes to `findings` and `finding_transitions`: a new failing entity starts `firing`, a severity change is `changed`, an entity no longer returned is `resolved`. Transitions carry the epoch's start time, not the wall clock. A finding is flapping when it went through more than three cycles (resolved transitions) in the hour before the epoch; `transitions_last_hour` counts all transitions. Every transition is also written as an event (source `finding`, kind `finding.firing`, `finding.changed` or `finding.resolved`, timed at the epoch's start, with the entity's account, stream, consumer, server or client in its columns and the transition in `raw`), in the same transaction, so that the event list and the timeline show findings with the NATS events.

The writes are one statement per batch of 5000 rows, the batch passed as one JSON parameter; 3000 new findings take about 50 ms. A failed write rolls back whole, so the epoch can be applied again. A value that is not a finite number is stored as NULL and logged. If the store is handed the same entity twice, it keeps the first row.

`system-001 Check failed to run` is built into the engine: one finding for the system while any check failed in the epoch, its detail listing them, critical once the same check has failed ten epochs in a row. A failed check leaves its own findings as they were, since nothing is known. A disabled check does not run, has a `disabled` run, and its findings resolve; so do the findings of a check that no longer exists in the binary.

Thresholds and lookbacks are overridden per check in the configuration file; an unknown code, threshold or value stops the start. Mutes apply when findings are read, not when they are written.

The retention sweep keeps the current episode of every finding it keeps (from its latest firing transition on), so a finding firing since a swept epoch still says when it started; resolved findings are swept once their last change is older than the oldest kept epoch. After a rebuild a finding's episode starts at its oldest loaded transition.

`natslens checks run` opens a database read-only, needs the binary's schema version, and stores nothing; it is for trying a check, not for catching up missed epochs.

### Codes

Codes are `<entity>-<NNN>`, prefixed by the entity a finding is about, not by a topic: `server-`, `stream-`, `consumer-`, `client-`, and `system-` for the meta group and the engine. There are 49 checks: 48 SQL files and the built-in `system-001`. The inventory checks are `stream-011`, `stream-012`, `consumer-010` and `consumer-011` (a missing KV bucket or object store is its stream, `KV_<bucket>`, `OBJ_<bucket>`). Client findings are keyed by connection name across connections (`(unnamed)` without one), so a mute or a finding survives reconnects; the detail names the server and cid.

The set covers:

- Health: server not reporting, healthz failing, JetStream not ready, meta leader missing or changing often, stream or consumer quorum lost, replica offline, replica lagging, raft group catching up for too long, raft write error.
- Agreement between servers: stream or consumer configuration differs, creation time differs, peer set differs, an asset missing on an assigned server (`stream-010`, `consumer-012`).
- Expected inventory: a stream, consumer, KV bucket or object store in the inventory file is missing, or has configuration that differs from it.
- Disappeared assets without an inventory: a stream (`stream-014`) or durable consumer (`consumer-013`) reported within the lookback that no server reports now.
- Consumers: backlog growing, ack pending near max, redeliveries high, max deliveries reached (from events), no delivery while messages wait, push consumer without a subscriber.
- Storage: server store above a share of its limit, store full soon, stream near its message or byte limit, stream reaching its byte limit soon, stream without any limit.
- Clients: slow consumer disconnects, stalled clients, high pending bytes, high round trip time, connection churn, stale connections, a client reconnecting repeatedly.
- Change: server restarted, version differs from the majority, configuration reloaded, stream configuration changed (from events).

What the set merges or measures differently:

- A raft group without a leader is `stream-001` and `consumer-001` (quorum lost covers no leader) and `system-002` for the meta group; a separate raft check would report the same group twice.
- nats-server reports no memory limit, so `server-008` compares a server's memory with the median of the others.
- Slow consumers, stalls, churn and stale connections count a counter's growth over a lookback (15 or 10 minutes) per server process, not since the previous epoch, so one event does not fire and resolve within a minute.
- Replica lag is the leader's raft lag (operations), not a difference in last sequence between replicas, which the servers report at slightly different moments.
- Consumer backlog growth needs the backlog to grow in both halves of the window, so one burst does not fire it; fill rate is the backlog's growth plus deliveries (redeliveries included), drain rate the deliveries.
- Growth forecasts (`server-014`, `stream-006`) take the smaller of the two halves' slopes and need both positive.
- Severity at a stream limit follows retention: work queue and interest streams with discard old lose unprocessed work (warning, critical); limits streams with discard old are ring buffers (info, no forecast).
- Push consumers without a subscriber are checked for durables only: the server removes an ephemeral push consumer (a KV watcher) itself.
- `consumer-003` is critical only when the consumer sat at `max_ack_pending` with messages waiting for `min_epochs` epochs and neither its ack floor nor its delivered sequence moved for `ack_wait`; a pool at the limit that acknowledges is "pool saturated", a warning.
- `consumer-013` and `stream-014` leave out `JS_` consumers, ephemeral ones, durables with an inactive threshold (removed by design) and assets in the inventory, which `consumer-010` and `stream-011` report.
- `server-011` (connection churn) fires when the thresholds hold in 2 epochs in a row and, once firing, resolves only below 0.8 of both thresholds, read from the finding's state at the previous epoch, so NACK's steady churn around the threshold does not flap; its detail names the three clients with the most connects (client.connect and client.churn events) in the lookback. `client-001` (round trip time) needs one client (server, pod (the name after "@") and address, else server and cid) slow in 2 epochs in a row and keys by service, the connection name before "@", so a mute covers every pod. `stream-005` leaves out a stream the inventory declares unbounded with `max_age: 0` and `max_bytes: -1` both given, and a KV bucket with `allow_msg_ttl` that the inventory lists, whose writers then bound its keys; an unlisted bucket with `allow_msg_ttl` stays at info and its detail says that nats-lens cannot see whether writers set TTLs (proposed, not yet confirmed).
- The remediation of a missing durable warns against restarting the owning service (a `deliver_policy: all` recreate replays the stream) and links the playbook "A durable consumer is missing".
- `stream-008` and `consumer-009` (creation time differs) are info while the replicas agree on their contents (first and last sequence, message count) or positions (delivered and ack floor): a replica rebuilt at another time holds the same state, and no action is needed. They are a warning only when the contents or positions differ as well, in this epoch and the previous one, because replicas report at slightly different moments. Every replica is compared with the leader, current or not, as a replica that kept an old copy is often listed as not current: one behind the leader by at most `position_tolerance` (100) messages, or `position_tolerance_ratio` (1%) of the leader's value when that is more, agrees, so a busy consumer's trailing follower stays info; one ahead of the leader by more than `position_ahead_tolerance` (10), or `position_ahead_tolerance_ratio` (0.1%) of the leader's value when that is more, as replies are read a moment apart, never agrees, as a follower is not otherwise ahead; one behind by more than the tolerance in two epochs warns even while it catches up, but only in these checks, where the creation times differ; a restarting replica catching up is left out by the restart window (proposed, not yet confirmed).

### Restarts

A rolling restart must raise nothing above info except replicas offline or behind while they are (`TestRollingRestartRaisesNothing`). The prelude separates three states:

- Restarting (`restarting()`): the server's process started within `checks.restart_window` (10m) and replaced an earlier process of the same name. The restart is dated by the process start, not by when natslens first saw the server ID, and needs an earlier process, so a server is not restarting just because natslens or a new cluster started. Only a process restart makes a server restarting.
- Recovering (`recovering()`): HEALTHZ reports a JetStream error, or its raft groups catch up. A server in trouble shows the same, so these never hide its reports; they only add to a restarting server's reasons in the detail text.
- Not ready (`not_ready()`): may hold an asset without reporting it: missing, restarting, JetStream not ready, or answering with JetStream disabled or without JSZ. It is planned when the server is restarting or announced its shutdown, and ready again when the only reason is a restart and it answered JSZ in the epoch. `planned_down()` lists the missing servers that announced lame duck or shutdown within `restart_window`.

The rules built on them:

- Quorum (`stream-001`, `consumer-001`) is judged from the leader most reporters name, and must be lost for `min_epochs` (2) epochs. A restarting server's report is left out only while the healthy servers (neither restarting nor recovering, nor reporting the asset without a leader) can form a majority of the replicas, and never a report that says leaderless or, as leader, fewer than a quorum online. A leader the others name that does not report, with fewer than a quorum of replicas reporting, is a loss. `stream-009` uses the same majority rule and two epochs, and also leaves out a restarting or recovering server's view that is a strict subset of the majority's.
- A server missing after it announced lame duck or shutdown is `server-001` at info for `restart_window`; a crash sends no announcement and is a warning. With half the servers missing `server-001` is critical and its detail counts the streams and consumers left without a quorum; the per-asset findings stay, because `suppressed_by` needs the same entity type and they say when each asset recovers.
- `stream-002` is info while the offline replica's server restarts or announced its shutdown.
- A missing asset (`stream-011`, `consumer-010`) or a disappeared one (`stream-014`, `consumer-013`) is weighed against the servers that held it when last reported (any server when it never was), counting a holder that is ready again as ready: critical with a delete event since its last report (the detail names the client), when no holder is not ready, or when the ready holders that lack it are a quorum of its holders; otherwise info when every not-ready holder is restarting or announced its shutdown, else warning. All four name the last holder and time. For `consumer-013` its stream's delete counts, at warning when the stream was created again; an asset stays a candidate past the lookback while a server that held it has not answered since, and the detail names any collector gap between the last report and now.
- `system-003` allows 4 meta leader changes in 15m and does not count the first change away from a server within two collect intervals (the median gap between epochs) after its announcement or after its replaced process last answered; a later hand-over, as with a lame duck period longer than about a minute at a 30-second interval, is counted.
- Tooling connections are left out of the client checks (`client-001` to `client-004`) and of `server-011`'s client list unless `checks.include_tooling` is set: nats-lens's own connections (`nats.name`, `<name>-events`, `<name>-<account>`) and clients connected from 127.0.0.1 or ::1, which is how a port-forwarded CLI appears. The loopback rule is `checks.loopback_is_tooling` (on by default) with a guard, as a sidecar can make every inbound connection look local: the share of loopback clients among the connections that are not nats-lens's own is taken over the last 10 epochs, the rule stops when it goes above 60% and returns only when it falls below 40%, and an epoch with fewer than 3 such connections keeps the previous decision, so a lone CLI stays tooling unless the guard is already off and a thin epoch does not reset the guard. After each epoch's checks the engine stores the decision in `epochs.loopback_tooling`, made from the latest decision stored in the trailing window (an epoch whose checks failed has none and does not reset the guard) and the trailing share, so an epoch is judged the same whenever it is read; an epoch without one (older data) counts as on, and the column is archived with the epoch. Each epoch's rows are judged by that epoch's decision; natslens logs it once when the guard drops the rule. The prelude macros `tooling_at(name, ip, epoch)` and `tooling(name, ip)` (the epoch checked, for events) decide, from the reserved variables `exclude_tooling`, `tooling_names`, `loopback_setting` and `loopback_now` (the decision being made) and the stored decisions. `ListConnections` leaves tooling connections out unless `include_tooling` is set and counts them in `tooling_hidden`. `server-011`'s count comes from the servers' connection counters, so it still includes them. The API marks such connections `tooling` (proposed, not yet confirmed).
- `client-004` counts reconnects, not connects: the smaller of the `client.disconnect` events whose reason is neither "Client Closed" nor "Server Shutdown" and the `client.connect` events of the same name in the window, paired in either order and from any host, so instances sharing a name are merged. A service scaling out (one name, many hosts, no disconnects) does not fire it (proposed, not yet confirmed).

Checks do not read Kubernetes events yet; the restart rules rely on process start times and lame duck and shutdown announcements.

### Inventory and mutes

The inventory file lists per account the streams (with their durable consumers), KV buckets and object stores that must exist, and the settings that must have a given value; a setting left out is not compared. Byte sizes take binary suffixes only (KiB to TiB), because NATS tools read `GB` differently. It is loaded into `inventory` on every start.

An operator can mute a check, or a check on one entity, with a reason and an expiry. Muted findings still record but are shown apart and do not alert. A mute without an expiry is not allowed.

## API

Connect service `natslens.v1.LensService`, protobuf in `proto/natslens/v1/`, served under `/api/` by `natslens run`. Connect handlers also speak gRPC and gRPC-Web; the server enables HTTP/2 without TLS. TypeScript is generated with `protoc-gen-es` v2 only: connect-es v2 reads the service from those files and needs no plugin of its own.

### RPCs

- `GetLensInfo`: the identity mode (verified IAP user, or a name in the request), version, cluster, collect interval, retention, and whether the data is live or the demo. The UI reads the identity mode to decide whether the mute dialog asks for a name. It is its own RPC because it does not depend on a range.
- `GetOverview`: cluster name, freshness, servers, the finding tally (critical, warning, info, muted, flapping, resolved in 24 hours), the worst unmuted finding, number tiles with their series, and time windows to shade.
- `ListServers`, `GetServer`; `ListStreams`, `GetStream`; `ListConsumers`, `GetConsumer`; `ListConnections`.
- `ListFindings`, `GetFinding` (one finding with its transitions); `ListEvents`, `GetEvent` (one event with its raw payload, which `ListEvents` leaves out).
- `GetStatusTimeline`: per check, per time bucket, the worst state for an entity or for the whole system.
- `ListMetrics` (the metric IDs `GetTimeSeries` serves, with entity type, unit and description) and `GetTimeSeries`: one metric for one entity over a range, downsampled to at most N points.
- `MuteFinding`, `UnmuteFinding`, `ListMutes`. The mute calls are the only writes, and they write only to the tool's own database.
- `ListSystems`: the systems in configuration order and the default one, each with a summary for the Fleet view (below); `ListAccounts`: a system's accounts in the range's newest epoch (in a range without an epoch, the accounts seen up to its end, every count unset), with connections (the sum of each server's STATZ share) and streams and consumers (counted from the JSZ detail; without detail a reported zero is zero and any other count unset, as the servers count each replica). `ListKeyValueStores` and `ListObjectStores` are declared and return empty lists for now.

A system's summary reads only data already collected, never NATS: the collection state, its error and since when; the freshness of the range's newest epoch; from that epoch the servers expected and answering (`epochs`), the distinct non-empty cluster names of its servers (zero for servers without a cluster), the streams in `stream_stats` and the sum of `server_stats.connections` (each unset without an epoch in the range); the status, worst unmuted finding and tally as the overview reads them (the status is no data without an epoch); and a strip, the worst cell per bucket of the status timeline's per-check rows with every check included, so a bucket is no data only when no check ran ok in it. The strip has `strip_buckets` buckets (48 by default, at most 500), each the range divided by that number, rounded up to whole seconds. The handler asks the providers in parallel, each within 5 seconds in its own goroutine, so a provider that ignores its context does not hold up the response, and one summary per system runs at a time: while the previous one still runs, a call gets the timeout error at once and starts nothing; a provider that fails, panics or runs out of time is listed with its reference and a generic `error`, its own text going only to the log. The runtime's collection status, when the system has one, replaces the provider's collection state; its error and since are kept only while the state is not connected or connection closed. Without it, the store provider reads the stored events of the collector's own connection (`raw.connection` is `collector`): the newest `collector.connection_closed` or `collector.not_connected` after both the newest epoch and the newest `collector.reconnected` of that connection gives the state and the error, the oldest such event the since; otherwise a provider with a collector in the process is collecting once an epoch is stored and starting before, and one without a collector reports no state. The event connections' losses never set the state, as they do not stop the epochs (proposed, not yet confirmed).

Every request except `ListSystems` and `ListMetrics` names its system in `system`. The handler resolves it before anything else: empty is the default system, the first configured; an unknown ID is invalid_argument naming the known IDs, and no provider is called. Each system has its own provider, which never reads the field. `GetLensInfo` returns the system's ID and name.

A read-only SQL endpoint and an MCP server for agents come after the first release.

### Ranges and identities

Reads over time take a `TimeRange`, never an epoch. An unset end means now (live mode); an unset start means 24 hours before the end; a range spans at most 400 days. Lists of entities and the freshness describe the newest epoch inside the range; growth and series cover the whole range.

Every ID is a string: event IDs, mute IDs, finding IDs (`<check code>:<entity key>`), timeline row keys (the check code, plus `:<entity key>` for a row of one entity), lane item IDs (the event ID, or one derived from source, kind and time for a Kubernetes item that is not stored), and connection keys (`server/cid`). Database IDs given as strings must be positive decimal numbers in canonical form.

Entity keys: server name, account, `account/stream`, `account/stream/consumer`, `server/cid`, client connection name; the system entity has none. The account is the one the servers report (the account ID in operator mode); every message of a response that holds an account (entity references, events, connections) also carries its readable `account_name` when known, filled by the handler from the `accounts` table, and the UI shows the name with the ID as its tooltip; event filters by stream or consumer also match the readable account name, because events can carry the configured name. A filter with an unknown entity type or a key of the wrong shape is invalid_argument, never ignored. A connection filter on events matches `client.id` in the payload.

### Status and findings

Status values combine severity with muted and no data in one enum (`Status`). Nothing defaults to OK. A server's, stream's, consumer's or tile's status is the worst firing finding on it (a stream's includes its consumers', muted when all are muted); OK when none fires and every check of its entity type ran ok (or was disabled) in the range's newest epoch; no data otherwise: when such a check failed or did not run, for a server that did not answer, for an expected consumer without a finding, and for a tile without a value. Tiles map to checks: servers to the server health and system checks, connections to the client checks and connection churn and staleness, slow consumers to `client-003`, `server-009` and `server-010`, JetStream storage to `server-007`, `server-014`, `stream-004` and `stream-006`, consumers to every consumer check.

A number that was not observed is unset, never zero: tile values, the counts of servers, connections, streams and consumers and of their per-server views, and a consumer's `num_pending` and `num_waiting` without a leader row. A replica's `lag` is unset for the leader, which does not list itself. `consumers_expected` counts the inventory's consumers of a stream the inventory names, and a consumer the inventory expects that no server reports is listed with `present` false and no numbers.

A stream's `bytes_per_day` is the Theil-Sen slope (the median of the slopes between every pair of points) of the leader's bytes over the range, reduced to at most 200 bucket medians, and is unset when the observations span less than 6 hours, so one write is never extrapolated to a day. It may be negative: a KV bucket whose entries expire by TTL faster than they are written shrinks for real, and a slope over 6 hours that one purge cannot move is real (proposed, not yet confirmed). The growth forecasts of the checks (`server-014`, `stream-006`) keep their own rule.

A finding's `since` is when nats-lens first saw it; `condition_since` is when the condition began, where the check knows it, and is unset otherwise, so a finding raised at the first epoch after nats-lens started does not read as a fresh problem. `GetLensInfo.history_start` is the start of the oldest stored epoch, and the finding page says when a finding was first seen at the start of that history (proposed, not yet confirmed).

A finding's state is flapping when the engine marks it flapping, changed when it fires and its last transition changed the severity, else firing or resolved. A range that ends more than one interval before now is a past range: its findings are rebuilt from `finding_transitions` as they stood at its end (state, severity, detail, value and cause from the newest transition before the end, since from the start of that episode, flapping from the hour before, mutes in force at the end), so a finding that fired, resolved and fired again keeps its earlier episode, and the statuses for that range use those findings. A live range that ages past that point switches once from the current state to the state at its end. A current range reads the findings table and the mutes in force now. The tally's resolved count is the resolve transitions of the 24 hours before the range's end. The value carries the header's `value_name` and `value_basis`; related configuration is the finding's own map with the header's `related_config` keys highlighted; the cause is the stored event the check named.

A finding event's summary is "<entity>: <detail>", and for a resolve "resolved: <entity>. Last failing result: <detail>". A finding event carries the transition, the finding as it is now and the check's name in its type; an event a finding names as its cause lists that finding as raised.

### Timeline and series

The status timeline takes its findings from the transitions in the range and the last one before it, and aggregates per check and bucket in SQL: one row per check that ran in the range (with an entity, one per finding of the entity, and with `include_ok` one for each check of the entity's type). A cell is the worst state of the epochs in the bucket whose run was ok: a firing finding's severity; muted when the findings firing then were under a mute in force at that run; ok when none fired; no data when the check did not run ok in any epoch of the bucket. `now` is the newest run's state with the mutes in force now. The event lane reads the NATS events and the finding transitions with a limit of 1000 each (`nats_truncated`, `finding_truncated`), so a burst of findings cannot push the NATS events out. The Kubernetes lane reads the newest 1000 Kubernetes events of the range (`kubernetes_truncated`).

`GetTimeSeries` metric IDs are `<scope>.<measure>`; the scope `cluster` takes no entity, `server`, `stream` and `consumer` take one of that type. Byte rates (`server.in_bytes_rate`, `server.out_bytes_rate` and their `cluster.` sums) come from the VARZ byte counters, and `stream.msgs_rate` from the increase of the leader's last sequence. The step is the range divided by `max_points`, rounded up to seconds and never below the collection interval. Buckets are aligned to the Unix epoch, so the first may start before the range and hold only its part, and a series can have `max_points + 1` points. Counts (servers up, connections, consumers, messages) take the last value of each bucket, so they stay whole numbers; other gauges are averaged per bucket, counter rates are computed per server process, and slow consumer increases are summed. A rate is not computed across a gap of more than three intervals between two epochs (a collector restart), so the increase over the gap is never one sample. An overview tile shows its chart's last point, so the number and the chart use the same reduction, and the chart's range includes the value (the JetStream storage tile, whose value is the servers' newest report, widens the range to it). This replaces the earlier rule that a tile shows the newest epoch's value (proposed, not yet confirmed). The first epoch of a restarted server, and the first after a collection gap, has no rate, so the cluster rates dip for one epoch at every restart; this is a gap in the data, not a drop in traffic.

### Paging and limits

Findings and connections page by offset; events page by time and ID and mutes by ID (newest first), so new events and mutes do not shift the pages. Folded events page by the time and ID of each row's newest event, and the token keeps the highest event ID of the first page: later pages leave out newer events, so a row that gains an event cannot cross the page boundary. A connection page token keeps the epoch of the first page; once that epoch is swept, the token is refused with failed_precondition. A token is only valid with the same filters.

A request or response message is at most 1 MiB; each call has 30 seconds in the provider; filter lists hold at most 10 states, severities or sources and 50 kinds; the timeline holds at most 2000 buckets, counting a partial last one. An events histogram holds at most 120 buckets. The HTTP server has read, header and idle timeouts, a write timeout of the call timeout plus 15 seconds, and a 64 KiB header limit. A panic in a call becomes an internal error and is logged with its stack.

### Mutes

A mute covers a check (`^[a-z]+-\d{3}$`), or a check for one entity of a known type; it needs a reason (at most 1000 characters) and an expiry between one minute and 90 days ahead, truncated to seconds. Lifted and expired mutes stay as history and are not swept by retention. The handler checks every rule before the store sees the row, so a row the table's constraints still reject is a server fault: internal, with the database's text only in the log. A mute call waits for the store's write lock only as long as its context allows.

### Health and metrics

`/healthz` answers while the process serves; it is the liveness probe and the load balancer's health check. `/readyz` answers ok once every system's store is open and the API serves; the API starts after the database is open, so a pod that rebuilds its database gets no traffic, and the start probe allows ten minutes for that. Readiness does not follow collection: with one replica a not-ready pod leaves the load balancer, and the UI would vanish exactly when NATS is down. A stopped collector shows "not collecting" in the UI header and through `nats_lens_last_epoch_age_seconds` and its alert rule (proposed, not yet confirmed). The collector's own connection is dialled again when nats.go closes it for good, with the event connections' backoff (proposed, not yet confirmed).

`/metrics` on the API listener exports each firing finding as three series of one identity (`system`, `code`, `entity_type`, `entity_key`): `nats_lens_finding` with a `muted` label, `nats_lens_finding_severity` (1 to 3) and `nats_lens_finding_flapping`, so that a change of severity does not start a new series. A mute does start a new `nats_lens_finding` series, as `muted` is its label; the `_severity`, `_flapping` and `_by_check` families keep their identity. The three series are capped at 50 unmuted entities per check (the worst, then by entity key, so the set is stable); muted findings are always exported and do not count against the cap. Beside them: per-check counts and highest severity without severity labels, counts by severity and of muted findings, and the health of the collector and the checks, including epochs without check runs. Every sample carries `system` as its first label; the handler collects the samples of every system by family and writes each family's HELP and TYPE once. Each system is read in parallel into an exposition of its own, within `metrics_timeout` (8 seconds, below the usual 10-second Prometheus `scrape_timeout`) and never twice at once, a panic counting as a failure, and merged only when it succeeded, so a failure halfway leaves no partial families; `nats_lens_system_scrape_success` says per system whether it was read, and the scrape answers 500 only when every system failed. An empty system ID is refused when the registry is built and when an exposition is made, so no sample goes without the label. A mute makes alert rules on the finding series stop matching.

Alerting is not part of the tool. A Prometheus scrapes `/metrics` (on GKE, Managed Prometheus through a PodMonitoring), and its alert rules or Cloud Monitoring PromQL alert policies evaluate the findings; the README gives example rules.

### Demo

`natslens demo` serves three fake systems without NATS or a database: `example-prod`, the default, with the reference mockup's scenario (a GKE node upgrade); `example-global`, 24 servers in clusters `eu`, `us` and `ap` joined by gateways after a node upgrade left one server down, with one store 71% full and two servers on an older version; and `example-staging`, whose collector connection closed three hours ago after its credentials were rejected, so its data ends then. Each is a `scenario` value in `internal/api/fake` that the same RPC code serves; a scenario's times are written in a fixed frame and shifted to now unless `-fixed`. It uses the same event kinds as the real capture, and is the fixture for handler tests and for building the UI without a cluster. The collecting demo systems report the collection state demo; `example-global` reports the cluster name `eu`, its meta leader's, as a system reports one of its servers' cluster names (proposed, not yet confirmed).

## UI

React, Vite, TypeScript, TanStack Query and the generated Connect client. A left sidebar lists the pages in groups: Fleet, Overview and Findings at the top; Messaging (Connections); JetStream (Streams, Consumers); Cluster (Servers, Events); and a detail page per entity. `web/src/nav.ts` is the list of sidebar pages: each has a group, a path, a label, an icon, a badge source, and `planned` for a page not built yet, which stays hidden. Each feature keeps its query hooks in `web/src/api/<feature>.ts`. The header names the page and holds the account scope, a search, the time range and the theme menu.

Three parameters of the URL are global: the time range (`?range=` live or `?from=&to=` fixed), the system (`?system=`) and the account scope (`?account=`). Every link between pages carries them (`useGlobalQuery`, worked out once per URL change by `GlobalQueryProvider`), so a link opens the same view for the next person; a parameter the link sets itself wins. The account goes only to pages that apply it (`accountScope` in `nav.ts`) and their detail pages, never to another system. Every request takes the system from `useSystem()` and every query key holds it as its second element, so two systems never share a cache entry. A page without `?system=` reads the default system; with several systems its links name the default system. A `?system=` that names no configured system shows "System not found" with a link to Fleet, and the UI makes no call for it. Pages read view models from `web/src/api`, the only code that touches the generated client.

The UI handler serves files from the embedded UI; a GET for a path that names no file and whose last element has no dot gets `index.html`, and a missing asset is a 404.

Visual design: Denis chose option A (light, calm, one mint-teal accent, IBM Plex Sans for labels and IBM Plex Mono for every value, code and timestamp), and asked for a dark theme too. The dark theme and two parts of option B are in the reference mockup `docs/ui-reference/reference.html`: severity badges that differ by shape as well as colour, and a zoomed timeline with Kubernetes events above the time axis and NATS events below. Every colour is a CSS custom property with a light and a dark value; the theme follows the system unless the viewer picks one. Fonts are self-hosted and the UI makes no third-party requests.

What we take from Synadia Insights: the checks-over-time grid, the event feed with remediation and the related configuration values, monospace numbers, colour only for status.

What we do differently: lead with the worst current finding, not an average grade; show mutes with their reason and expiry; draw Kubernetes and NATS events on the same timeline.

At a phone width tables scroll inside their own container, so the page never scrolls sideways. Charts' accessible names include the current value and the range shown. Event IDs are URL-encoded in links.

## Kubernetes

The watcher (optional, `kubernetes.enabled`, in-cluster and read-only) reads `events.k8s.io/v1` Events only: the core `v1` Events API serves the same objects, so watching both would double the traffic. It also watches the NATS pods (by label selector) in the same namespaces, the nodes, and the node Events in `default`, where the API server files them.

- Every kind has one origin, so that one change is stored once. State changes (pod scheduled, ready, not ready, restarted, evicted, terminating, deleted; node added, cordoned, uncordoned, ready, not ready, deleted, kubelet version changed) come from the pod and node watches; the kubelet's and controllers' reports (killing, started, unhealthy, back-off, failed scheduling, preempted, a node Event whose reason names an upgrade) from the Events. An eviction is recorded once, from the `DisruptionTarget` condition or the `Evicted` Event, whichever comes first. Kinds carry a `k8s.` prefix (`k8s.pod.killing`, `k8s.node.drained`).
- `k8s.node.drained` is derived: a NATS pod evicted, terminated or killed on a cordoned node, recorded once per cordon. A cordon is `spec.unschedulable` or the cluster autoscaler's `ToBeDeletedByClusterAutoscaler` taint, which drains a node without setting it unschedulable. A cordon seen after a NATS pod on the node was already evicted or terminating is a drain too, as the watches do not order the two; the drained event carries the cordon's cause.
- Node changes are kept for nodes that run or ran a NATS pod within the last hour, and for nodes added within 15 minutes of a drain or upgrade, as a surge upgrade adds the new node before it drains the old one. Keeping every node of a pool that held a NATS node is `kubernetes.pool_nodes`, off by default: on GKE Autopilot the autoscaler's few pools hold every workload, and their churn would bury the upgrade.
- Pod Events of a NATS pod are stored with its server name (`kubernetes.server_name`), except routine ones (`Scheduled`, `Pulling`, `Pulled`, `Created`) and those of other containers (the config reloader). Events of other pods are stored only as warnings, kills, evictions and preemptions, without a server name.
- Each Event occurrence count is stored once: the watcher remembers the highest count per UID for three hours and is seeded from the store at start, so a relist or a restart of natslens does not store the Events again. A Kubernetes Event is one row per UID: a higher count updates the row stored within the last three hours (count, note, summary and receive time become the latest; `time` stays the first occurrence and `raw.first_seen` and `raw.last_seen` record both) instead of adding a row. The new receive time moves the row to the hour of its latest occurrence, so the archive exports both hours again by their event counts; an exported hour left without rows is exported again empty (proposed, not yet confirmed).
- Watches resume from the last resource version seen (bookmarks included). An expired version (410) means a relist, whose differences are recorded timed when noticed; it is not a gap. A list or watch failing for `gap_after` (30s) is recorded as `collector.k8s_not_watching`, its recovery as `collector.k8s_gap` with the span. A watch the server ends without an error within a second counts as a failure, and the next one opens a second later at the soonest; a failure ends when a watch opens, not at the first event, so a quiet namespace after a short failure is no gap. Lists are paged by 500 objects, at most 20 pages; attempts wait 1 second doubling to a minute.
- At start the watcher records a `collector.k8s_gap` from the newer of the last stored Kubernetes event and the last epoch, when longer than `gap_after`, and compares its first lists with the state the stored events last gave each NATS pod (ready, present) and node (cordoned, present), recording the differences as observed at start.
- The timeline's Kubernetes lane shows node cordons, drains, readiness, upgrades and deletions, the NATS pods' stops, starts and readiness, the watch gaps, and, as warnings, the failed scheduling, crash loops (`BackOff`, `CrashLoopBackOff`) and OOM kills of pods that are NATS clients: a pod whose name follows "@" in a connection name seen from an hour before the range, or one `kubernetes.client_pods` matches. The pod names are read once per collect interval per range and kept for up to 8 ranges (least recently used dropped), not on every lane request. No check reads them yet. `k8s.pod.oom_killed` comes from the pod watch for NATS pods (a restart whose last termination is `OOMKilled`); client pods are not watched, so for them it comes only from an Event with reason `OOMKilled` or `OOMKilling`, which kubelets do not send for pods (their `OOMKilling` is a Node Event). The retained Events of the development cluster it was tried on held no OOM, back-off or failed-scheduling Event, so the client path is untested on a real cluster (proposed, not yet confirmed); with a server entity only that server's pods, with any other entity the whole lane, as a drain is context for every stream. The NATS lane does not hold Kubernetes events.
- A node upgrade window opens at a drain or an upgrade, not at a cordon alone (the autoscaler cordons too), and takes in every following node or NATS pod event within 15 minutes of the one before; it ends at the last one, normally the last node or pod back to ready. A window without a drain or a NATS pod event is not shown. Windows are derived when read, from the newest 5000 events up to 12 hours before the range, not stored. One window covers all pools; its label is "GKE node upgrade" with upgrade evidence (an upgrade event, or nodes of different kubelet versions), "Node removed by the autoscaler" when every drain in it was the autoscaler's (a NATS disruption all the same), else "Node drain".

## Deployment

- One replica with the `Recreate` strategy, since DuckDB has one writer, and a PVC for the database and the archive until a bucket destination exists. The `gke-iap` example gives the PVC `argocd.argoproj.io/sync-options: Delete=false,Prune=false`, so that Argo CD never deletes the history.
- GKE Autopilot: the pod carries `cluster-autoscaler.kubernetes.io/safe-to-evict: "false"`, and its CPU limit equals its request, as Autopilot requires both. A preferred pod anti-affinity (weight 100) keeps it off nodes with a NATS server pod when another node fits, so a drain of that node does not take the observer away; a node pool of its own cannot be had on Autopilot, where pools are the autoscaler's.
- Its own namespace `nats-lens`, with list-and-watch Roles in the NATS namespace (Events, pods; `nats` in the base) and `default` (node Events) and a ClusterRole for nodes.
- Distroless `cc` image as user 65532 with a read-only root filesystem and an `emptyDir` on `/tmp`; 30 seconds to stop. The Dockerfile's Node and Go stages run on the build platform and the Go stage cross-compiles for `TARGETARCH` with Debian's gcc cross toolchain and cgo for DuckDB, so an amd64 image builds on an arm64 host without QEMU. Images go to `registry.example.com/nats-lens/natslens`.
- `k8s/base` is provider-neutral; `k8s/components/gke-iap` adds the GKE pieces, and `k8s/examples/` holds two overlays to copy: `basic` (port-forward only) and `gke-iap`. A deployment keeps its own overlay, with its image and its whole `natslens.yaml`, outside this repository.
- The NATS credentials are two Secrets the overlay provides, `natslens-nats-system` and `natslens-nats-app`, created by hand or synced from a secret store. The NATS users are scoped to the subjects natslens uses (README, NATS permissions); tests show the grants are enough.
- In the `gke-iap` example an HTTPRoute attaches to an existing Gateway; a GCPBackendPolicy turns IAP on and a HealthCheckPolicy points the load balancer at `/healthz`.
- A NetworkPolicy admits port 8080 only from Google's load balancer ranges and from Managed Prometheus (`gke-gmp-system`); a PodMonitoring scrapes `/metrics`.
- The image digest and the IAP audience are filled per deployment; the `gke-iap` example carries placeholders that the API server and natslens reject.

## Security

- No authentication of its own; IAP decides who may pass. natslens refuses to listen beyond loopback without `iap_audience` unless `allow_unauthenticated: true`, and an `iap_audience` must have the backend service form.
- With `iap_audience` set, mute and unmute need the signed `X-Goog-IAP-JWT-Assertion`. It is verified (ES256 against Google's IAP keys, fetched and cached for an hour, refetched at most once a minute for an unknown key, failed fetches included, one fetch at a time and without holding up callers whose key is cached; while refreshes fail, the cached keys stay trusted however old they are; issuer `https://cloud.google.com/iap`, the audience, exp and iat with 30 seconds of skew), and its email is recorded as the user. A call without a valid assertion is unauthenticated. Without `iap_audience` both IAP headers are ignored and the request names the user. Reads never need an assertion.
- Host allowlist: the API answers only requests whose Host is in `allowed_hosts`; without the setting that is localhost, 127.0.0.1 and ::1 when the listen address is a loopback one (against DNS rebinding from a web page), and any host otherwise. `/healthz` answers every host. On a loopback listener the allowlist applies to `/metrics`; on any other listener `/metrics` is exempt, because Prometheus scrapes by the pod's address, and that listener must stay inside the cluster.
- Every UI and API response carries `Content-Security-Policy`, `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. The policy is `default-src 'self'`; `script-src` and `style-src` are `'self'` plus the sha256 hash of each inline script and style of the embedded `index.html`, computed at start so they follow every build; `img-src 'self' data:` (the favicon); `object-src 'none'`, `base-uri 'none'`, `form-action 'self'` and `frame-ancestors 'none'`. No `unsafe-inline`.
- Credentials are never logged; the password has no flag, so it never shows in the process list. User JWTs are removed from stored event payloads at any depth.
- Supply chain: base images are pinned by digest; the web tree has an `.npmrc` with a min-release-age cooldown of 7 days; `make vuln` runs govulncheck when it is installed.

## Decisions awaiting confirmation

Proposed, not yet confirmed by the owner.

| Decision | Section |
| --- | --- |
| `systems` in the file; a file without it is one system `system_id` (default `default`) in `data_dir` unchanged; per-system: NATS, accounts, inventory, Kubernetes, retention, archive retention, directories, memory | Configuration |
| In a list: retention and archive retention default to the top-level values; top-level per-system keys and flags refused; overlapping archive and data directories and shared password variables refused | Configuration |
| A listed system's `nats.name` defaults to `natslens-<id>`, so connections can be told apart; a file without `systems` keeps `natslens` | Configuration |
| One process runs every system: per system a store, archive, collector, capture, Kubernetes watcher and check engine; one shared `checks.Set`, with each engine leaving out its own system's tooling names | Multiple systems |
| A store that fails to open fails the start; a system whose NATS is down affects neither the others nor readiness | Multiple systems |
| One start check of every system's marker and files refuses the start naming every broken system; after it, a system's connection or setup failure isolates that system (`connection_closed`, logged, tried again with the redial backoff) instead of stopping the others; a store failure while running still stops the process | Multiple systems |
| The default archive directory inside the data directory is not warned about; an `archive_dir` set inside a data directory is | Storage and archive |
| A stop signal while the stores open exits with status 0 | Multiple systems |
| Each data directory carries `system.json` with its id; another id refuses the start, a directory without one is adopted | Multiple systems |
| DuckDB `threads` is `max(2, GOMAXPROCS / number of systems)` per database, also with one system | Multiple systems |
| One shutdown deadline for all systems, which stop in parallel | Multiple systems |
| `sql` and `checks run` require `-system` when the file lists several systems, rather than reading the first | Multiple systems |
| Warnings of the configuration go out through a JSON default logger set in `main`, not again through the configured logger | Multiple systems |
| A listed system without its own `duckdb_memory_limit` gets an equal share of what the own limits leave of the budget, at least 256 MiB; a total above the budget (own limits, or the floor) is a warning, not an error | Configuration |
| Expected servers by name within `server_forget_after` (1h), plus route and meta peers; RAFTZ per account, so an epoch takes at most three windows and `timeout` is under a third of `interval` | Collection |
| Follower `num_pending`, `num_waiting`, `push_bound` stored as NULL; the `consumer_leader_stats` view holds the leader's row | Collection |
| The system connection also subscribes to `$JS.EVENT.ADVISORY.>`; cluster advisories received twice are stored once | Events |
| Client named from the API audit advisory received within 2 seconds of the action | Events |
| A separate `<name>-events` connection; refused connections redialled with backoff | Events |
| Account name from `$SYS.REQ.USER.INFO`, at most five requests; the configured label until known or when refused (also a refused reply inbox) | Events |
| Account connection counts captured only with `events.account_connections` | Events |
| Self-closed connections within 10s collapsed into one `client.churn` event per name per minute | Events |
| Stop timeout follows from `shutdown_grace + events.flush_interval` plus 2 seconds | Events |
| Configuration tables hold runs of identical configuration, not a row per epoch; applied migrations are checksummed | Storage and archive |
| Archive layout: export ids, manifest written last, superseded files removed after 10 minutes; open hour archived every 5 minutes | Storage and archive |
| Findings and mutes archived whole every hour; the newest copy wins on a rebuild | Storage and archive |
| `archive_retention` (90d, `0` keeps every hour, at least `retention`) prunes whole hours after a successful export | Storage and archive |
| Rebuild only when the database is missing or empty, written aside and renamed into place | Storage and archive |
| DuckDB `memory_limit` 60% of the cgroup limit by default | Storage and archive |
| Check header keys and `getvariable()` inputs; undeclared variables and unread thresholds refused at load | Checks |
| `system-001` built into the engine, critical after ten failed epochs in a row; the check order rotates | Checks |
| Flapping is more than three cycles (resolved transitions) in an hour; every transition is also an event | Checks |
| Restarting is only a process restarted within `restart_window`; JetStream errors and catching up are recovering | Checks |
| Quorum checks leave a restarting server's report out only while a healthy majority remains, over two epochs | Checks |
| Missing and disappeared assets weighed against their last holders (info, warning, critical) | Checks |
| `system-003` excuses one hand-over within two intervals after a step-down and allows 4 changes in 15m; `consumer-003` critical only when stuck at the limit for `ack_wait` | Checks |
| `stream-008` and `consumer-009` info while contents or positions agree; warning when they differ in two epochs in a row | Checks |
| `server-011` hysteresis (2 epochs to fire, below 0.8x to resolve); `client-001` by service over 2 epochs; `stream-005` follows the inventory and per-key TTLs | Checks |
| Read RPCs take a `TimeRange`; past ranges rebuilt from transitions; status never defaults to OK and unobserved numbers are unset | API |
| `/metrics` series identity and 50-entity cap; `/metrics` exempt from the host allowlist off loopback | API |
| An empty `system` reads the first configured system; an unknown one is invalid_argument naming the known IDs; a system whose metrics fail is left out of the scrape and reported by `nats_lens_system_scrape_success`; `run` serves the configured systems and the demo `example-prod` | API |
| `ListAccounts` lists the accounts of the range's newest epoch, or without one the accounts seen up to its end with every count unset; streams and consumers from the JSZ detail, a reported zero kept, other counts without detail unset | API |
| `/readyz` is true once every system's store is open and the API serves, independent of collection; the start probe allows ten minutes for a rebuild | API |
| Alerting through the deployment's Prometheus rules or Cloud Monitoring PromQL policies on the `/metrics` data | API |
| Kubernetes: `events.k8s.io` only, one origin per kind, derived drains, `pool_nodes` off; upgrade windows derived when read | Kubernetes |
| One row per Kubernetes Event UID, updated in place and moved to its latest receive hour | Kubernetes |
| `safe-to-evict: "false"` with CPU and memory limits equal to the requests on Autopilot | Deployment |
| PVC protected with `Delete=false,Prune=false`; a PodMonitoring scrapes `/metrics` every 30 seconds | Deployment |
| Manifests: a provider-neutral `k8s/base`, a `gke-iap` component and two example overlays; namespace `nats-lens` | Deployment |
| Credentials in two Secrets the overlay provides; scoped NATS users | Deployment |
| The `gke-iap` example routes through an existing Gateway | Deployment |
| The Dockerfile cross-compiles with a gcc cross toolchain instead of QEMU | Deployment |
| CSP with hashes of the inline script and style, nosniff and no-referrer on every response; JWTs removed from `raw` at any depth | Security |
| `iap_audience` required beyond loopback unless `allow_unauthenticated`; IAP key caching rules | Security |
| `.npmrc` cooldown of 7 days; `make vuln` runs govulncheck by hand, no CI job | Security |
| Planned pages stay in `nav.ts`, hidden; KV Stores and Object Stores stay hidden until their pages exist | UI |
| With more than one system, `/` without `?system=` lands on Fleet; with one, on the Overview | UI |
| Links carry `?system=` and the range, and `?account=` only to pages that apply it and their detail pages; a link's own parameter wins; a link to another system drops the account; with several systems a page without `?system=` links to the default system by name | UI |
| An unknown `?system=` shows "System not found", the shell makes no call for it, and the sidebar's links leave it out | UI |
| Switching system opens the list page of the current section on the other system, with the range kept and the account dropped | UI |
| The header has no server chips; the switcher shows the number of servers answering | UI |
| The Fleet badge stays empty until `ListSystems` returns each system's status | UI |
| Search loads the servers, streams, consumers, KV stores, object stores and accounts of the current system on first focus; an account result opens Connections scoped to it; the shortcut is Cmd+K on macOS (Ctrl+K kills the line there) and Ctrl+K elsewhere, never taken from another text field | UI |
| The account scope applies to Consumers (without a chosen stream) and Connections, and the header offers it only there (`accountScope` in `nav.ts`); other pages follow as they add the parameter | UI |
| The page content is keyed by path and system, so nothing of one system shows under another; mutes take the system the dialog opened on | UI |
| The collapsed sidebar is remembered in the browser and keeps the system as its initials; at phone width the sidebar is a drawer with the page behind it inert | UI |
| `ListSystems` asks the systems in parallel, 5 s each; a failure, panic or timeout is a generic `error` on that system's summary only | API |
| A system summary reads only collected data: counts from the range's newest epoch, unset without one; status no data without an epoch; strip of 48 buckets by default (at most 500), the worst cell of every check's timeline row | API |
| Without a runtime status, the collection state comes from the newest close or not-connected event of the collector's own connection after the newest epoch and reconnect; the error began at the oldest such event; the runtime status, when set, replaces it | API |
| The demo serves three systems, `example-prod` (default), `example-global` and `example-staging`; demo systems that collect report the state demo; `example-global`'s cluster name is its meta leader's, `eu` | Demo |
| Routine events: the kinds `client.churn`, `consumer.ack_sampled`, `account.connections`, and `client.connect`/`client.disconnect` of a tooling client; `checks.include_tooling` does not apply, since hiding is display only; tooling as the client checks judge it, in the newest epoch that started at or before the event (no epoch, or no stored decision, counts loopback as tooling) | API |
| An `api.audit` record is routine only when its reason is empty and its subject changes nothing: `$JS.API.` followed by STREAM CREATE, UPDATE, DELETE, PURGE, MSG.DELETE, SNAPSHOT, RESTORE, PEER.REMOVE or LEADER.STEPDOWN; CONSUMER CREATE, DURABLE.CREATE, DELETE, PAUSE, UNPIN or LEADER.STEPDOWN; META.LEADER.STEPDOWN; SERVER.REMOVE; ACCOUNT.PURGE is notable. Decided in the session that built it, not confirmed | API |
| Folding: events with the same source, kind, server, account, stream, consumer, client name and reason (and the finding ID of a finding event, the object name of a Kubernetes event) fold while each is at most 10 minutes after the previous one of the same key, whatever comes between; a Kubernetes row adds its stored count; routine events fold too when shown | API |
| `ListEventsRequest.include_facets` (12) asks for the facets and `total_unfiltered`, so that a request without the new flags is answered as before; `Event.routine` is set only on requests with a new flag, and a routine event loses its severity | API |
| Facet values: the source as its `EventSource` name, the server as its name; events without a server are not in the server facet; `total_count` and the facet counts count events, not folded rows, and the UI words them as events | API |
| Histogram: warning counts severities warning and critical; routine comes first; buckets are the range divided by the count, rounded up to a whole second, so the last may be partial and there may be fewer than asked; routine events are counted even when hidden | API |
| The demo counts a client as tooling by the `natslens-` name prefix or a loopback address; the reconnect fillers of `example-prod` and `example-global` are `client.churn` summaries | Demo |

## Out of scope for the first release

High availability, write actions towards NATS, alert delivery (findings are exposed as Prometheus metrics), a bucket archive destination.

## Local development

`scripts/dev-cluster.sh` starts three `nats-server` processes on localhost in config mode with a system account, a JetStream account and monitoring ports. `scripts/load.sh` creates the example shop's streams, consumers and KV buckets and generates traffic. Integration tests start the same cluster in process with the nats-server Go package.
