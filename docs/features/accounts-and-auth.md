# Accounts and auth

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [event-capture.md](event-capture.md), [client-checks.md](client-checks.md), [connections.md](connections.md), [monitoring-snapshots.md](monitoring-snapshots.md)

## What it does

Planned; nothing here is built yet. The Accounts and auth pages will show every account with its connections, subscriptions, JetStream use, streams, imports and exports, and when its JWT expires; the failed logins of the last day, grouped by user and source; and the credentials and certificates that expire soon. A client that retries with revoked credentials every minute shows up here before anyone notices that it never connected. All scenarios below describe intended behaviour.

## Behavior and rules

- **Accounts.** One row per account: its readable name and ID, connections, subscriptions, JetStream disk use, streams, imports and exports, and when its JWT expires. The header says whether the system runs in operator mode (names from the account JWTs) or config mode.
- **Failed logins, 24 hours.** From the servers' authentication error events, grouped by user and source address: the reason, the count and the last time.
- **Expiring soon.** User and account JWTs, and the servers' own listener certificates, with their expiry date and the days left. In config mode, without JWTs, only the server certificates are listed. Client certificates cannot be listed: CONNZ reports a client certificate's subject and hashes, not its expiry.
- **Built partly on stored data.** Connections, subscriptions and JetStream use per account come from account STATZ and JSZ, and authentication errors from the events, all stored today. Account limits, imports, exports and JWT expiry need the per-account ACCOUNTZ detail, which nats-lens requests today only to learn an unnamed account's name. Server certificate expiry is in VARZ (`tls_cert_not_after`, for the client listener and for each cluster, gateway, leaf node, websocket and MQTT listener) but not stored yet.

## Scenarios

### Find a client with revoked credentials

- **Surfaces:** `ui`
- **Given** a client whose credentials were revoked retries every minute
- **When** I open Accounts and auth
- **Then** "Failed logins, 24 h" lists its user and address with the reason and about 1,440 attempts

### See a certificate that expires soon

- **Surfaces:** `ui`
- **Given** the client listener certificate of `nats-0` expires in 16 days
- **When** I open Accounts and auth
- **Then** "Expiring soon" lists it with its date and "16 days"

### List accounts from the API

- **Surfaces:** `api`
- **Given** a system in operator mode with three accounts
- **When** a client asks for the accounts
- **Then** it receives each account's name, ID, connections and JetStream use

## Not supported / out of scope

- **No account or user management.** nats-lens does not create, edit or revoke accounts, users or JWTs.
- **No permission violations.** nats-server publishes no event when a client uses a subject it may not; only the server log and the client see it.
- **No secrets shown.** JWTs and credentials are never displayed; nats-lens removes user JWTs from stored payloads.
