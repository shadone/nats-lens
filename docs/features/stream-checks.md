# Stream checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [consumer-checks.md](consumer-checks.md), [inventory.md](inventory.md), [streams.md](streams.md), [kv-and-object-stores.md](kv-and-object-stores.md)

## What it does

The stream checks judge each JetStream stream: whether its replicas keep quorum and stay current, whether it nears or will soon reach its limits, whether the servers that hold it agree about it, whether it matches the inventory, and whether it disappeared. A KV bucket and an object store are streams (`KV_<bucket>`, `OBJ_<bucket>`), so these checks cover them too. A stream finding is keyed by `account/stream`. There are 14 stream checks, `stream-001` to `stream-014`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

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
- **What to do:** set `max_age` or `max_bytes`; for a stream that keeps everything on purpose, declare it in the inventory, mute the finding, or accept it as by design with a review date on the Capacity page ([capacity.md](capacity.md)), and watch `server-014` for the date the store fills.

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
- **Left out:** streams of accounts whose advisories nats-lens does not capture (see [event-capture.md](event-capture.md)).

### `stream-014` Stream disappeared

- **Problem:** a stream, KV bucket or object store that a server reported a short while ago is gone. Deleting a stream deletes its messages and its consumers.
- **Fires when:** an asset that a server reported within the lookback, or later than the server that held it was last seen ready, is reported by none now. A `stream.deleted` advisory since its last report names the time and the client. Without one, the detail says which servers that held it may still hold it, and names any time nats-lens was not collecting since the last report, when a delete would have left no event. The finding stays firing for the lookback, as long as a server that held it stays away, or until the stream is back.
- **Severity:** critical with a delete event. Without one: info while a server that held it is restarting or announced its shutdown, warning while one did not answer or has JetStream disabled, as long as the ready servers that lack it are fewer than a quorum of its holders; critical once they are a quorum.
- **Thresholds:** lookback 1h.
- **Left out:** assets in the inventory, which `stream-011` reports.

## Behavior and rules

- **Leader's view first.** Checks of contents and growth (`stream-002` to `stream-006`, `stream-012`) read the leader's report, because followers report at slightly different moments.
- **Agreement checks compare every server.** `stream-007`, `stream-008` and `stream-010` leave out servers that restarted within `restart_window`; `stream-009` leaves out an unhealthy server's view under the conditions in its section; `stream-014` counts a restarting holder as one that may still hold the asset.
- **Missing assets are weighed against their holders.** `stream-011` and `stream-014` look at the servers that held the asset when it was last reported, so a server that is away does not make an asset read as deleted.
- **KV buckets and object stores count as streams.** Their findings name the stream `KV_<bucket>` or `OBJ_<bucket>`, and `stream-005` names them "KV bucket" or "Object store" in the detail.

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

## Planned

These stream checks are not built. Each line says what it would detect and what data it needs.

- **Mirror lagging.** A mirror stays many messages behind its origin, or has not been active for a while. Needs: the mirror lag and last activity from JSZ, stored today.
- **Source lagging.** A stream's sources fall behind. Needs: the largest source lag from JSZ, stored today.
- **Mirror error.** A mirror reports an error, for example when its origin is gone. Needs: the mirror error from JSZ, stored today.
- **Single replica in a cluster.** A stream, KV bucket or object store has one replica while the cluster has three or more JetStream servers, so one server loss loses its data. Needs: the replicas setting and the meta group size, stored today. An object store without replicas is the case that matters most, as its objects are often the only copy.
- **KV bucket history setting.** A KV bucket keeps a long history per key without a byte limit, so it grows by history times keys. Needs: the bucket's `max_msgs_per_subject` and `max_bytes`, stored today.
- **Stream without a consumer.** A work-queue or interest stream has no consumer, so its messages are never removed. Needs: the consumer count from JSZ, stored today.
- **Many deleted messages.** A stream holds many interior deletes, which cost memory and disk. Needs: the deleted count from JSZ, stored today.
- **Oldest message expires unread.** A stream's `max_age` removes messages before its slowest consumer reads them. Needs: the stream's first sequence and time and each consumer's ack floor, stored today. The stream page plans the same view per consumer ([streams.md](streams.md)).

## Not supported / out of scope

- **No per-subject checks.** NATS servers do not report message counts per subject.
- **No message inspection.** No check reads the messages in a stream.
- **No configuration change.** nats-lens never edits, purges or deletes a stream.
- **`stream-013` sees only captured accounts.** Without an account connection, nats-lens receives no `stream.updated` advisories for that account.
