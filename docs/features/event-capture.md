# Event capture

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** shipped
- **Related:** [monitoring-snapshots.md](monitoring-snapshots.md), [kubernetes-context.md](kubernetes-context.md), [events.md](events.md), [client-checks.md](client-checks.md), [design.md](../design.md)

## What it does

Between two collections a lot can happen that a snapshot never sees: a leader election that takes a second, a consumer deleted and recreated, a client that connects and drops. nats-lens subscribes to the events and advisories the NATS servers publish and stores each one as an event, with the server, account, stream, consumer and client it concerns. Short-lived connections that close themselves are folded into one `client.churn` event per client and minute, so that a chatty tool does not bury everything else; the check `client-005` flags a client that does it often.

## Behavior and rules

- **System account events.** On its own system account connection (named `<nats.name>-events`), `natslens run` records client connects and disconnects with their reason (for example a slow consumer), authentication errors, lame duck and shutdown announcements, and the JetStream advisories that concern no account: meta leader elected, server out of storage, server removed, API limit reached, and a server lowering the meta group's quorum (`meta.rescue`).
- **Account advisories need an account connection.** JetStream publishes stream and consumer advisories inside the account that owns the asset. For every account listed under `accounts` in the configuration, nats-lens opens a connection with that account's credentials and records stream created, updated and deleted, consumer created and deleted, leader elected, quorum lost, max deliveries, NAK, terminated, paused and resumed, and sampled acknowledgements. Stream snapshots and restores, pinned and unpinned consumer groups and abandoned batches have kinds of their own; an advisory nats-lens does not know is stored as `jetstream.other`.
- **Who made the change.** Stream and consumer action advisories do not name a client. nats-lens reads the JetStream API audit advisory of the same call and names its client when the two arrive within 2 seconds of each other. A server-side action without an API call, such as an inactive consumer that the server removes, has no client.
- **No consumer update advisory.** nats-server 2.15 sends no advisory when a consumer is updated, so nats-lens has no event for it.
- **Each event once.** A cluster advisory that arrives in both the asset's account and the system account is stored once. An account-scoped authentication error is stored once, with the server-wide event of the same failure.
- **Connection churn is folded.** With `events.churn.collapse` on (the default), nats-lens holds each connect for `events.churn.pair_within` (10 seconds). When its disconnect arrives within that time with the reason "Client Closed", nats-lens stores neither. Instead it stores one `client.churn` event per client name, client host, account and `events.churn.window` (1 minute), whose summary reads like "jetstream-controller connected and disconnected 28 times in 1 min (each closed by the client within 10 s)". A connect not closed in time is stored as it is. A disconnect for any other reason is always stored, together with its connect. Folding only saves storage and keeps the event list readable: such connections are usually a fault of the client, and `client-005` ([client-checks.md](client-checks.md)) fires for a client name with 20 or more of them in 10 minutes.
- **Optional, noisy events.** The JetStream API audit of every call is stored only with `events.api_audit`. The connection count that every server sends for every account every 30 seconds is stored only with `events.account_connections`.
- **nats-lens records itself.** Events with source `collector` record the collector's own state: `collector.started`, `collector.gap` (the time since the last stored collection or event, at start), `collector.disconnected` and `collector.reconnected`, `collector.not_connected`, `collector.connection_closed`, `collector.subscription_error`, `collector.messages_dropped` and `collector.database_recovered`. Silence in the event list therefore never passes for health.
- **Findings are events too.** Every finding transition is also stored as an event with source `finding` (`finding.firing`, `finding.changed`, `finding.resolved`), so the event list and the timeline show findings beside the NATS events.
- **Written in batches.** nats-lens writes events every `events.flush_interval` (1 second) or every `events.batch_size` (500) events, whichever comes first. Because of the churn folding, a connect event can reach the database up to `pair_within` late.
- **Account names on events.** An account connection asks `$SYS.REQ.USER.INFO` for the account name the servers use. Until it knows that name, or for the whole run when the user may not ask, its events carry the account `name` from the configuration.
- **The account connections also ask micro services.** The connection of each account under `accounts` also sends `$SRV.INFO` and `$SRV.STATS` for the collection (see [monitoring-snapshots.md](monitoring-snapshots.md)), unless the account sets `micro: false`; nats-lens opens one connection per account for both. With event capture off (`events.enabled: false`), `natslens run` still opens a connection for each account that asks, subscribed to nothing but the replies, and an account that cannot connect then is dialled again and does not stop the collection; with event capture on, it still stops the system's start, which is tried again. `natslens once` opens one connection per asking account for its one collection and records one that does not connect as not connected. A refused `$SRV` request is recorded with the collection and logged once, and is no `collector.subscription_error` event; it is not sent again for 10 minutes, so the server the entry's connection is on logs each refusal once per 10 minutes per entry and per request (two entries for one account log twice; INFO and STATS each when both are refused). An account listed for its advisories before nats-lens asked micro services is asked too after an upgrade; `micro: false` stops it.
- **Privacy of payloads.** nats-lens keeps the raw payload of each event, with every user JWT removed.
- **Same retention as collections.** Events are kept for `retention` (14 days by default), by the time they were received, and archived hourly ([history-and-archive.md](history-and-archive.md)).

## Scenarios

### See who deleted a stream

- **Surfaces:** `ui`
- **Given** the account `APP` is listed under `accounts` with its own credentials
- **When** a client named `cleanup-job` deletes the stream `order-jobs`
- **Then** the Events page shows a `stream.deleted` event for `order-jobs` that names `cleanup-job` and its address
- **And** the stream's findings, such as `stream-014`, name the same client

### Capture only the system account

- **Surfaces:** `cli`
- **Given** a configuration without `accounts`
- **When** I run `natslens run`
- **Then** nats-lens records connects, disconnects, authentication errors, lame duck and shutdown events and the account-less JetStream advisories
- **And** it records no stream or consumer advisories

### A chatty tool connects every few seconds

- **Given** a controller that opens a connection, makes one call and closes it every few seconds
- **When** a minute passes
- **Then** nats-lens stores one `client.churn` event for that client name, host and account, counting the pairs
- **And** `client-005` fires for the client name once it has 20 or more such connections in 10 minutes
- **And** a disconnect of the same client with the reason "Stale Connection" is still stored as its own event, with its connect

### Read the raw payload of an event

- **Surfaces:** `ui`, `api`
- **Given** a stored `consumer.max_deliveries` event
- **When** I expand it on the Events page and choose "Raw payload", or call `GetEvent` with its ID
- **Then** I see the advisory as the server sent it, without user JWTs

### nats-lens was down for a while

- **Given** nats-lens stopped for 20 minutes and started again
- **When** it starts
- **Then** it records a `collector.gap` event that covers the time since its last stored collection or event

## Not supported / out of scope

- **No message content.** nats-lens records advisories and system events, never application messages.
- **No events from accounts that are not configured.** Stream and consumer advisories of an account reach nats-lens only through a connection to that account.
- **No consumer update events.** The servers do not send them.
- **No client for server-side actions.** An asset that the server removes by itself carries no client name.
- **No replay of missed events.** Advisories published while nats-lens was not connected are lost; the `collector.gap` event marks the time.
- **`natslens once` captures no events.** Only `natslens run` does, and only with `events.enabled` (the default).
