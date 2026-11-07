# Streams

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [stream-checks.md](stream-checks.md), [consumers.md](consumers.md), [inventory.md](inventory.md), [capacity.md](capacity.md), [placement.md](placement.md), [kv-and-object-stores.md](kv-and-object-stores.md)

## What it does

The Streams page lists the JetStream streams of the newest collection in the time range, worst first, as their leaders report them, with how fast each one grows. A stream's own page shows its numbers and charts, its findings, what each server that holds it reports and where the servers disagree, its configuration, its consumers and its events. Together they answer: how big is each stream, is it growing, do its replicas agree, and are its consumers all there.

## Behavior and rules

- **The leader's view.** Numbers in the list come from each stream's leader in the newest collection of the range.
- **KV buckets and object stores are hidden by default.** "Include KV and object stores" adds the `KV_` and `OBJ_` streams to the list. The KV Stores and Object Stores pages show them as buckets ([kv-and-object-stores.md](kv-and-object-stores.md)).
- **The list.** For each stream: its status (the worst firing finding on it or on its consumers), name and account, "servers disagree" when the servers report different configurations or creation times for it, replicas, leader, messages, bytes, consumers, a 7-day bytes chart, growth per day, and the codes of its firing findings. A footer sums the bytes stored and counts the durable and the ephemeral or ordered consumers; these totals include KV buckets and object stores even while they are hidden from the list.
- **Present out of expected.** For a stream the inventory names, the consumer count reads present out of expected (for example `4/5`), marked when consumers are missing. For any other stream it is a plain count.
- **Growth per day.** Growth is the median of the slopes between every pair of points of the leader's bytes over the range, scaled to a day. The selected range and the entity’s actual observations must each span at least 6 hours. Growth reads at most two collection intervals before the selected start, capped at 5 minutes, and excludes the range end. Bucket reduction keeps the actual observation bounds for this requirement. A short range cannot borrow old observations. It can be negative, for example for a KV bucket whose entries expire faster than they are written.
- **The stream page.** Stat cards show messages, bytes with growth per day, consumers (present out of expected) and the leader. Charts show messages and bytes over the range, and bytes over 7 days, drawn as on the Overview tiles: line and fill neutral, the change in the accent.
- **Servers disagree.** When the servers report different configuration hashes or creation times, a "Servers disagree" table shows each server's configuration hash and creation time.
- **Per server.** For each server that holds the stream: its replica role (leader, current, behind or offline), its lag, messages, bytes, first and last sequence, creation time and configuration hash. This is where a replica rebuilt at another time, or a replica that drifted, shows.
- **A limit in the API.** `ListStreams` takes a `limit`: at most that many streams, worst first, then by name; `total_count` and the totals still cover every stream that matches. The Overview asks for the 10 worst.
- **Placement in the API.** `ListStreams` and `GetStream` give each stream a `placement`: one entry per replica, the leader first and the rest by server name. In order: the leader is `LEADER` (the first by name when two claim it); a replica the leader lists as offline is `OFFLINE`; one the leader lists that reports nothing itself is `MISSING`; one that reports but that the leader does not list is `DISAGREES`; one the leader reports not current is `BEHIND`, even with an older configuration; one with a configuration hash other than the leader's is `DISAGREES`; one the leader reports current is `IN_SYNC`. Without a leader's report a replica's state is unset, or `DISAGREES` when the servers hold different configurations. Each configured replica beyond the servers named is `MISSING`, with no server. KV buckets and object stores use the same rule. The page does not draw the squares yet.
- **Configuration.** The stream's configuration as its leader reports it.
- **Copy event text.** Event types and summaries in the feed are selectable without opening or closing the row. See [Events](events.md).
- **Consumers and events.** The stream's consumers with their backlog, and its newest 15 events, with a link to all of them.

## Scenarios

### Find the stream that grows fastest

- **Given** a range of the last 24 hours
- **When** I open the Streams page
- **Then** I see each stream's growth per day, and the worst streams first

### See a replica that was rebuilt

- **Given** the replica of `order-jobs` on `nats-1` has another creation time than the others
- **When** I open the page of `order-jobs`
- **Then** the "Per server" table shows the different creation time on the `nats-1` row, with the first and last sequences to compare

### Include KV buckets

- **Given** the account `APP` has the KV bucket `customer-info`
- **When** I tick "Include KV and object stores"
- **Then** `KV_customer-info` appears in the list

### See a lagging replica in the placement

- **Surfaces:** `api`
- **Given** `order-jobs` has 3 replicas, led by `nats-0`, and its leader reports `nats-2` as not current
- **When** I call `ListStreams`
- **Then** the placement of `order-jobs` reads `nats-0` leader, `nats-1` in sync, `nats-2` behind

### Read a stream from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** I call `GetStream` with the account and the stream name
- **Then** I receive the stream's summary, each server's view, the disagreements, the configuration and its consumers

## Planned

- **Retention at a glance.** A summary of what bounds the stream: its age, message and byte limits, which one binds now, how full each is, how many messages it published since it was created, how many it keeps now, how many expire in the next hour, and how many it publishes per day.
- **What the stream keeps.** A bar of the kept messages from oldest to newest, with the next hour's expiry marked and a tick where each consumer has acknowledged up to.
- **Published per hour.** Seven days of the publish rate from the growth of the last sequence; what was published at an hour last week expires now.
- **Limits ahead.** A table of each limit (maximum age, maximum messages, the server store, the account's disk), its current value, and when the stream reaches it at the current rate.
- **Consumers against expiry.** For each consumer: its filter, backlog, unacknowledged messages, how far it is behind the head, and when its oldest unread message expires, marked when it drains slower than its messages age out ([capacity.md](capacity.md)).

## Not supported / out of scope

- **No per-subject counts.** The servers report a stream's subject count, but no traffic per subject ([subjects.md](subjects.md)).
- **No message browsing.** nats-lens does not read the messages of a stream.
- **No stream actions.** nats-lens does not edit, purge, step down or delete a stream.
- **No growth for a short range.** Growth per day needs a range of at least 6 hours and actual observations spanning 6 hours, with the bounded lookback described above.
