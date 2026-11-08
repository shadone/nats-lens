# Accounts and auth

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [account-checks.md](account-checks.md), [server-checks.md](server-checks.md), [event-capture.md](event-capture.md), [client-checks.md](client-checks.md), [connections.md](connections.md), [monitoring-snapshots.md](monitoring-snapshots.md), [navigation.md](navigation.md), [demo-mode.md](demo-mode.md), [design.md](../design.md)

## What it does

The Accounts page, under Cluster in the sidebar, shows a system's accounts in the newest collection of the range: their connections, subscriptions, JetStream disk use and streams against the limits their account JWTs set, their imports and exports, and when their JWTs expire. Two cards show the failed logins of the 24 hours before that collection, grouped by user, address and reason, and the certificates and JWTs that have expired or expire within 30 days. A client that retries with revoked credentials every minute shows up here before anyone notices that it never connected. `GetAccountsAndAuth` serves the same data, in the demo too.

## Behavior and rules

- **First visit.** The page reads data for the current scope while its view loads. The shell stays available, with loading and reload recovery described in [navigation](navigation.md).

- **One collection.** The page reads the newest collection at or before the end of the range, and says which: "Operator mode · 3 accounts · names from the account JWTs, in the collection of 14:30:07 UTC." A range without a collection shows "No collection in this range." and no cards. A live range refreshes every collect interval.
- **Operator and config mode.** The system runs in operator mode when an account identifier of the collection is an account public key; then names come from the account JWTs. Otherwise it runs in config mode and names come from the server configuration. In config mode there are no JWTs: no limits, an en dash for JWT expiry, and only server certificates under "Expiring soon".
- **The accounts.** One row per account the servers report, the system account last and the others by connections as the Connections cell counts them (proposed, not yet confirmed), most first, then by name; at most 500, with "The first 500 of 612 accounts are listed." when more exist. An account's name opens the Connections list scoped to it; an account public key is shown shortened under the name. Connections, leaf nodes and subscriptions are summed over the servers; of two processes of one name in the collection, the old one shutting down and the new one up, both count, as nats-server counts both until the old one shuts down. JetStream disk use is the largest any server reports; streams are counted once each, whatever their replicas. A value no server reported is an en dash, never 0.
- **Connections as nats-server counts them.** A server refuses a new client of the account once every server's client connections plus its own leaf node connections reach the limit, so the Connections cell counts the client connections plus the leaf node connections of the server that holds the most, as `account-001` does, and names those leaf nodes under the count: "93 of 100" with "includes 8 leaf nodes on nats-0 · 11 leaf nodes in all". The second part appears only when other servers hold leaf nodes too, which are not counted; without leaf nodes there is no line.
- **Limits.** The connection and JetStream disk limits come from the account JWT: "1,142 of 2,000", "0 of unlimited", or the count alone without a known JWT. A bar shows the share of a limit above 0, in the accent colour below 80%, the warning colour from 80% and the critical colour over the limit, with the share in words under it from 80% ("82% of the limit", "150% of the limit, over the limit"). The connection bar is critical already at the limit, where nats-server refuses new clients and `account-001` is critical, and reads "100% of the limit, at the limit" there (the owner's decision of 2026-10-27). Config mode shows no limits: the JetStream reservation the servers report is the sum of the streams' reservations, not an account limit.
- **JetStream off or memory only.** An account JWT whose JetStream disk and memory limits are both 0 disables JetStream for the account, and the disk cell reads "JetStream off". One that allows memory but no file storage reads "memory only".
- **Imports and exports.** "2 imports · 1 export", "none", or "system account". Imports leave out those nats-server adds to every account. An account with no connections, no leaf nodes and no streams now adds "idle now"; a count no server reported does not make an account idle.
- **Not looked up yet.** Limits, imports, exports and the JWT come from an ACCOUNTZ lookup, each account at most every 10 minutes. An account whose newest lookup is more than 24 hours older than the collection, or that was never looked up, reads "not looked up yet" in Imports / exports, and its limits and JWT expiry are left out.
- **JWT expires.** The date, "never", "expired" in the critical colour, or a date within 30 days in the warning colour with "in 12 days" under it.
- **Failed logins, 24 h.** The servers' authentication error events from the 24 hours before the collection, or from the first collection when the history is shorter, grouped by user, address and reason, with the count and the last time; most first, at most 100 groups. A client that gave no user reads "(no user)"; the account an event names is shown under the user. A group that averages 60 or more attempts an hour is in the warning colour and says "about 60 an hour" under the user ("70 since 14:00 UTC" when the history is under an hour). A shorter history is named: "The history starts at 17 Oct 11:30 UTC, so fewer than 24 hours are counted."
- **Events not captured.** When no event was stored up to the collection, the card says only "Failed logins are not shown: no event was stored up to this collection, so event capture was not running for this system then.", never "No failed logins". When capture ran but stopped for part of the window, "Event capture stopped for part of these 24 hours, so the counts can be low."
- **Expiring soon.** Each server's listener certificates (client, cluster, gateway, leaf node, websocket, MQTT), the account JWTs, and the user JWTs of the collection's connections by account and user, with the soonest expiry and how many connections hold it; listed when expired or within 30 days, expired first, then the soonest, at most 100. A row reads "nats-eu-0 certificate, client listener", "SUPPLIERS account JWT" or "pos-terminal user JWT"; "In" is in the warning colour, or critical when expired. A server that did not answer VARZ in the collection keeps its certificates from its newest collection within 24 hours, with "read at 13:20 UTC; no answer from the server since" under the row. A line counts the rest: "71 more expire later; 3 never expire."
- **Findings from the same data.** The account checks fire on what the page shows: `account-001` and `account-002` from 90% of the connection or disk limit, `account-003` on an account JWT and `account-004` on user JWTs that expire within 14 days; `server-016` on listener certificates within 14 days, read with the page's 24-hour rule for a server that did not answer. The connection bar counts as `account-001` does, so the finding fires once the bar reaches 90%. The page's bars warn earlier, from 80%, and "Expiring soon" lists 30 days; see [account-checks.md](account-checks.md) and [server-checks.md](server-checks.md).
- **Phone width.** The two cards stack, and each table scrolls inside its card.
- **Nothing secret is kept.** nats-lens reads a JWT's claims for the expiry and limits and drops the token: the store holds no JWT, signing key or seed, and user JWTs are removed from stored event payloads.

## Scenarios

### Find a client with revoked credentials

- **Surfaces:** `ui`
- **Given** `example-global`, where `scale-terminal-07` at `10.20.4.31` retries every minute with revoked credentials
- **When** I open Accounts
- **Then** "Failed logins, 24 h" lists `scale-terminal-07` first, with `10.20.4.31`, "User Authentication Revoked" and 1,440 attempts, in the warning colour and with "about 60 an hour" under the user

### See a certificate that expires soon

- **Surfaces:** `ui`
- **Given** the client listener certificate of `nats-eu-0` expires in 16 days
- **When** I open Accounts
- **Then** "Expiring soon" lists "nats-eu-0 certificate, client listener" with its date and "16 days" in the warning colour
- **And** the line under the table counts the certificates and JWTs that expire later or never

### See an account near its limits

- **Surfaces:** `ui`
- **Given** an account in operator mode whose JWT allows 2,000 connections and which holds 1,700
- **When** I open Accounts
- **Then** its Connections cell reads "1,700 of 2,000" with the bar in the warning colour and "85% of the limit" under it

### Leaf nodes count against the connection limit

- **Surfaces:** `ui`
- **Given** an account whose JWT allows 100 connections, with 85 client connections over the servers, 8 leaf node connections on `nats-0` and 3 on `nats-1`
- **When** I open Accounts
- **Then** its Connections cell reads "93 of 100" with "93% of the limit" and "includes 8 leaf nodes on nats-0 · 11 leaf nodes in all" under it
- **And** `account-001` fires a warning for the same 93 of 100

### An account at its connection limit

- **Surfaces:** `ui`
- **Given** an account whose JWT allows 50 connections and which holds 50
- **When** I open Accounts
- **Then** its Connections cell reads "50 of 50" in the critical colour, with the bar critical and "100% of the limit, at the limit" under it
- **And** a JetStream disk use exactly at its limit stays in the warning colour with "100% of the limit"

### An account whose JWT disables JetStream

- **Surfaces:** `ui`
- **Given** `example-global`'s `SUPPLIERS`, whose JWT sets JetStream disk and memory limits of 0 and which has no connections or streams
- **When** I open Accounts
- **Then** its JetStream disk reads "JetStream off" and its Imports / exports "1 export · idle now"

### A system in config mode

- **Surfaces:** `ui`
- **Given** `example-prod`, whose accounts are configured in the server configuration
- **When** I open Accounts
- **Then** the line reads "Config mode · 1 account · names from the server configuration", and `APP` shows its connections without a limit and an en dash under JWT expires
- **And** "Expiring soon" lists server certificates only and says so

### Events were not captured

- **Surfaces:** `ui`
- **Given** a system for which no event was ever stored, because event capture is off
- **When** I open Accounts
- **Then** "Failed logins, 24 h" says that failed logins are not shown because event capture was not running, and does not say there were none

### Read accounts and auth from the API

- **Surfaces:** `api`
- **Given** `example-global` in the demo
- **When** a client calls `GetAccountsAndAuth` for the last 24 hours
- **Then** it receives operator mode, the three accounts with their connections, the leaf nodes counted against the limit (`limit_leafnodes`, `limit_leafnodes_server`), limits and `jetstream_enabled`, the failed-login groups with their counts and last times, `events_captured`, and the expiring certificates and JWTs with the counts of the rest

## Not supported / out of scope

- **No account or user management.** nats-lens does not create, edit or revoke accounts, users or JWTs.
- **No permission violations.** nats-server publishes no event when a client uses a subject it may not; only the server log and the client see it.
- **No client certificates.** CONNZ reports a client certificate's subject and hashes, not its expiry.
- **No bearer user JWTs.** The server reports a bearer JWT empty, so its expiry is not known and it is not listed.
- **No signing key rotation.** A JWT carries no rotation date.
- **No "unused for 30 days".** The page says "idle now" from the shown collection only; it does not read account history.
- **No secrets shown.** JWTs and credentials are never displayed or stored.
