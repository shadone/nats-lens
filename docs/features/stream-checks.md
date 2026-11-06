# Stream checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [consumer-checks.md](consumer-checks.md), [inventory.md](inventory.md), [streams.md](streams.md), [kv-and-object-stores.md](kv-and-object-stores.md)

## What it does

The stream checks judge each JetStream stream: whether its replicas keep quorum and stay current, whether it nears or will soon reach its limits, whether the servers that hold it agree about it, whether it matches the inventory, and whether it disappeared. They also judge how a stream is kept: whether a mirror or a sourcing stream keeps up with its origin, whether a single replica holds data a cluster could spread, whether a KV bucket's history grows without a bound, whether a work-queue or interest stream has a consumer, and whether deleted messages pile up inside a stream. A KV bucket and an object store are streams (`KV_<bucket>`, `OBJ_<bucket>`), so these checks cover them too. A stream finding is keyed by `account/stream`. There are 23 stream checks, `stream-001` to `stream-023`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

## The checks

### `stream-001` Stream quorum lost

- **Problem:** a replicated stream has no working leader. Publishes to it fail until a leader is elected again.
- **Fires when:** in each of the last `min_epochs` collections, the servers that report the stream do not agree by majority on a leader, or the leader they agree on sees fewer online replicas than a quorum (2 of 3 for R3), or that leader does not report and fewer than a quorum of replicas report at all.
- **Severity:** critical.
- **Thresholds:** `min_epochs` 2.
- **Left out:** a restarting server's report while the other reporters can form a majority without it (just after a restart a server may think it leads alone). Never left out: a report that says the stream has no leader, or a leader's report of fewer than a quorum online.

### `stream-002` Stream replica offline

- **Problem:** the stream leader reports one or more replicas as offline. The stream keeps working while a quorum is online, but one more lost replica stops it.
- **Fires when:** the leader lists an offline replica in this collection.
- **Severity:** warning. Info while every offline replica's server is restarting or announced lame duck mode or shutdown.
- **Thresholds:** none.
- **Left out:** nothing; a planned restart lowers the severity instead. The detail says how long ago the leader last saw each offline replica.

### `stream-003` Stream replica lagging

- **Problem:** an online replica is far behind the leader. It does not help the quorum, and takes over with old data if it becomes leader.
- **Fires when:** the leader reports an online replica more than `max_lag` raft operations behind.
- **Severity:** warning.
- **Thresholds:** `max_lag` 1000.
- **Left out:** offline replicas, which `stream-002` reports.

### `stream-004` Stream near its message or byte limit

- **Problem:** the stream holds nearly as many messages or bytes as its `max_msgs` or `max_bytes` allows.
- **Fires when:** the leader's message count or bytes reach `warn_ratio` of the limit, whichever is fuller.
- **Severity:** warning from `warn_ratio` and critical from `crit_ratio`, because at the limit publishes fail (discard new), or a work-queue or interest stream drops messages nobody processed (discard old). A limits stream with discard old is a ring buffer that drops its oldest messages early; it is info.
- **Thresholds:** `warn_ratio` 0.8, `crit_ratio` 0.95.
- **Left out:** streams without a message or byte limit (see `stream-005`).

### `stream-005` Stream without any limit

- **Problem:** the stream has no `max_msgs`, `max_bytes` or `max_age`, so it grows until the JetStream store of its servers is full. For a KV bucket or object store, the history per key is bounded but the number of keys is not.
- **Fires when:** a stream's current configuration sets none of the three limits.
- **Severity:** info.
- **Thresholds:** none.
- **Left out:** a stream the inventory declares unbounded on purpose, with `max_age: 0` and `max_bytes: -1` both given. A KV bucket that the inventory lists and that allows per-key TTLs (`allow_msg_ttl`), whose writers bound its keys. A bucket that allows per-key TTLs but is not in the inventory is still reported, and its detail says that nats-lens cannot see whether its writers set TTLs.
- **What to do:** set `max_age` or `max_bytes`; for a stream that keeps everything on purpose, declare it in the inventory, mute the finding, or accept it as by design with a review date on the [Capacity page](capacity.md), and watch `server-014` for the date the store fills.

### `stream-006` Stream reaches its byte limit soon

- **Problem:** at its current growth, the stream reaches `max_bytes` soon.
- **Fires when:** the days until `max_bytes`, at the rate of the lookback (leader's view), are below `warn_days`. The rate is the smaller of the slopes of the window's two halves, and both must be positive.
- **Severity:** warning. Critical below `crit_days`.
- **Thresholds:** lookback 6h, `warn_days` 14, `crit_days` 3, `min_span` 1h.
- **Left out:** streams without `max_bytes`, and limits streams with discard old, as reaching their limit only drops their oldest messages.

### `stream-007` Stream configuration differs across servers

- **Problem:** the servers that hold the stream's replicas report different configurations for it. Replicas with different limits remove different messages, so their contents drift apart.
- **Fires when:** any setting differs between the servers' current configurations (subjects, retention, limits, replicas, placement and the others), a missing value included. The creation time is `stream-008`'s.
- **Severity:** warning.
- **Thresholds:** none.
- **Left out:** servers that restarted within `checks.restart_window` (10 minutes by default).

### `stream-008` Stream creation time differs across servers

- **Problem:** the servers report different creation times for the stream. When the contents agree, a replica was rebuilt at another time and holds the same messages. When they differ too, one replica may hold a stream that was deleted and created again under the same name.
- **Fires when:** the creation times differ. The contents agree when a replica's last sequence and message count are at most `position_tolerance` (or `position_tolerance_ratio` of the leader's value, if that is more) behind the leader, at most `position_ahead_tolerance` (or `position_ahead_tolerance_ratio`, if that is more) ahead of it, and its first sequence is within `position_tolerance` (or `position_tolerance_ratio` of the leader's first sequence, if that is more). Every replica is compared with the leader, whether it is current or not.
- **Severity:** info. Warning when the contents differ as well, in this collection and the previous one. A replica behind by more than the tolerance in two collections warns even while it catches up, but only here, where the creation times differ.
- **Thresholds:** `position_tolerance` 100, `position_tolerance_ratio` 0.01, `position_ahead_tolerance` 10, `position_ahead_tolerance_ratio` 0.001.
- **Left out:** servers that restarted within `checks.restart_window` (10 minutes by default).

### `stream-009` Stream peer set differs across servers

- **Problem:** the servers disagree on which servers hold the stream's replicas. During a peer move this lasts a few collections; a lasting difference means a replica follows a group the others have left.
- **Fires when:** the peer sets differ in each of the last `min_epochs` collections.
- **Severity:** warning.
- **Thresholds:** `min_epochs` 2.
- **Left out:** the view of a restarting, recovering (JetStream not ready, catching up) or leaderless server while the healthy servers form a majority, and whenever its view is a strict subset of the majority's. The check is quiet while `stream-001` fires for the same stream.

### `stream-010` Stream missing on an assigned server

- **Problem:** the leader lists a server as a replica, but that server does not report the stream: it lost or never created its replica.
- **Fires when:** an assigned server answered JSZ in this collection and does not report the stream.
- **Severity:** warning. Critical when the stream is missing on more than half of its assigned servers.
- **Thresholds:** none.
- **Left out:** restarting servers, and servers that did not answer JSZ.

### `stream-011` Expected stream or bucket missing

- **Problem:** a stream, KV bucket or object store that the inventory lists is reported by no server.
- **Fires when:** no server reports the asset in this collection. When the events hold its `stream.deleted` advisory, the detail names the time and the client that deleted it.
- **Severity:** critical. While a server that held it when it was last reported (any server, if it never was) is not ready, the asset may still be there: info when that server is restarting or announced its shutdown, warning when it did not answer or has JetStream disabled. A delete event since its last report makes it critical in any case, and so do ready holders that lack it once they are a quorum of its holders; while they are fewer, the not-ready rule above applies.
- **Thresholds:** none; it reads `inventory_file`.
- **Left out:** assets not in the inventory (see `stream-014`).

### `stream-012` Stream configuration differs from the inventory

- **Problem:** an asset from the inventory exists, but a setting the inventory gives has another value on its leader.
- **Fires when:** any of `replicas`, `retention`, `storage`, `discard`, `max_age`, `max_bytes`, `max_msgs` and `max_msgs_per_subject` (for a bucket: `history`, `ttl`, `max_bytes`, `replicas`, `storage`) differs from the inventory.
- **Severity:** warning.
- **Thresholds:** none; it reads `inventory_file`.
- **Left out:** settings the inventory does not give.

### `stream-013` Stream configuration changed

- **Problem:** a client changed the stream's configuration. The detail names the client and its address.
- **Fires when:** a `stream.updated` advisory arrived within the lookback.
- **Severity:** info.
- **Thresholds:** lookback 1h.
- **Left out:** streams of accounts whose advisories nats-lens does not capture (see [event capture coverage](event-capture.md)).

### `stream-014` Stream disappeared

- **Problem:** a stream, KV bucket or object store that a server reported a short while ago is gone. Deleting a stream deletes its messages and its consumers.
- **Fires when:** an asset that a server reported within the lookback, or later than the server that held it was last seen ready, is reported by none now. A `stream.deleted` advisory since its last report names the time and the client. Without one, the detail says which servers that held it may still hold it, and names any time nats-lens was not collecting since the last report, when a delete would have left no event. The finding stays firing for the lookback, as long as a server that held it stays away, or until the stream is back.
- **Severity:** critical with a delete event. Without one: info while a server that held it is restarting or announced its shutdown, warning while one did not answer or has JetStream disabled, as long as the ready servers that lack it are fewer than a quorum of its holders; critical once they are a quorum.
- **Thresholds:** lookback 1h.
- **Left out:** assets in the inventory, which `stream-011` reports.

### `stream-015` Mirror lagging

- **Problem:** a mirror stays behind its origin, or has stopped hearing from it. Readers of the mirror see old data, and a mirror kept for recovery is missing the newest messages.
- **Fires when:** every leader report within the lookback, covering at least half of it, has the mirror more than `max_lag` messages behind, and the lag did not fall over the window; or the mirror has received nothing from its origin, not even the heartbeat the origin sends every second, for more than `max_inactive`; or the two newest leader reports say it never heard from its origin, and its leader was elected, the stream created and the leader's server started more than `max_inactive` ago. A new leader, or a restarted server, reports that it never heard until its copy of the origin starts, which takes seconds, or longer while the origin has no leader, so such a report after a leader change or a restart is not a finding.
- **Severity:** warning.
- **Thresholds:** lookback 5m, `max_lag` 10000, `max_inactive` 2m, `resolve_ratio` 0.8 (proposed, not yet confirmed).
- **Resolves when:** the lag falls below `resolve_ratio` of `max_lag` and the time without data below `resolve_ratio` of `max_inactive`. A collection without a leader report keeps the finding as it was, as long as a leader report is within the lookback.
- **Left out:** a mirror whose lag falls, such as one catching up after a restart or a new mirror copying an old origin. A mirror with an error in either of its two newest leader reports, which is `stream-017`'s, also in the collections before `stream-017` has seen it `min_epochs` times; a firing `stream-015` finding stays out of this rule until `stream-017` fires for the mirror.

### `stream-016` Source lagging

- **Problem:** a stream that sources other streams stays behind one of them, so it lacks their newest messages.
- **Fires when:** every leader report within the lookback, covering at least half of it, has the largest lag across the stream's sources above `max_lag`, and that lag did not fall over the window. This check uses the largest lag across the reported sources; the detail lists their names.
- **Severity:** warning.
- **Thresholds:** lookback 5m, `max_lag` 10000, `resolve_ratio` 0.8 (proposed, not yet confirmed).
- **Resolves when:** the lag falls below `resolve_ratio` of `max_lag`; a collection without a leader report keeps the finding, as for `stream-015`.
- **Left out:** a lag that falls. A silent source keeps the lag it last had, so this check does not detect silence below `max_lag`. `stream-022` reports silence and `stream-023` reports source errors.

### `stream-017` Mirror error

- **Problem:** a mirror reports an error, for example because its origin is gone or out of its account's reach. It copies nothing while the error lasts.
- **Fires when:** the mirror's leader reports an error in at least `min_epochs` of the last twice `min_epochs` collections. The server clears the error each time it tries again, so a mirror that keeps failing shows it in some collections and not in others. The detail gives the server's error text, and says so when the newest report does not have it.
- **Severity:** warning.
- **Thresholds:** `min_epochs` 2 (proposed, not yet confirmed).
- **Resolves when:** no leader report in the last twice `min_epochs` collections has an error.
- **Left out:** a follower's report, as only the leader runs the mirror.
- **Not seen:** an error that shows in fewer than `min_epochs` of every twice `min_epochs` collections, such as one in every fourth or fifth. `stream-015` then reports the mirror only when it has not heard from its origin or is lagging.

### `stream-018` Single replica in a cluster

- **Problem:** a stream, KV bucket or object store has one replica in a cluster that could hold three, so losing that one server's disk loses its data, and while the server is down the asset is unavailable.
- **Fires when:** the asset's configuration has one replica and `min_servers` or more JetStream servers could hold its replicas: servers the collection expects, in the asset's cluster and domain, that its placement tags allow, as nats-server matches them: without case, a tag with a leading `!` rules out the servers that carry it, and a server tagged `!jetstream` takes no stream. The collection expects the servers that answered, those that stopped answering within `server_forget_after` (1h by default), and the route and JetStream meta group peers that the answering servers report. A cluster scaled down therefore stops counting a removed server an hour after it last answered, or, while the others still list it as a meta group peer, once it is removed with `nats server raft peer-remove`.
- **Severity:** info, object stores and mirrors included; a mirror's detail says that its origin holds the data.
- **Thresholds:** `min_servers` 3 (proposed, not yet confirmed, as is the severity).
- **Resolves when:** the replicas are raised. While the asset's only server is not ready (restarting, missing, JetStream not ready), the configuration it reported last is kept, so a restart does not resolve and fire the finding again.
- **Left out:** an asset whose replicas the inventory gives: one replica there is by design, another number is `stream-012`'s. A stream on a server without a cluster. A stream pinned by placement tags to fewer than `min_servers` servers.

### `stream-019` KV bucket history without a byte limit

- **Problem:** a KV bucket keeps several revisions of every key and has no byte limit, so its size is bounded only by its keys times its history, and its keys are not bounded. A TTL bounds how long a revision lives, but not how many keys hold their full history. The check fires on the size and revisions per key reached, not on a growth rate.
- **Fires when:** the bucket's history is above 1, its `max_bytes` is not set, and its leader reports on average `min_revisions` or more messages per key (messages divided by keys) and `min_bytes` or more in all.
- **Severity:** info.
- **Thresholds:** `min_revisions` 5, `min_bytes` 100 MiB, `resolve_ratio` 0.8, lookback 10m (proposed, not yet confirmed).
- **Resolves when:** the revisions per key or the size fall below `resolve_ratio` of their threshold, or the bucket gets a byte limit. A collection without a leader report keeps the finding while a leader report is within the lookback.
- **Left out:** a bucket whose `max_bytes` the inventory gives, `-1` included, as a decision to leave it unbounded. The check is quiet while `stream-005` reports the bucket (no TTL and no byte limit), whose detail names the history.

### `stream-020` Stream without a consumer

- **Problem:** a work-queue or interest stream has no consumer. A work-queue stream keeps every message until a consumer takes it, so messages pile up until a limit stops publishes or drops them. An interest stream with no consumer stores nothing: the server acknowledges each publish and drops the message.
- **Fires when:** every leader report within the lookback, covering at least half of it, counts no consumer on the stream, and the stream was created before the lookback began.
- **Severity:** warning when a work-queue stream holds messages, or when an interest stream's last sequence rose within the lookback (messages were dropped). Info otherwise (proposed, not yet confirmed).
- **Thresholds:** lookback 10m (proposed, not yet confirmed).
- **Resolves when:** a leader report within the lookback counts a consumer. A collection without a leader report keeps the finding.
- **Left out:** limits streams, KV buckets and object stores. A stream while `consumer-010` or `consumer-013` reports one of its consumers missing, as that finding names the consumer to bring back. A stream that a stream in the store mirrors or sources, matched by name in any account: the copying stream reads it through a consumer the servers do not count, as nats-server 2.12.5 and 2.15.0 create it as a direct consumer without storage (only a durable named in the source's configuration, an option 2.15.0 has and 2.12.5 does not, is counted). An interest stream read only by consumers that clients create on demand and that go away when idle is not left out: it fires between their visits by design, as messages published then are dropped.

### `stream-021` Many deleted messages inside a stream

- **Problem:** the stream holds many deleted messages between its first and last sequence (interior deletes). Every replica keeps a record of the gaps in memory and carries it in the snapshots a replica catches up from. Many of them usually mean one old message holds the start of the stream open: an unacknowledged work item, or a KV key not written since.
- **Fires when:** the leader reports `min_deleted` or more interior deletes, and they are `min_deleted_ratio` or more of the sequence range (messages plus interior deletes). The detail gives the age of the oldest message.
- **Severity:** info.
- **Thresholds:** `min_deleted` 1000000, `min_deleted_ratio` 0.5, `resolve_ratio` 0.8, lookback 10m (proposed, not yet confirmed).
- **Resolves when:** the deletes or their share fall below `resolve_ratio` of their threshold. A collection without a leader report keeps the finding while a leader report is within the lookback.
- **Left out:** a follower's count.

### `stream-022` Source not seen

- **Problem:** a source stops receiving data and heartbeats from its origin, even when its last reported lag is zero.
- **Fires when:** the leader reports activity older than `max_inactive`, after grace for stream creation, server start, and leader election. A source that has never heard from its origin needs two newest reports saying so and observations spanning `max_inactive`.
- **Severity:** warning.
- **Thresholds:** lookback 5m, `max_inactive` 2m, `resolve_ratio` 0.8 (proposed, not yet confirmed). Keep the lookback longer than `max_inactive` to cover the required never-heard observation span.
- **Resolves when:** activity falls below 0.8 of `max_inactive`. A firing source holds its last known silence through missing runtime or leader reports within the lookback. Removing or changing the source ends its history. A missing configuration stays unknown; an observed empty source list removes the old sources.
- **Left out:** idle sources receiving heartbeats, mirrors, and a source with any error report inside the lookback. Other silent sources in the same stream remain visible. A configured source without an attributable runtime report cannot start a finding.
- **Detail:** each silent source's name, external prefixes, filter or transforms, and configuration ID, with its last activity or never-heard state. Sources sharing an origin name remain distinct.

### `stream-023` Source error

- **Problem:** a source cannot establish its copy from an origin, for example because the origin is missing or account permissions prevent access.
- **Fires when:** the source reports errors in at least `min_epochs` of the last twice `min_epochs` collections inside the lookback. A new finding needs a current runtime report and grace after creation, server start, leader election, and first observation of this source configuration.
- **Severity:** warning.
- **Thresholds:** lookback 5m, `min_epochs` 2, `grace` 2m (proposed, not yet confirmed). Keep the lookback longer than `grace` and enough collections to include `min_epochs` errors.
- **Resolves when:** the recent collection window has no errors. NATS clears errors at each retry, so one clean report does not resolve the finding. Missing reports retain recent errors only within that window and the lookback.
- **Left out:** mirrors, follower reports, removed source configurations, and ambiguous runtime matches. A different source cannot inherit a firing source's history.
- **Detail:** each failing source's name, external prefixes, filter or transforms, configuration ID, and reported error. Read the source and origin's stream info, account permissions, external prefixes, and links before considering a configuration change. NATS retries automatically.

## Behavior and rules

- **Leader's view first.** Checks of contents and growth (`stream-002` to `stream-006`, `stream-012`, `stream-019` to `stream-021`) read the leader's report, because followers report at slightly different moments. The mirror and source checks (`stream-015` to `stream-017`, `stream-022`, and `stream-023`) read only the leader, as only the leader runs the mirror or the sources.
- **New stream checks do not flap.** `stream-015`, `stream-016`, `stream-019`, `stream-020` and `stream-021` read whether their finding was firing in the previous collection: a firing finding keeps firing down to `resolve_ratio` (0.8) of the thresholds, or until the condition clearly ends, and through a collection without a leader report as long as one is within the lookback. A finding that is not firing needs the full threshold in the current collection. `stream-015` needs two leader reports for a mirror that never heard from its origin, `stream-017` needs the error in `min_epochs` of the last twice `min_epochs` collections, and `stream-018` keeps an asset's last configuration while its only server is not ready.
- **One problem, one finding.** `stream-015` is quiet for a mirror with an error in either of its two newest leader reports, which is `stream-017`'s; `stream-019` is quiet for a bucket while `stream-005` reports it; `stream-020` is quiet for a stream while `consumer-010` or `consumer-013` reports one of its consumers missing; `stream-018` leaves an asset whose inventory entry gives other replicas to `stream-012`.
- **Agreement checks compare every server.** `stream-007`, `stream-008` and `stream-010` leave out servers that restarted within `restart_window`; `stream-009` leaves out an unhealthy server's view under the conditions in its section; `stream-014` counts a restarting holder as one that may still hold the asset.
- **Missing assets are weighed against their holders.** `stream-011` and `stream-014` look at the servers that held the asset when it was last reported, so a server that is away does not make an asset read as deleted.
- **KV buckets and object stores count as streams.** Their findings name the stream `KV_<bucket>` or `OBJ_<bucket>`, and `stream-005`, `stream-018`, `stream-019` and `stream-021` name them "KV bucket" or "Object store" in the detail.

## Scenarios

### A stream loses quorum and recovers

- **Given** an R3 stream `order-jobs` whose servers `nats-0` and `nats-1` are down
- **When** nats-lens collects twice in a row
- **Then** `stream-001` fires as critical for `APP/order-jobs`, and its detail lists each reporting server's view of the leader
- **And** it resolves once a leader with a quorum is back

### A replica was rebuilt

- **Given** the replica of `order-jobs` on `nats-1` was rebuilt five days after the others, and holds the same messages
- **When** nats-lens collects
- **Then** `stream-008` is info, and its detail names the servers on each creation time and says their contents agree

### A cleanup job deletes a stream

- **Given** `order-archive` is not in the inventory, and a client deletes it
- **When** nats-lens collects after the delete advisory
- **Then** `stream-014` "Stream disappeared" is critical, naming the time and the client

### An archive stream must keep everything

- **Surfaces:** `cli`
- **Given** the inventory declares `order-archive` with `max_age: 0` and `max_bytes: -1`
- **When** nats-lens starts with that inventory
- **Then** `stream-005` does not report `order-archive`

### A mirror loses its origin

- **Given** `order-archive` mirrors `order-events`, and `order-events` is deleted
- **When** nats-lens collects while the mirror's leader reports "stream not found"
- **Then** `stream-015` "Mirror lagging" stays quiet for it, though the mirror no longer hears from its origin
- **When** the leader reports the error again within the next three collections (the server clears it at each retry, so it can skip one)
- **Then** `stream-017` "Mirror error" is a warning for `APP/order-archive` with that error

### A mirror falls behind and catches up

- **Given** the mirror `order-archive` is 40000 messages behind `order-events`, and its lag grows for 5 minutes
- **When** nats-lens collects
- **Then** `stream-015` is a warning, and its detail gives the lag now and at the start of the window
- **When** the mirror catches up and its lag falls below 8000, 0.8 of `max_lag`
- **Then** the finding resolves

### A bucket kept on one server

- **Given** the KV bucket `worker-presence` has one replica in a cluster of three JetStream servers, and the inventory does not give its replicas
- **When** nats-lens collects
- **Then** `stream-018` "Single replica in a cluster" is info for `APP/KV_worker-presence`, naming the server that holds it
- **And** while that server restarts the finding stays, and its detail says the server is not ready

### A work queue nobody consumes

- **Given** the work-queue stream `order-callbacks` holds 50 messages and its consumer was deleted by a client more than 10 minutes ago, and the inventory does not list the consumer
- **When** nats-lens collects
- **Then** `stream-020` "Stream without a consumer" is a warning, saying the messages wait and nothing removes them
- **And** for an interest stream in the same state that clients publish to, the detail says the server drops each message

### A bucket keeps every revision

- **Given** the KV bucket `stock-levels` has history 10, a TTL of 48 hours, no byte limit, 1000 keys and 8000 messages in 200 MiB
- **When** nats-lens collects
- **Then** `stream-019` is info with 8 revisions per key as its value

### One old message holds a work queue open

- **Given** the work-queue stream `media-windows` kept one unacknowledged message from three days ago while 4 million later ones were acknowledged
- **When** nats-lens collects
- **Then** `stream-021` "Many deleted messages inside a stream" is info, and its detail says the oldest message is 3 days old

### An idle source and a silent source

- **Given** `order-search` sources `order-events` and `catalog-changes`, and nobody publishes to either origin
- **When** `order-events` keeps sending heartbeats but `catalog-changes` reports no activity for more than 2 minutes after the creation, server-start, and election grace
- **Then** `stream-022` warns for `APP/order-search` and identifies only `catalog-changes`
- **And** a configured source without a runtime report does not count as never heard

### A source retries an error

- **Given** `order-search` has observed a source configuration for more than 2 minutes, and its creation, server start, and leader election also precede that grace
- **When** its leader reports an origin error in two of four collections, clearing it between retries
- **Then** `stream-023` warns with the source identity and error
- **And** `stream-022` stays quiet for that source while another silent source in the same stream can still warn
- **When** four collections pass without the error, or the source is removed
- **Then** the error finding resolves

## Planned

These stream checks are not built. Each line says what it would detect and what data it needs.

- **Oldest message expires unread.** A stream's `max_age` removes messages before its slowest consumer reads them. Not built as a stream check: the consumer side, a consumer whose oldest waiting message approaches expiry, is built as `consumer-014` in [consumer-checks.md](consumer-checks.md) and reports the risk per consumer. Telling from the stored data that messages were already removed unread does not work either: a filtered consumer's ack floor stays at the last message it matched, so once the stream removes later messages on other subjects the floor is below the stream's first sequence, though the consumer missed nothing. The stream page plans the same view per consumer ([streams.md](streams.md)).

## Not supported / out of scope

- **No per-subject checks.** NATS servers do not report message counts per subject.
- **No message inspection.** No check reads the messages in a stream.
- **No configuration change.** nats-lens never edits, purges or deletes a stream.
- **`stream-013` sees only captured accounts.** Without an account connection, nats-lens receives no `stream.updated` advisories for that account.
