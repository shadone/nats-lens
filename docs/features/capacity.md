# Capacity and retention

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [streams.md](streams.md), [stream-checks.md](stream-checks.md), [server-checks.md](server-checks.md), [checks.md](checks.md)

## What it does

Planned; nothing here is built yet. The Capacity page will show, for every stream, what it keeps, what bounds it, and when the disks fill: one card per server store with its use, growth and time to full, and a table of streams with their bounding limit and how long a span of data each one holds. Streams that grow on purpose can be accepted as by design, with a reason. All scenarios below describe intended behaviour.

## Behavior and rules

- **One card per server store.** The JetStream file store of each server: used out of its limit, the share, the growth per day, and when it is full at that rate.
- **One row per stream.** Its kind (stream, KV bucket, object store), what bounds it (age, messages, bytes, or nothing), the span of data it holds from its oldest retained message to now on a one-year scale, its size times its replicas, its growth per day, the next limit it reaches and when, and its status.
- **Accept as by design.** "Accept as by design…" records a reason and who accepted it. The stream's growth finding then leaves the Overview, and comes back if the growth changes by more than 50% ([checks.md](checks.md)).
- **Time to full uses the checks' rule.** Forecasts take the smaller of the two halves' slopes, as `server-014` and `stream-006` do, and say when they lack the history for a rate.
- **Built on stored data.** Store use and limits, stream sizes, limits and first message times are in the collections today.

## Scenarios

### Find what fills the disk

- **Surfaces:** `ui`
- **Given** the stores of three servers are 40% full and growing
- **When** I open Capacity
- **Then** the server cards show when each store is full, and the stream table shows which streams grow most

### Accept an archive that keeps everything

- **Surfaces:** `ui`
- **Given** `order-archive` has no limits on purpose
- **When** I choose "Accept as by design…" and give the reason
- **Then** the row is marked "BY DESIGN", and its finding leaves the Overview until its growth changes by more than half

### Ask the API when a stream is full

- **Surfaces:** `api`
- **Given** a stream with `max_bytes` that grows steadily
- **When** a client asks for the stream's capacity
- **Then** it receives the next limit and when the stream reaches it

## Not supported / out of scope

- **No account disk limits yet.** The account's JetStream limits need the per-account detail that nats-lens does not collect today.
- **No resizing.** nats-lens does not change limits or volumes.
