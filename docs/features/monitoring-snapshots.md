# Monitoring snapshots

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [overview.md](overview.md), [event-capture.md](event-capture.md), [history-and-archive.md](history-and-archive.md), [servers.md](servers.md), [running-nats-lens.md](running-nats-lens.md), [design.md](../design.md)

## What it does

Every collect interval (30 seconds by default), nats-lens takes one snapshot of the whole NATS system, called an epoch in the data and a collection in the UI. It asks every server for its monitoring reports through the system account, keeps each server's own answer, and stores the collection in one piece. A collection in which a server or a report is missing is still stored, marked partial, so that a gap in the answers is visible rather than hidden.

## Behavior and rules

- **Discovery, then detail.** nats-lens first finds the servers with `$SYS.REQ.SERVER.PING.STATSZ`. It then asks every server for VARZ, CONNZ (with subscription counts), ROUTEZ, GATEWAYZ, LEAFZ, ACCOUNTZ, account STATZ, JSZ (accounts, streams, consumers, their configuration and raft groups), HEALTHZ (with details), RAFTZ and IPQUEUESZ.
- **RAFTZ per account.** A plain RAFTZ request returns only the JetStream meta group, so nats-lens sends one RAFTZ request per JetStream account it saw in JSZ, recorded as `RAFTZ:<account>`.
- **A collect window per request.** Each request waits up to `timeout` (3 seconds by default) for replies and stops early once every server it expects has answered. A collection takes at most three windows, so nats-lens refuses a `timeout` that is not under a third of `interval`.
- **Every server's view is kept.** nats-lens stores what each server reports about every asset, connection and raft group. It never keeps one server's answer in place of the others'.
- **Expected servers, by name.** A server gets a new server ID on every restart, so nats-lens tracks servers by name. A collection expects every server name that answered within `server_forget_after` (1 hour by default), every server that answers now, and every route peer and JetStream meta peer that those servers list.
- **Partial collections are stored.** A collection is partial when an expected server answered nothing, when a server did not answer one of the requests or answered with an error, or when the collector itself failed (for example, a reply it could not decode). nats-lens records the outcome of every request per server: `ok`, `error` (with the error text) or `missing` (no reply within the window).
- **Connections are capped.** nats-lens stores at most `connz_limit` (4096 by default) connections per server per collection.
- **Subscription lists, every few minutes.** A collection also asks every server for its subscription list (SUBSZ) when the last one that did started at least `subscriptions_interval` (5 minutes by default) earlier; `0` turns the lists off. The request goes out with the other reports, so the collection takes no longer, and SUBSZ is recorded missing for a server only in a collection that asked for it. Each server lists the subscriptions of its own connections: its clients' and those of its internal system, JetStream and account clients (a stream's subjects, for example, under its leader). It does not list what its routes, gateways and leaf nodes carry, so a client's subscription is listed once, by the server the client is connected to. nats-lens keeps the account, subject, queue group, connection ID and delivered message count, one row per connection, subject and queue group, at most `subsz_limit` (10,000 by default) subscriptions per server; it records how many the server holds, so a cut list shows as cut. Subscriptions carry no message content and no JWT. The [Subjects](subjects.md) page reads them.
- **Account details.** nats-lens asks ACCOUNTZ about each account the servers list, in operator and config mode, at most every 10 minutes per account and for at most 32 accounts per collection, the ones never asked first. It keeps the account's readable name, whether it is the system account, its imports and exports, and from its account JWT the issue time, the expiry and the limits (connections, leaf nodes, subscriptions, data, payload, JetStream disk and memory, streams, consumers). The JetStream limits are the ones the server enforces: the account-wide ones when the JWT sets them, else the tiers, each tier's disk and memory counted once per replica so that they compare with the account's replicated usage. A config-mode account has no JWT, so its limits are not known.
- **Micro services in the listed accounts.** For each account listed under `accounts`, nats-lens asks the account's services built on the NATS micro framework over that account's connection: `$SRV.STATS` in every collection, `$SRV.INFO` in the first, every `subscriptions_interval` and whenever STATS names an instance it has not described. It stores each instance's version, description, metadata and endpoints, and per endpoint its subject, queue group, request and error counts, processing time, last error and custom stats, readable with `natslens sql` in `micro_instances`, `micro_stats` and `micro_endpoints`. It records how each request went per account in `micro_requests`: `ok`, `no_responders` (no service in the account), `timeout` or `error` (for example a permissions violation, with the server's text). A request that fails never makes the collection partial. An account with `micro: false` is not asked. The page that shows the services is planned ([micro-services.md](micro-services.md)).
- **Readable account names.** In operator mode the servers name accounts by ID. nats-lens learns each account's readable name from the servers' reports, from the account lookups and from connect events. A name, once known, is kept.
- **Expiry of certificates and user JWTs.** From VARZ, nats-lens keeps when each server's listener certificates expire: client, cluster, gateway, leaf node, websocket and MQTT. From CONNZ it keeps whether each connection presented a user JWT and when that JWT expires. The server reports a bearer JWT as empty, so its connection counts as having none.
- **No JWT is stored.** nats-lens reads the expiry and limits from a JWT's claims, without verifying it (the server did when the client connected), and drops the token.
- **Collection pauses while NATS is away.** `natslens run` skips collections while its connection has no server and keeps serving the UI and the API. With event capture on (the default), it records `collector.not_connected` after 30 seconds without a server (and again every 10 minutes), and `collector.reconnected` when it connects again. When the NATS client closes the connection for good, for example after its credentials were revoked, nats-lens records `collector.connection_closed` and dials again, waiting 2 seconds at first and doubling up to a minute.
- **The permissions it needs.** The system account user publishes only to `$SYS.REQ.SERVER.PING.>` and `$SYS.REQ.ACCOUNT.PING.STATZ`, and subscribes to `$SYS.ACCOUNT.>`, `$SYS.SERVER.>`, `$JS.EVENT.ADVISORY.>` and `_INBOX.>`. A scoped user is safer than the account's full user, which may also send the `$SYS.REQ` calls that reload, kick or lame-duck a server. The project README lists the grants for the account users, which ask `$SRV.INFO` and `$SRV.STATS` too.

## Scenarios

### Collect every interval

- **Surfaces:** `cli`
- **Given** a system account user with the grants above
- **When** I run `natslens run -nats-url nats://nats.example.com:4222 -nats-creds ./sys.creds`
- **Then** nats-lens stores one collection every 30 seconds in `./data/natslens.duckdb`
- **And** it logs "epoch stored" for each one, with how many servers it expected, how many answered, the missing names and whether the collection was partial

### Take one collection and stop

- **Surfaces:** `cli`
- **Given** a reachable NATS system
- **When** I run `natslens once` with the same connection flags
- **Then** nats-lens stores a single collection, runs the checks on it and exits
- **And** it captures no events, serves no UI, and fails at once when NATS is not reachable

### Look up when an account JWT expires

- **Surfaces:** `cli`
- **Given** a system in operator mode whose account `APP` has a JWT that expires in 12 days
- **When** I run `natslens sql "SELECT account_name, jwt_expires, limit_connections FROM account_details WHERE account_name = 'APP' ORDER BY looked_up_at DESC LIMIT 1"`
- **Then** the row shows `APP` with the JWT's expiry and its connection limit, -1 when the JWT sets none

### See which connections subscribe to a subject

- **Surfaces:** `cli`
- **Given** three `order-service` connections in queue group `order-service` subscribed to `orders.created`, and a collection that read the subscription lists
- **When** I run `natslens sql "SELECT c.name, s.queue, s.subs FROM subscriptions s JOIN connections c USING (epoch, server_id, cid) WHERE s.subject = 'orders.created' AND s.epoch = (SELECT max(epoch) FROM subscription_lists)"`
- **Then** each connection is one row, named `order-service`, with queue `order-service`, from the server it is connected to

### A server stops answering

- **Given** three servers that answered in earlier collections
- **When** one of them answers no request in a collection
- **Then** the collection is stored as partial, with that server's name among the missing ones
- **And** the Servers page shows "2 of 3" answering and "<name> did not answer", and the system switcher says "2 of 3 servers"

### One request fails on one server

- **Surfaces:** `ui`
- **Given** a server that answers every request except HEALTHZ within the window
- **When** I open that server's page
- **Then** the "Monitoring endpoints" section shows HEALTHZ as `missing` and the other endpoints as `ok`
- **And** the header's freshness tooltip names the newest collection as partial

### NATS becomes unreachable

- **Surfaces:** `cli`
- **Given** `natslens run` is collecting
- **When** NATS stops answering for longer than 30 seconds
- **Then** nats-lens stores no collections, records `collector.not_connected`, and keeps serving the UI
- **And** once NATS answers again it records `collector.reconnected` and collects again

## Not supported / out of scope

- **No sampling of individual messages.** nats-lens reads the servers' monitoring reports and advisories, never application messages.
- **No subscription list in every collection.** Subscription lists are read every `subscriptions_interval`, not every collection, and at most `subsz_limit` subscriptions per server; which ones a cut list keeps is up to the server.
- **No client certificate expiry.** CONNZ reports a client certificate's subject and hashes, not when it expires.
- **No collection without the system account.** nats-lens needs a system account user; it cannot collect through an application account alone.
- **No catch-up of missed collections.** A collection that was not taken while nats-lens or NATS was down is not taken later. The gap stays visible ([data-history.md](data-history.md) plans a page for it).
- **No more than `connz_limit` connections per server.** On a server with more connections, the rest are not stored for that collection.
- **One system only.** Watching several systems is planned ([multiple-systems.md](multiple-systems.md)).
