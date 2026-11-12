# Checks

This page describes how a check is written and run, how to override its thresholds and add site-specific advice, the inventory file, and the list of checks. Each check category has its own feature page: [server](features/server-checks.md), [stream](features/stream-checks.md), [consumer](features/consumer-checks.md), [client](features/client-checks.md), [account](features/account-checks.md) and [system](features/system-checks.md) checks.

## How a check works

A check is a file `checks/<code>.sql`: a YAML header in a block comment (code, name, entity, category, severity and when a finding gets another one, kind, lookback, thresholds with defaults and units, description, remediation, related configuration, for a check that returns a number `value_name` and `value_basis`, what it measures and how, and `suppressed_by`, checks of the same entity whose finding for an entity makes this one's redundant) and one SELECT. The query returns one row per failing entity with `entity_key`, `detail` and optionally `severity` (else the header's), `value`, `cause_event_id`, `related_config` (a JSON object of setting to value), `related_source` and `condition_since` (when the condition began, where the check knows it). It reads its inputs with `getvariable()`: `epoch`, `epoch_time` (the epoch's start), `prev_epoch`, `restart_window`, `archive_since` (when the process began exporting to the archive, NULL when it does not), and for a check with a lookback `lookback`, `lookback_text`, `window_start` and `window_epoch`, plus its own thresholds (a duration threshold is an INTERVAL, a version threshold such as `1.37.0` text). The macro `ns_text(ns)` writes nanoseconds as `7d`, `30s` or `1.5ms`; `account_lookups(e)` is each account's newest lookup within 24 hours of epoch `e`, as the Accounts page reads it, and `expiry_text(t, now)` writes an expiry such as "expires 2026-11-08 09:00 UTC, in 12 days". Three table macros describe servers that cannot be taken at their word: `restarting(lo, hi)` lists the answering servers whose process replaced an earlier one within `restart_window` (a JetStream error or raft groups catching up only add to their reasons, since a server in trouble shows the same); `planned_down(e)` the missing servers that announced lame duck or shutdown; `not_ready(e)` every server that may hold an asset without reporting it. Checks that judge the cluster (quorum, agreement, missing assets) leave restarting servers' reports out while the other servers can still form a majority, and never a report that is itself evidence of a quorum loss, so a rolling restart raises nothing above info except replicas offline or behind while they are (`TestRollingRestartRaisesNothing`). The headers are validated when natslens starts: a query that reads an undeclared variable or never reads a declared threshold is refused.

After every stored epoch, outside the epoch's transaction, the engine runs every check on one connection, each within `checks.timeout`, all within half an interval. It then writes, in one transaction, a `check_runs` row per check (`ok`, `error` or `disabled`) and the changes to `findings` and `finding_transitions`: a new failing entity starts `firing`, a severity change is `changed`, an entity no longer returned is `resolved`. A check that fails to run leaves its findings as they were and raises `system-001`, critical once the same check has failed ten epochs in a row; the order of the checks rotates by one each epoch, so a budget overrun does not leave out the same checks every time. A disabled check resolves its findings; a finding that resolved more than three times in the hour is flapping. Mutes are not applied when findings are written; they apply when findings are read. Transitions and check runs are swept and archived with their epoch; the archive holds the whole `findings` table in every hour, and a rebuild keeps each finding's newest copy.

## Override thresholds

Override a threshold, or the lookback of a check that has one, in the configuration file; an unknown code, threshold or value stops the start:

```yaml
checks:
  disabled: [stream-005]
  thresholds:
    consumer-006: {max_idle: 10m}
    consumer-002: {lookback: 1h, min_growth: 500}
```

## Add site-specific remediation

The remediations describe the general case. To add your own advice (a runbook link, the team that owns a stream, how your deployment declares streams), point `checks.remediation_file` at a YAML file that maps a check code to text; the API and the UI show that text after the check's own remediation, below a `---` line. The UI renders Markdown with inline code, links, lists and tables, as in the checks' own remediations; raw HTML stays inert. The file is read at start only, so restart natslens after a change (in Kubernetes, a changed ConfigMap needs a rollout), and an unknown code stops the start:

```yaml
consumer-010:
  text: Recreate the durable from the declared configuration; the order team owns it.
  playbook: https://runbooks.example.com/nats/missing-durable
stream-011: Streams are declared in the platform repository; recreate them from there.
```

An entry can keep the plain-string form or pair non-empty `text` with `playbook`. The phone card offers the playbook as a separate link. A playbook must be an HTTPS URL with a host, without credentials or control characters; an invalid entry stops startup.

## Try a check on a copy of a database

To try a change to a check on a copy of a database: `natslens checks run -data-dir DIR [-epoch N]`. It reads within the system's `duckdb_memory_limit` and spill bound; for a large copy pass `-config` with a larger `duckdb_memory_limit`. To read the findings: `natslens sql -data-dir DIR "SELECT id, severity, state, since, detail FROM findings WHERE state = 'firing'"`.

## Inventory

`inventory_file` lists per account the streams (with their durable consumers), KV buckets and object stores that must exist, and the settings that must have a given value; a setting left out is not compared. [inventory.example.yaml](../inventory.example.yaml) describes an online shop that processes product media; [scripts/inventory.dev.yaml](../scripts/inventory.dev.yaml) matches what `make load` creates, and `make churn` then shows consumer-010 naming the `load-churn` connection. The file is loaded into the `inventory` table on every start; without one the table is emptied.

## The checks

| Code | Name | Severity | Reads |
| ---- | ---- | -------- | ----- |
| server-001 | Server not reporting | warning; info for `restart_window` after it announced lame duck or shutdown; critical with half the servers missing, counting the streams and consumers that lost quorum | `epochs.missing_names`, `planned_down` |
| server-002 | Healthz not ok | critical | `health`; JetStream-only errors are server-003's, a restarting server's catch-up errors are left out |
| server-003 | JetStream not ready | critical, warning on a restarted server; JetStream disabled critical at once | JETSTREAM errors in consecutive epochs for `min_duration` (3m) |
| server-004 | Server restarted | info; critical above `max_restarts` unplanned restarts in 1h | `servers` processes per name; says whether it was an upgrade or a configuration change |
| server-005 | Version differs from the majority | info | `server_stats.version` |
| server-006 | Configuration reloaded | info | `config_load_time` after `start_time`, within 1h |
| server-007 | JetStream store usage high | warning 75%, critical 90% | `js_storage` and `js_memory` against their limits |
| server-008 | Memory far above the other servers | warning | `mem_bytes` against the other servers' median |
| server-009 | Slow consumers | warning; critical when a route, gateway or leaf node was a slow consumer | slow consumer counters, 15m, per process |
| server-010 | Stalled clients | warning | `stalled_clients`, 15m, per process |
| server-011 | Connection churn | warning | connections accepted within 10m at least half the open ones and at least 50, in 2 epochs in a row; a firing finding resolves below 0.8 of both; the detail names the clients that connected most |
| server-012 | Raft groups catching up for too long | warning | `raft_stats.catching_up` run longer than 5m |
| server-013 | Raft write error | critical | `raft_stats.wal_error` |
| server-014 | JetStream store full soon | warning 14d, critical 3d | `js_storage` growth over 6h per process, the smaller of the two halves' slopes |
| server-015 | Stale connections | info | `stale_connections`, 15m, per process |
| server-016 | TLS certificate expires soon | warning 14d, critical 3d or expired | the six `*_tls_cert_not_after` columns of `server_stats`; a server that did not answer VARZ keeps its newest answer within 24h |
| server-017 | Route missing | warning; critical without a route to any peer | `routes` of servers whose ROUTEZ answered against the peers of their cluster that answered, 3 epochs in a row |
| server-018 | Bytes queued on a route or gateway | warning from 25% of `max_pending`, critical from 50% | `routes.pending_bytes` and `gateways.pending_bytes` in 3 epochs in a row; held down to half the level |
| server-019 | Gateway missing | warning; critical when no server of the cluster has the gateway | outbound `gateways` against the remote clusters the cluster had a gateway to or from within 24h and that still connects in or has servers answering, 2 epochs in a row; restarting servers left out |
| server-020 | Connections near max_connections | warning 80%, critical 95% | `connections` against the process's newest `max_connections`, 2 epochs in a row; held down to 90% of the level |
| server-021 | JetStream API errors high | warning | `js_api_errors` growth within 15m, at least 100 and half of `js_api_total`'s growth, 2 epochs in a row, per process; suppressed by server-003 |
| server-022 | Internal queue backlog | warning | `ipqueues.pending` at least 10000 in one queue in 3 epochs in a row; restarting servers left out |
| server-023 | CPU saturated | warning | `cpu_percent` at least 90% of the smaller of `cores` and `gomaxprocs` in 80% of the epochs of 5m (at least 5), per process; a gap over three intervals ends a run, restarting servers left out |
| server-024 | OCSP peer rejected | warning | Final `server.ocsp_peer_rejected` events within 15m, by captured server name or ID; distinct event IDs per process, resolves when the window is empty |
| server-025 | Slow routes | warning | Topology’s shared classifier finds a pair slow in two eligible collections within three intervals; both endpoints receive the finding, with peers and current RTTs |
| server-026 | Leaf node disconnects or repeated reconnects | warning, system finding | observed site absent for two fully observed collections, or three observed drops in 1h; handoffs excluded, last sighting within 24h |
| server-027 | Raft apply progress stalled | warning | at least 1000 unapplied log indexes with an unchanged applied index across 3 eligible retained samples spanning 2m |
| server-028 | Raft node remains a candidate | warning | previously led local voting group reports candidacy without a leader across 3 eligible retained samples spanning 2m |
| system-001 | Check failed to run | warning, critical after 10 epochs of the same failure | the engine's own runs |
| system-002 | No JetStream meta leader | critical | `meta_leader` of the clustered servers, or the leader seeing more peers offline than its quorum allows |
| system-003 | JetStream meta leader changing often | warning above 4 in 15m | `meta.leader_elected` events and the leader per epoch; one hand-over within two intervals after each step-down not counted |
| system-004 | JetStream meta group falling behind | warning | the meta leader's `meta_peers_not_current` less `meta_peers_offline`, in every epoch for 5m; epochs with a restarting or newly joined server count as clean, epochs without the leader's report as unknown |
| system-005 | JetStream meta group size differs from the servers | warning | the leader's `meta_cluster_size` against the answering JetStream servers of its domain and their own sizes, in every epoch for 10m |
| system-006 | Mixed JetStream API levels | warning | more than one `js_api_level` among the answering servers of one JetStream domain in every epoch for 30m; epochs with a restarting server count as clean |
| system-007 | Collections often incomplete | warning | `epoch_endpoints` not ok for answering servers or collector errors in at least 25% of 30m's epochs (10 or more); resolves below half of that |
| system-008 | Archive export behind | warning | a closed hour with epochs or events and no final `archive_hours` row for over 1h after the hour or the process start; not in `natslens once` |
| system-009 | Event messages dropped | warning | `collector.messages_dropped` events within 1h, those of the collector's own connection left out; a lower bound |
| system-010 | Notifications not delivered | warning; critical for a recent refusal or give-up | `notify_outbox`: a channel with a notification pending longer than `pending_for` (10m), or one refused or given up within 1h; names channels, never a URL |
| stream-001 | Stream quorum lost | critical | the leader most reporters name and its online replicas, or too few reporters when it does not report, 2 epochs; restarting servers left out only while the rest form a majority |
| stream-002 | Stream replica offline | warning; info while the replica's server restarts or announced its shutdown | the leader's `stream_replicas` |
| stream-003 | Stream replica lagging | warning | the leader's replica lag above 1000 operations |
| stream-004 | Stream near its message or byte limit | warning/critical; info for a limits stream with discard old | leader's messages and bytes against `max_msgs`, `max_bytes` |
| stream-005 | Stream without any limit | info | `stream_config`; a KV bucket or object store counts when it has neither a TTL nor max_bytes; left out: streams the inventory declares with max_age 0 and max_bytes -1, KV buckets the inventory lists that have allow_msg_ttl |
| stream-006 | Stream reaches its byte limit soon | warning/critical; limits streams with discard old left out | leader's bytes growth over 6h, the smaller of the two halves' slopes |
| stream-007 | Stream configuration differs across servers | warning | current `stream_config` rows, setting by setting, restarting servers left out |
| stream-008 | Stream creation time differs across servers | info; warning when a replica's last sequence or message count is behind the leader's by more than 100 or 1%, or ahead by more than 10 or 0.1% (first sequence: off by more than 100 or 1%), two epochs in a row | current `stream_config.created` and `stream_stats`, restarting servers left out |
| stream-009 | Stream peer set differs across servers | warning | the servers' peer sets, 2 epochs; restarting servers left out while the rest form a majority |
| stream-010 | Stream missing on an assigned server | warning, critical on most | leader's peers that answered JSZ without the stream and are not restarting |
| stream-011 | Expected stream or bucket missing | critical; info or warning while a server that held it is not ready | `inventory`, `stream_stats`, `not_ready`, `stream.deleted` events |
| stream-012 | Stream configuration differs from the inventory | warning | `inventory` against the leader's configuration |
| stream-013 | Stream configuration changed | info | `stream.updated` events with the client, 1h |
| stream-014 | Stream disappeared | critical with a delete event; info or warning while a server that held it is away | a stream reported within 1h (or since its server went away) that no server reports, not in the inventory |
| stream-015 | Mirror lagging | warning | leader's `mirror_lag` above 10000 in every report of 5m without falling, or `mirror_active_ns` above 2m, or never heard from its origin in two reports; held down to 0.8; a mirror error is stream-017's |
| stream-016 | Source lagging | warning | leader's `sources_max_lag` above 10000 in every report of 5m without falling; held down to 0.8 |
| stream-017 | Mirror error | warning | leader's `mirror_error` in at least 2 of the last 4 epochs |
| stream-018 | Single replica in a cluster | info | `stream_config.replicas` of 1 where 3 or more expected JetStream servers in its cluster and domain match its placement tags; not for replicas the inventory gives |
| stream-019 | KV bucket history without a byte limit | info | `KV_` stream with history above 1 and no `max_bytes`, at least 5 messages per key and 100 MiB from the leader's `stream_stats`; held down to 0.8; not while stream-005 reports it |
| stream-020 | Stream without a consumer | warning when a work-queue stream holds messages or an interest stream dropped some; info otherwise | leader's consumer count of 0 in every report of 10m, created before; not for a stream another stream mirrors or sources |
| stream-021 | Many deleted messages inside a stream | info | leader's `num_deleted` at least 1000000 and half of the sequence range; held down to 0.8 |
| stream-022 | Source not seen | warning | per-source leader activity over 2m after creation/start/election grace; never-heard needs two reports spanning 2m; held down to 0.8; errors left to stream-023 per source |
| stream-023 | Source error | warning | per-source errors in 2 of 4 collections within 5m, after 2m grace; held while any recent error remains; source removal ends its history |
| consumer-001 | Consumer quorum lost | critical | as stream-001 |
| consumer-002 | Consumer backlog growing | warning | leader's `num_pending` and delivered increases over 30m, growing in both halves |
| consumer-003 | Ack pending near max_ack_pending | warning 80% with messages waiting ("pool saturated" at the limit); critical when at the limit with no acknowledgement or delivery for ack_wait | leader's `num_ack_pending`, `num_pending`, ack floor and delivered sequence |
| consumer-004 | Many redeliveries | warning | `num_redelivered` at a quarter of `max_ack_pending`, at least 10 or `max_ack_pending` if smaller |
| consumer-005 | Messages reached max_deliver | warning | `consumer.max_deliveries` events, 1h |
| consumer-006 | No delivery while messages wait | warning | how long the backlog has waited without a delivery, 5m |
| consumer-007 | Push consumer without a subscriber | warning | leader's `push_bound` of durable push consumers, 2m |
| consumer-008 | Consumer configuration differs across servers | warning | as stream-007 |
| consumer-009 | Consumer creation time differs across servers | info; warning when a replica's delivered or ack floor position is behind the leader's by more than 100 or 1%, or ahead by more than 10 or 0.1%, two epochs in a row | current `consumer_config.created` and `consumer_stats`, restarting servers left out |
| consumer-010 | Expected consumer missing | critical; info or warning while a server that held it is not ready | `inventory`, `consumer_stats`, `not_ready`, `consumer.deleted` and `stream.deleted` events |
| consumer-011 | Consumer configuration differs from the inventory | warning | `inventory` against the leader's configuration |
| consumer-012 | Consumer missing on an assigned server | warning | leader's peers that answered JSZ without the consumer and are not restarting |
| consumer-013 | Durable consumer disappeared | critical with a consumer.deleted event; warning after its stream's delete; info or warning while a server that held it is away | a durable reported within 1h (or since its server went away) that no server reports while its stream is there; not internal, ephemeral, expiring or in the inventory |
| consumer-014 | Oldest unread message about to expire | warning at 0.8 of the stream's `max_age`, critical at 0.95; a firing finding resolves below 0.7 | the leader's ack floor or delivered sequence, dated by the first collection whose stream last sequence reached it (a lower bound for filtered consumers), not getting younger over 10m; durables only; each stream's history read back to its own `max_age` |
| consumer-015 | Pull requests at max_waiting | warning | leader's `num_waiting` at `max_waiting` with messages waiting, in 2 epochs in a row; a firing finding resolves below 0.9 or after 2 epochs with nothing waiting; `max_waiting` below 2, push and paused consumers left out |
| consumer-016 | Single replica on a replicated stream | info | a durable whose `consumer_config.replicas` is 1 on a limits stream with more |
| consumer-017 | Consumer paused for a long time | warning with messages waiting, else info | paused longer than 2h, from the first `consumer.paused` event or paused collection since it last ran or was resumed |
| consumer-018 | Consumer replicas degraded | warning | an offline, noncurrent or lagging peer in 3 consecutive leader reports while an online quorum survives |
| client-001 | High client round trip time | warning | `connections.rtt_ns` above 100ms on one client (server, pod and address, else cid) in 2 epochs in a row, by service (the connection name before "@") |
| client-002 | High pending bytes | warning | `connections.pending_bytes` above 4 MiB, by connection name |
| client-003 | Slow consumer disconnects | warning | `client.disconnect` events with a slow consumer reason, 30m |
| client-004 | Client reconnecting repeatedly | warning | 3 or more reconnects of one name in 10m: the smaller of the `client.disconnect` events not asked for by the client nor caused by a server stopping, and the `client.connect` events, of that name; instances sharing a name are merged |
| client-005 | Short-lived connections | warning | 20 or more connects of one name in 10m folded into `client.churn` events (closed by the client within `events.churn.pair_within`); instances sharing a name are merged; a firing finding resolves below 0.8 of the threshold |
| client-006 | Repeated authentication errors | warning | 15 or more `client.auth_error` events of one user in one account in 15m, from any address and server; a firing finding resolves below 0.8 of the threshold |
| client-007 | Unnamed connections | info | `connections` without a name (or nothing before "@") connected for 10m or longer, one finding `(unnamed)`; kept while any unnamed connection remains |
| client-008 | Outdated client library | info | `connections.lang` and `version` below a minimum per language (version thresholds `min_go` 1.37.0, `min_nats_js` 2.28.0, `min_python` 2.9.0, `min_java` 2.20.0, `min_dotnet` 2.5.0, `min_rust` 0.37.0, `min_c` 3.9.0) within 1h, by application |
| client-009 | Duplicate subscription | warning | a `subscriptions` row without a queue group with `subs` above 1 (one connection holding a plain subscription on a subject more than once) in each server's newest subscription list within 30m, by application; queue groups, `_INBOX.` and `$SRV.` subjects and the servers' own subscriptions left out |
| account-001 | Account near its connection limit | warning 90%, critical at the limit | client connections summed over the servers plus the leaf node connections of the server with the most, as nats-server counts them, against the account JWT's `limit_connections`, from the newest lookup within 24h |
| account-002 | Account near its JetStream disk limit | warning 90%, critical at the limit | the largest `js_storage` any server reports against `limit_js_disk`; 0 (JetStream disk off) and -1 left out |
| account-003 | Account JWT expires soon | warning 14d, critical 3d or expired | `account_details.jwt_expires` and `expired`, from the newest lookup within 24h |
| account-004 | User JWTs expire soon | warning 14d, critical 3d or expired | `connections.jwt_expires` of the epoch's connections, by account and user |

Raft diagnostics (`server-027`, `server-028`) use samples, not proof of continuous capture or an uninterrupted condition. Entirely absent epochs can bridge within `max_gap` (90s); a retained epoch missing the group or successful RAFTZ capture resets evidence, as does a larger gap. Sequence-number holes alone do not reset it. See [Server checks](features/server-checks.md) for the proposed, not yet confirmed policy.

To add a check, write its file, then a test in `internal/checks` that builds the rows it reads with the fixture helpers and shows one entity that fails and one that does not.

