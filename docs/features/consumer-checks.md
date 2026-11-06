# Consumer checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [stream-checks.md](stream-checks.md), [inventory.md](inventory.md), [consumers.md](consumers.md), [event-capture.md](event-capture.md)

## What it does

The consumer checks judge each JetStream consumer: whether it keeps quorum, whether its backlog grows, whether its workers keep up with acknowledgements and redeliveries, whether delivery has stopped, whether its oldest unread message is about to leave the stream, whether fetches are refused, whether it stays paused, whether the servers agree about it, whether it matches the inventory, whether it keeps its state on one server, and whether a durable consumer disappeared. A consumer finding is keyed by `account/stream/consumer`. There are 17 consumer checks, `consumer-001` to `consumer-017`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

## The checks

### `consumer-001` Consumer quorum lost

- **Problem:** a replicated consumer has no working leader. Nothing is delivered and no acknowledgement is recorded until a leader is elected again.
- **Fires when:** in each of the last `min_epochs` collections, the servers that report the consumer do not agree by majority on a leader, or the leader they agree on sees fewer online replicas than a quorum, or that leader does not report and fewer than a quorum of replicas report at all.
- **Severity:** critical.
- **Thresholds:** `min_epochs` 2.
- **Left out:** a restarting server's report while the others can form a majority without it. Never left out: a report that says the consumer has no leader, or a leader's report of fewer than a quorum online.

### `consumer-002` Consumer backlog growing

- **Problem:** messages arrive for the consumer faster than it takes them.
- **Fires when:** over the lookback, the leader's backlog (`num_pending`) grew by at least `min_growth`, and grew in both halves of the window, so one burst does not fire it. The detail gives the fill rate (backlog growth plus deliveries) and the drain rate (deliveries).
- **Severity:** warning.
- **Thresholds:** lookback 30m, `min_growth` 100, `min_span` 10m (the least span of leader data needed).
- **Left out:** followers' reports, which do not count the backlog. A consumer that was recreated within the window starts a new window.

### `consumer-003` Ack pending near max_ack_pending

- **Problem:** the consumer has nearly as many unacknowledged messages out as `max_ack_pending` allows while more messages wait. At the limit the server delivers nothing more until acknowledgements arrive.
- **Fires when:** the leader's unacknowledged messages reach `warn_ratio` of `max_ack_pending` and `num_pending` is above 0.
- **Severity:** warning; at the limit the detail reads "pool saturated", as the workers acknowledge but every slot is taken. Critical when the consumer has been at the limit with messages waiting for `min_epochs` collections and neither its ack floor nor its delivered sequence moved for at least `ack_wait`: delivery has then stopped.
- **Thresholds:** `warn_ratio` 0.8, `min_epochs` 2.
- **Left out:** consumers without a `max_ack_pending` limit, and consumers whose slots are all in use with nothing waiting.

### `consumer-004` Many redeliveries

- **Problem:** many of the consumer's unacknowledged messages were delivered more than once: they timed out (`ack_wait`) or were rejected (NAK) and came back.
- **Fires when:** the leader's redelivered messages reach `redelivered_ratio` of `max_ack_pending` (of the unacknowledged messages when `max_ack_pending` is unlimited), and at least `min_redelivered` or `max_ack_pending`, whichever is smaller.
- **Severity:** warning.
- **Thresholds:** `redelivered_ratio` 0.25, `min_redelivered` 10.
- **Left out:** consumers without redeliveries.

### `consumer-005` Messages reached max_deliver

- **Problem:** messages were delivered `max_deliver` times without an acknowledgement, and the server stopped delivering them. For a work queue, the job behind each such message is not done.
- **Fires when:** the consumer sent `consumer.max_deliveries` advisories within the lookback. Each advisory names the stream sequence of the message.
- **Severity:** warning.
- **Thresholds:** lookback 1h.
- **Left out:** consumers of accounts whose advisories nats-lens does not capture.

### `consumer-006` No delivery while messages wait

- **Problem:** messages wait and nothing is delivered. For a pull consumer nobody is fetching; for a push consumer nobody is subscribed, or delivery stopped at `max_ack_pending`.
- **Fires when:** the leader's backlog has waited longer than `max_idle` without a delivery. The wait counts from the first collection of the current run with messages waiting, or from the last delivery when that is later, so the first message after an idle hour does not fire at once. The detail gives the pull requests waiting on the leader; 0 means no client is fetching.
- **Severity:** warning.
- **Thresholds:** `max_idle` 5m.
- **Left out:** paused consumers.

### `consumer-007` Push consumer without a subscriber

- **Problem:** a durable push consumer has no active subscriber on its deliver subject, so its messages are not delivered.
- **Fires when:** the leader reports the consumer unbound for longer than `min_unbound`.
- **Severity:** warning.
- **Thresholds:** `min_unbound` 2m.
- **Left out:** ephemeral push consumers, such as KV watchers, which the server removes once they lose their subscriber; pull consumers; paused consumers.

### `consumer-008` Consumer configuration differs across servers

- **Problem:** the servers that hold the consumer's replicas report different configurations for it.
- **Fires when:** any setting differs between the servers' current configurations (ack policy, `ack_wait`, `max_deliver`, filters, `max_ack_pending` and the others), a missing value included. The creation time is `consumer-009`'s.
- **Severity:** warning.
- **Thresholds:** none.
- **Left out:** servers that restarted within `checks.restart_window` (10 minutes by default).

### `consumer-009` Consumer creation time differs across servers

- **Problem:** the servers report different creation timestamps for the consumer. Timestamps alone do not prove a restore, rebuild, recreation or unhealthy replica. Matching delivered and acknowledged positions do not establish identical state or explain the difference.
- **Fires when:** the creation times differ. The positions agree when a replica is at most `position_tolerance` (or `position_tolerance_ratio` of the leader's position, if that is more) behind the leader and at most `position_ahead_tolerance` (or `position_ahead_tolerance_ratio`, if that is more) ahead of it, current or not.
- **Severity:** info. Warning when the positions differ as well, in this collection and the previous one. A replica behind by more than the tolerance in two collections warns even while it catches up, but only here, where the creation times differ.
- **Thresholds:** `position_tolerance` 100, `position_tolerance_ratio` 0.01, `position_ahead_tolerance` 10, `position_ahead_tolerance_ratio` 0.001.
- **Left out:** servers that restarted within `checks.restart_window` (10 minutes by default).
- **Next steps:** compare delivered and acknowledged positions, replica currentness and lag, and configuration. Inspect captured `consumer.created` and `consumer.deleted` lifecycle events and available server logs. Missing events do not rule out a change. A leader step-down does not guarantee repair; deletion or recreation based only on this metadata can lose the durable's position or replay messages.

### `consumer-010` Expected consumer missing

- **Problem:** a durable consumer that the inventory lists is reported by no server.
- **Fires when:** no server reports the consumer in this collection. When the events hold its `consumer.deleted` advisory, or the `stream.deleted` advisory of its stream, the detail names the time and the client that deleted it.
- **Severity:** critical. While a server that held it when it was last reported (any server, if it never was) is not ready, the consumer may still be there: info when that server is restarting or announced its shutdown, warning when it did not answer or has JetStream disabled. A delete event since its last report makes it critical in any case, and so do ready holders that lack it once they are a quorum of those that held it; while they are fewer, the not-ready rule above applies.
- **Thresholds:** none; it reads `inventory_file`.
- **Left out:** consumers not in the inventory (see `consumer-013`). The remediation warns against restarting the owning service first, because a service that recreates a durable with `deliver_policy: all` replays the stream.

### `consumer-011` Consumer configuration differs from the inventory

- **Problem:** a consumer from the inventory exists, but a setting the inventory gives has another value on its leader.
- **Fires when:** any of `ack_wait`, `max_deliver`, `max_ack_pending`, `filter` and `replicas` differs. A `replicas` value of 0 on the consumer means the stream's replica count.
- **Severity:** warning.
- **Thresholds:** none; it reads `inventory_file`.
- **Left out:** settings the inventory does not give.

### `consumer-012` Consumer missing on an assigned server

- **Problem:** the consumer leader lists a server as a replica, but that server does not report the consumer.
- **Fires when:** an assigned server answered JSZ in this collection and does not report the consumer.
- **Severity:** warning.
- **Thresholds:** none.
- **Left out:** restarting servers, and servers that did not answer JSZ.

### `consumer-013` Durable consumer disappeared

- **Problem:** a durable consumer that a server reported a short while ago is gone while its stream is still there.
- **Fires when:** a durable that a server reported within the lookback, or later than the server that held it was last seen ready, is reported by none now. A `consumer.deleted` advisory since its last report names the time and the client. Without one, the detail says which servers that held it may still hold it, and names any time nats-lens was not collecting since the last report. The finding stays firing for the lookback, as long as a server that held it stays away, or until the durable is back.
- **Severity:** critical with a `consumer.deleted` event. Warning after a `stream.deleted` event of its stream when the stream was created again. Without an event: info while a server that held it is restarting or announced its shutdown, warning while one did not answer or has JetStream disabled, as long as the ready servers that lack it are fewer than a quorum of its holders; critical once they are a quorum.
- **Thresholds:** lookback 1h.
- **Left out:** internal consumers (`JS_` names of mirrors and sources), ephemeral and ordered consumers, durables with an inactive threshold (the server removes those by design), and durables in the inventory, which `consumer-010` reports.

### `consumer-014` Oldest unread message about to expire

- **Problem:** a durable consumer reads slower than its stream ages messages out, or has stopped: a message it has not read or not acknowledged is close to the stream's `max_age`, and the stream will remove it unread.
- **Fires when:** the consumer's oldest unread message has used `warn_ratio` of `max_age`, and over the lookback that message's age fell by less than a tenth of the time that passed, so the consumer is not catching up. The detail gives the age, the share of `max_age`, when the message leaves the stream, the age at the start of the lookback, the messages waiting and the unacknowledged ones, and whether the consumer is paused.
- **How the age is found:** a message was published no later than the start of the first collection in which the stream leader's last sequence reached it, so its age is known to a collection interval; it may read a few seconds older, as a collection starts before the servers answer. For a consumer without a filter on a stream without deleted sequences (`num_deleted` 0), the message is the one after the ack floor while messages are unacknowledged, or the one after the delivered sequence while only undelivered messages wait (the stream's first message when that one has left), and the detail says "about". Otherwise the age is a lower bound and the detail says "at least": the newest delivered message's while messages are unacknowledged, else the time since the backlog began (the earliest collection with the same delivered sequence in which messages waited, after the newest two collections in a row in which none waited, so a backlog that emptied, its messages deleted or aged out, and filled again starts at the refill; one empty collection alone, as a new leader may report, does not restart the age, so the backlog's age reads younger only after it really emptied). The backlog's start is searched among every 64th collection and then the 64 before, so its cost per consumer does not grow with the history; a collection the search skips can only make the age younger. That covers a filter, whose next sequences may not match it; deleted sequences after the consumer's position (a KV overwrite, a purge by subject, a work queue), which can be far older than its next message; and a message after the ack floor that has left the stream. With the lower bound, a stuck unacknowledged message of a consumer that keeps delivering newer ones is not seen. Each stream's history is read back to its own `max_age` plus the lookback only: a message older than that, or than the stored history, reads younger. An age reads older than the message waiting now only when sequences after the consumer's position were deleted, when messages that waited left the stream (deleted or aged out) while newer ones kept the backlog from emptying, and by the few seconds a collection takes.
- **Severity:** warning; critical once the message has used `critical_ratio` of `max_age`.
- **Thresholds:** lookback 10m, `warn_ratio` 0.8, `critical_ratio` 0.95, `resolve_ratio` 0.7: a firing finding stays until the message has used less than 0.7 of `max_age`, or the consumer catches up. These values are proposed, not yet confirmed.
- **Left out:** streams without `max_age`, ephemeral and ordered consumers, and a consumer without a leader report, or whose stream has none, in the collection or over less than half the lookback.
- **Not the stream's view:** a stream-side check would judge the stream's oldest message against `max_age`, whoever reads it. `consumer-014` judges one consumer's read position, names that consumer, and stays quiet for a consumer that is catching up, even on a stream whose oldest message is about to expire.

### `consumer-015` Pull requests at max_waiting

- **Problem:** a pull consumer holds as many waiting pull requests as `max_waiting` allows. The server refuses every new fetch with "409 Exceeded MaxWaiting", or drops it without an answer for a client that asked for heartbeats, so a worker that starts or reconnects gets nothing.
- **Fires when:** the leader's waiting pull requests (`num_waiting`, counted after the server drops expired ones) reach `warn_ratio` of `max_waiting` while messages wait (`num_pending` above 0), in each of the last `min_epochs` collections. A firing finding then holds while the waiting requests stay at `resolve_ratio` of `max_waiting` or above, through collections with no message waiting, and resolves once no message has waited in `min_epochs` collections in a row. With nothing waiting a full queue starts no finding: idle workers that each hold a long fetch are the usual pull pattern, and a refused fetch would have got nothing.
- **Severity:** warning.
- **Thresholds:** `warn_ratio` 1, `min_epochs` 2, `resolve_ratio` 0.9 (so a queue at the limit does not flap, and a busy consumer whose backlog touches 0 for one collection keeps its finding), `min_max_waiting` 2. These values are proposed, not yet confirmed.
- **Left out:** consumers with `max_waiting` below `min_max_waiting` (with 1, one worker's fetch fills it), push consumers, paused consumers, followers' reports (only the leader holds pull requests), and a consumer created again within the collections counted.
- **The limit cannot be changed in place.** nats-server refuses an update of `max_waiting`, so the remediation suggests fewer concurrent fetches or shorter expiries first, and a new durable created by start time for a larger limit. A queue that is full by design is muted with that reason.

### `consumer-016` Single replica on a replicated stream

- **Problem:** a durable consumer is configured with one replica while its stream has more. Its position and acknowledgements are on one server: while that server restarts the consumer delivers nothing, although the stream keeps its quorum, and if the server loses its disk the durable loses its position.
- **Fires when:** every current configuration of the consumer says `num_replicas` 1, the stream has limits retention and more replicas, and a server reports the consumer in this collection. The detail names the server that holds it.
- **Severity:** info, proposed, not yet confirmed: some durables are kept at one replica on purpose, for throughput, and can be muted with that reason.
- **Thresholds:** none.
- **Left out:** consumers of work-queue and interest streams, which nats-server runs at the stream's replica count whatever their configuration says; consumers with `num_replicas` 0, which take the stream's count; ephemeral and ordered consumers; internal `JS_` consumers of mirrors and sources; consumers whose servers report different replica counts, which `consumer-008` reports.

### `consumer-017` Consumer paused for a long time

- **Problem:** a consumer stays paused past a set duration. It delivers nothing until its pause ends or someone resumes it, so its backlog grows and its messages may leave the stream unread (`consumer-014`).
- **Fires when:** the consumer has been paused for longer than `max_paused`. The pause counts from the first `consumer.paused` event since the consumer last ran, or from the first collection since then in which it reports paused, whichever is earlier. The consumer last ran in the newest collection in which it reported running, or at its newest `consumer.resumed` event, if that is later. A pause extended with a new end is one pause. The detail gives the start, the duration, the pause end from the consumer's configuration, and the messages waiting; the finding names the `consumer.paused` event when there is one.
- **Severity:** warning while messages wait for the consumer; info while none wait, or no leader reports how many.
- **Thresholds:** `max_paused` 2h, proposed, not yet confirmed. A pause on a schedule, such as a consumer that runs only at night, is by design: its finding is muted with that reason.
- **Left out:** no paused consumer. The leader's report is read, or any replica's when no leader reports. A consumer created again starts a new pause.

## Behavior and rules

- **The leader's view for the backlog.** nats-server tracks `num_pending`, waiting pull requests and push interest on the leader only, so `consumer-002`, `consumer-003`, `consumer-004`, `consumer-006`, `consumer-007`, `consumer-014` and `consumer-015` read the leader's report. Followers' values for these fields are not observed.
- **Paused consumers are quiet where it matters.** `consumer-006`, `consumer-007` and `consumer-015` leave out a consumer that is paused; `consumer-017` reports the pause itself once it lasts. `consumer-014` still judges a paused consumer, as its messages expire all the same, and says in its detail that it is paused.
- **A firing finding resolves below a lower mark.** `consumer-014` and `consumer-015` keep a firing finding down to `resolve_ratio`, read from the finding's state at the previous collection, so a value that hovers at the threshold does not flap.
- **Never restart first.** The remediations of `consumer-010` and `consumer-013` tell the reader to recreate a missing durable from its previous configuration with `deliver_policy: by_start_time` before restarting the service that owns it.

## Scenarios

### Workers stop acknowledging

- **Given** the durable `resize-worker` on `order-jobs` with `max_ack_pending: 20` and messages waiting
- **When** its workers take 20 messages and acknowledge none for longer than `ack_wait`, over two collections
- **Then** `consumer-003` is critical: delivery has stopped
- **And** `consumer-006` fires once nothing was delivered for 5 minutes

### A backlog builds up

- **Given** `index-worker` receives more messages than it processes for half an hour
- **When** its backlog grows by more than 100 messages, in both halves of the window
- **Then** `consumer-002` fires, with the fill and drain rates in the detail

### A worker deletes the shared durable on shutdown

- **Given** `order-processed` is not in the inventory, and a client named `order-service` deletes it
- **When** nats-lens collects after the `consumer.deleted` advisory
- **Then** `consumer-013` is critical and names `order-service` and the time
- **And** its remediation says to recreate the durable by start time instead of restarting the service

### Messages are given up on

- **Given** a worker that fails one message every time, and `max_deliver: 3`
- **When** the server sends a `consumer.max_deliveries` advisory
- **Then** `consumer-005` fires, and the event on the Events page names the stream sequence of the message

### A consumer falls behind on a stream with max_age

- **Given** `order-callbacks` keeps messages for 1 day, and `callback-delivery` reads it slower than messages arrive
- **When** its oldest unacknowledged message is 20 hours old, and was 19 hours 50 minutes old 10 minutes before
- **Then** `consumer-014` fires as a warning, with the time the message leaves the stream
- **And** it is critical once the message is older than 95% of a day, about 22.8 hours

### Workers hold every pull slot

- **Given** the pull consumer `resize-worker` with `max_waiting: 512`, and 600 worker goroutines that each fetch with a long expiry
- **When** nats-lens sees 512 waiting pull requests in two collections in a row, with messages waiting
- **Then** `consumer-015` fires, and its detail says that new fetches are refused
- **And** the finding stays while 461 or more requests wait, and resolves below that

### A durable kept on one server

- **Given** `order-events` has 3 replicas and the durable `invoice-renderer` was created with `--replicas 1`
- **When** nats-lens collects
- **Then** `consumer-016` reports it at info, naming the server that holds it

### A pause outlives the maintenance

- **Given** someone ran `nats consumer pause order-jobs resize-worker` for a database migration, and messages keep arriving
- **When** the consumer is still paused 2 hours later
- **Then** `consumer-017` fires as a warning, starting at the `consumer.paused` event and naming it
- **And** it resolves once the consumer is resumed

## Planned

These consumer checks are not built. Each line says what it would detect and what data it needs.

- **Acknowledgements slower than ack_wait.** Workers take longer to acknowledge than `ack_wait`. Needs: the time from a message's first delivery to its acknowledgement. nats-lens captures `consumer.ack_sampled` events, with an acknowledgement time and a delivery count, for consumers with a sample frequency set, but nats-server measures that time from the latest delivery: an acknowledgement that arrives after `ack_wait`, once the message was redelivered, is measured from the redelivery and looks fast. Many redeliveries (`consumer-004`) and `consumer.max_deliveries` events (`consumer-005`) show the effect, not its cause.

## Not supported / out of scope

- **No backlog for followers.** nats-server reports the backlog on the leader only.
- **No message content.** nats-lens names the stream sequence of a message that reached `max_deliver`; it does not fetch the message.
- **No consumer update events.** nats-server sends no advisory for a consumer update, so no check reports who changed a consumer.
- **No recreation of consumers.** nats-lens never creates, edits or deletes a consumer.
- **No check of backoff against max_deliver.** nats-server refuses a consumer whose `backoff` list has more steps than `max_deliver`, and with a `backoff` list it sets `ack_wait` to the first step, so neither mismatch can exist on a running consumer.
