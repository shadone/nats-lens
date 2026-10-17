# Consumers

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [consumer-checks.md](consumer-checks.md), [streams.md](streams.md), [inventory.md](inventory.md), [applications.md](applications.md), [replay.md](replay.md)

## What it does

The Consumers page lists the JetStream consumers of the newest collection in the time range, worst first, with their backlog as each consumer's leader reports it. A consumer's own page shows its backlog numbers and charts, its findings, what each server that holds it reports and where the servers disagree, its configuration and its events. Together they answer: which consumer falls behind, which stopped delivering, and which one is missing.

## Behavior and rules

- **The leader's view where it matters.** Pending and waiting come from the leader only, because followers report zero for both. Ack pending, redelivered, the leader and the last delivery fall back to a follower's report when no leader reports. A dash means the value was not observed, for example pending on a consumer without a leader report; it never stands for zero.
- **Durables by default.** "Include ephemeral and ordered" adds the consumers without a durable name, such as KV watchers.
- **Filter by stream.** A stream selector limits the list to one stream's consumers.
- **The list.** For each consumer: its status, name and stream, pending messages, unacknowledged messages, redelivered messages, waiting pull requests, leader, how long ago it last delivered, and notes: "missing", "paused", "servers disagree", "ephemeral", and the codes of its firing findings.
- **Missing consumers are listed.** A consumer the inventory expects that no server reports appears in the list as "missing", with no numbers. Its page says "Missing. The consumer does not exist now: it is expected by the inventory, or it was deleted inside the range."
- **The consumer page.** The header names the stream, durable or ephemeral, the replicas, and "paused" or "servers disagree" when they apply. Stat cards show pending, ack pending, redelivered, waiting pulls, the leader and the last delivery. Charts show pending, ack pending and redelivered over the range.
- **Servers disagree and per server.** As on the stream page: a table of each server's configuration hash and creation time when they differ, and for each server its replica role, lag, delivered and ack floor sequences, creation time and configuration hash.
- **Configuration and events.** The configuration as the leader reports it, or, for a missing consumer, the configuration the inventory expects; and the newest 15 events of the consumer.

## Scenarios

### Find the consumer that falls behind

- **Given** several consumers on `order-jobs`
- **When** I open the Consumers page and choose `order-jobs` in the stream selector
- **Then** the consumers of `order-jobs` are listed worst first, with their pending and unacknowledged messages

### A durable is gone

- **Given** the inventory lists `resize-worker`, and no server reports it
- **When** I open the Consumers page
- **Then** `resize-worker` is listed with the note "missing" and no numbers
- **And** its page explains that it does not exist now

### Compare the replicas of a consumer

- **Given** the consumer `index-worker` whose replica on `nats-2` has another creation time
- **When** I open its page
- **Then** "Per server" shows each replica's delivered and ack floor sequences side by side, so I can see whether their positions agree

### Read a consumer from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** I call `ListConsumers` with a stream, or `GetConsumer` with the account, stream and consumer
- **Then** I receive the consumers with their leader's numbers, or the one consumer with each server's view and its configuration

## Planned

- **The consumer as a flow.** The page leads with a sentence that says whether the consumer drains, and when its backlog is empty at the rate of the last 10 minutes. It shows the stream it reads, the consumer and its workers side by side, with the fill rate into it and the drain rate out of it.
- **Risk of losing unread messages.** A line compares when the oldest waiting message expires with when the backlog is drained, and turns red, with the number of messages that would be dropped unread, when the drain is slower than the expiry.
- **Backlog, fill and drain on one chart.** The waiting messages, fill and drain rates and unacknowledged messages against `max_ack_pending`, with Kubernetes events (for example a worker pod restart) on the same clock.
- **Workers.** The client applications that pull from the consumer, how many instances, which server they use and their round trip time ([applications.md](applications.md)).
- **Delivery.** `ack_wait`, `max_deliver`, the messages given up on with their stream sequences (from the max deliveries advisories), the last delivery and the last acknowledgement.
- **Compared with the inventory.** A table of each setting the inventory gives against the live value.
- **Recent events in plain words**, such as "drain overtook fill".

## Not supported / out of scope

- **No follower backlog.** Followers do not report the backlog.
- **No message content.** nats-lens does not fetch the messages behind a consumer.
- **No consumer actions.** nats-lens does not create, edit, pause, step down or delete a consumer.
- **No acknowledgement latency.** The servers do not report how long each acknowledgement took, except as sampled acknowledgements where sampling is turned on.
