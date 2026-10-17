# Consumer checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [stream-checks.md](stream-checks.md), [inventory.md](inventory.md), [consumers.md](consumers.md), [event-capture.md](event-capture.md)

## What it does

The consumer checks judge each JetStream consumer: whether it keeps quorum, whether its backlog grows, whether its workers keep up with acknowledgements and redeliveries, whether delivery has stopped, whether the servers agree about it, whether it matches the inventory, and whether a durable consumer disappeared. A consumer finding is keyed by `account/stream/consumer`. There are 13 consumer checks, `consumer-001` to `consumer-013`. The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

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
- **Left out:** restarting servers, until they have caught up.

### `consumer-009` Consumer creation time differs across servers

- **Problem:** the servers report different creation times for the consumer. When the delivered and acknowledged positions agree, a replica was rebuilt at another time and holds the same state. When they differ too, the consumer may have been deleted and created again with one replica keeping the old one.
- **Fires when:** the creation times differ. The positions agree when a replica is at most `position_tolerance` (or `position_tolerance_ratio` of the leader's position, if that is more) behind the leader and at most `position_ahead_tolerance` (or `position_ahead_tolerance_ratio`, if that is more) ahead of it, current or not.
- **Severity:** info. Warning when the positions differ as well, in this collection and the previous one. A replica behind by more than the tolerance in two collections warns even while it catches up, but only here, where the creation times differ.
- **Thresholds:** `position_tolerance` 100, `position_tolerance_ratio` 0.01, `position_ahead_tolerance` 10, `position_ahead_tolerance_ratio` 0.001.
- **Left out:** restarting servers, until they have caught up.

### `consumer-010` Expected consumer missing

- **Problem:** a durable consumer that the inventory lists is reported by no server.
- **Fires when:** no server reports the consumer in this collection. When the events hold its `consumer.deleted` advisory, or the `stream.deleted` advisory of its stream, the detail names the time and the client that deleted it.
- **Severity:** critical. While a server that held it when it was last reported (any server, if it never was) is not ready, the consumer may still be there: info when that server is restarting or announced its shutdown, warning when it did not answer or has JetStream disabled. A delete event since its last report makes it critical in any case.
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

## Behavior and rules

- **The leader's view for the backlog.** nats-server tracks `num_pending`, waiting pull requests and push interest on the leader only, so `consumer-002`, `consumer-003`, `consumer-004`, `consumer-006` and `consumer-007` read the leader's report. Followers' values for these fields are not observed.
- **Paused consumers are quiet where it matters.** `consumer-006` and `consumer-007` leave out a consumer that is paused.
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

## Planned

These consumer checks are not built. Each line says what it would detect and what data it needs.

- **Oldest unread message about to expire.** A consumer drains slower than its oldest waiting message ages out of the stream, so messages would be dropped unread. Needs: the stream's `max_age` and first message time and the consumer's ack floor, stored today.
- **Waiting pull requests at their limit.** A pull consumer has as many waiting requests as `max_waiting` allows, so new fetches are refused. Needs: `num_waiting` and `max_waiting`, stored today.
- **Single replica on a replicated stream.** A durable has one replica while its stream has three, so it loses its state with one server. Needs: the consumer's and the stream's replicas, stored today.
- **Paused for a long time.** A consumer stays paused past a set duration. Needs: the paused state and the `consumer.paused` and `consumer.resumed` events, stored today.
- **Backoff longer than max_deliver allows.** A consumer's `backoff` list has more steps than `max_deliver`, or `ack_wait` is shorter than the first backoff step. Needs: the consumer configuration, stored today.
- **Acknowledgements slower than ack_wait.** Workers take longer to acknowledge than `ack_wait`. Needs: per-message acknowledgement times, which NATS reports only as sampled acknowledgements on consumers with a sample frequency set. Without sampling the data does not exist.

## Not supported / out of scope

- **No backlog for followers.** nats-server reports the backlog on the leader only.
- **No message content.** nats-lens names the stream sequence of a message that reached `max_deliver`; it does not fetch the message.
- **No consumer update events.** nats-server sends no advisory for a consumer update, so no check reports who changed a consumer.
- **No recreation of consumers.** nats-lens never creates, edits or deletes a consumer.
