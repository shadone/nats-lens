# Consumers

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [consumer-checks.md](consumer-checks.md), [streams.md](streams.md), [inventory.md](inventory.md), [applications.md](applications.md), [replay.md](replay.md)

## What it does

The Consumers page lists the JetStream consumers of the newest collection in the time range, worst first, with their backlog as each consumer's leader reports it. A consumer's own page shows its backlog numbers and charts, its findings, what each server that holds it reports and where the servers disagree, its configuration and its events. Together they answer: which consumer falls behind, which stopped delivering, and which one is missing.

## Behavior and rules

- Supported history titles open [metric investigations](metric-investigations.md) with the resource identity, system and range. Native observation summaries distinguish peaks from bucket aggregates, and fixed-range findings/events remain accessible. Backing-stream statistics do not measure individual keys, objects or content.


- **Chart descriptions.** Detail charts and Overview delivery histories use [shared chart help](chart-help.md), with the recorded metric definition and units.

- **The leader's view where it matters.** Pending and waiting come from the leader only, because followers report zero for both. Ack pending, redelivered, the leader and the last delivery fall back to a follower's report when no leader reports. A dash means the value was not observed, for example pending on a consumer without a leader report; it never stands for zero.
- **Durables by default.** "Include ephemeral and ordered" adds the consumers without a durable name, such as KV watchers.
- **Filter by stream.** A stream selector limits the list to one stream's consumers. Its choices are the range's streams, read when the page opens. Unlike the list they are not refreshed every 30 seconds, nor when the window regains focus: they are read again when the page is opened more than five minutes after the last successful read, when the network comes back after that, when the system or range changes, or on a reload. If a read fails, the selector keeps the streams it last read; if none was ever read, it offers only "All streams" (and the chosen stream, if any) and tries again the next time the page opens. The consumers are still listed.
- **Bounded list pages.** The UI shows at most 100 consumers per page, with the total match count and Next page. Refresh list requests a fresh first page even when page 1 is still cached. System, account, stream, ephemeral, or range changes discard the continuation. The API uses the same bounded traversal context and reset rules as [Streams](streams.md). Delivery-rate ranking rejects more than 20,000 matching historical identities before building an oversized rate map; subsequent pages reuse that ranking.
- **Metadata limits.** Consumer list capture reads inventory and finding status within its account and stream scope, with row and byte limits. Excessive metadata returns a resource error instead of an incomplete list of missing consumers.
- **The list.** For each consumer: its status, name and stream, pending messages, unacknowledged messages, redelivered messages, waiting pull requests, leader, how long ago it last delivered, and notes: "missing", "paused", "servers disagree", "ephemeral", and the codes of its firing findings.
- **Missing consumers are listed.** A consumer the inventory expects that no server reports appears in the list as "missing", with no numbers. Its page says "Missing. The consumer does not exist now: it is expected by the inventory, or it was deleted inside the range."
- **The consumer page.** The header names the stream, durable or ephemeral, the replicas, and "paused" or "servers disagree" when they apply. Stat cards show pending, ack pending, redelivered, waiting pulls, the leader and the last delivery. Charts show pending, ack pending and redelivered over the range, drawn as on the Overview tiles: line and fill neutral, the change in the accent.
- **Chart annotations.** The consumer page's charts mark the system's inferred Kubernetes windows, read with `GetTimeWindows` for the page's system and exact range rather than with the whole Overview. Pages showing the same system and range share one answer; a live range reads it again every 30 seconds. Windows keep their supporting-event identities and capture-limit notes. They are system-wide context, not a shown cause for this resource, and an empty answer does not prove complete event capture or that nothing changed. When a refresh of the windows fails, the page keeps the windows it last read and its stale banner says "Showing data from <time> UTC; the last refresh failed", with Retry. When the first read fails, the charts draw without windows and nothing says so.
- **Consistent finding evidence within a detail response.** `GetConsumer` reads the firing findings once and takes both the consumer's status and its findings list from that read, so a check run during the request cannot make the two disagree. Other facts and separate page requests are not pinned to that set; `ListConsumers` keeps its own scoped status read. This single-read policy is proposed, not yet confirmed.
- **Servers disagree and per server.** As on the stream page: a table of each server's configuration hash and creation time when they differ, and for each server its replica role, lag, delivered and ack floor sequences, creation time and configuration hash.
- **Delivery rate in the API.** `delivered_per_second` is delivery-sequence advance per second in the newest comparable run within the 15 minutes before the range’s end. It is not unique messages or acknowledgements; redeliveries can advance the sequence. Adjacent observations require one leader, the same known creation time, nondecreasing sequences and positive elapsed time within three collection intervals (at least three seconds). A leader/incarnation return cannot rejoin an older run. Unknown or conflicting creation, missing/multiple leaders, resets and invalid timing start a new run. Fewer than two comparable reports leaves the rate unset. Entirely absent rows can bridge a short elapsed gap; this does not prove continuous capture. This stricter evidence policy is proposed, not yet confirmed.
- **Busiest first, in the API.** `ListConsumers` with `sort` set to `CONSUMER_SORT_DELIVERED_RATE` puts the highest delivery rate first and the consumers without a rate last; `limit` keeps only the first ones. The default order stays worst first.
- **Copy event text.** Event types and summaries in the feed are selectable without opening or closing the row. See [Events](events.md).
- **Configuration and events.** The configuration as the leader reports it, or, for a missing consumer, the configuration the inventory expects; and the newest 15 events of the consumer.

## Scenarios

### Refresh a recently loaded list

- **Surfaces:** `ui`
- **Given** I followed Next page and the previous first page remains cached, or the traversal was evicted
- **When** I choose Refresh list
- **Then** the page requests a fresh first page with the current system, range, and filters
- **And** it disables navigation and repeat refresh until the request finishes

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

### Find the busiest consumers

- **Surfaces:** `api`
- **Given** consumers that deliver between 1 and 8 messages per second, and one that only a follower reports
- **When** I call `ListConsumers` with `sort` `CONSUMER_SORT_DELIVERED_RATE` and `limit` 6
- **Then** I receive the six fastest consumers, fastest first, each with its `delivered_per_second`
- **And** traversing every page puts the consumer only a follower reports last, with no rate

### Keep consumer status and firing evidence together

- **Surfaces:** `api`
- **Given** a consumer has a warning finding and a later check run makes it critical while its detail is read
- **When** the API returns the consumer detail
- **Then** the summary status and the returned findings come from the same read: a warning status with the warning finding
- **And** no snapshot is promised for charts, per-server views or independent requests

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
