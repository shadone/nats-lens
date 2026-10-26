# Account checks

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [accounts-and-auth.md](accounts-and-auth.md), [server-checks.md](server-checks.md), [monitoring-snapshots.md](monitoring-snapshots.md), [design.md](../design.md)

## What it does

The account checks turn what the Accounts page shows into findings, so that an operator hears about an account before it hits a limit or a credential expires: client connections near the connection limit of the account JWT, JetStream disk use near its disk limit, an account JWT that expires soon, and user JWTs of the account's connections that expire soon. An account finding is keyed by the account identifier (the account public key in operator mode); the Findings list, the finding page and the mute dialog name it by the account's readable name when it has one, and its detail starts with that name. There are 4 account checks, `account-001` to `account-004`. The expiry of the servers' own TLS certificates is `server-016` in [server-checks.md](server-checks.md). The general rules of findings, thresholds and mutes are in [checks.md](checks.md).

## The checks

### `account-001` Account near its connection limit

- **Problem:** the account's connections come close to the connection limit of its account JWT. At the limit the servers refuse new client connections of the account, so a service that restarts or scales out cannot connect.
- **Fires when:** the connections, counted as nats-server counts them, are at `warn_ratio` of the limit or more. A server refuses a new client when its own clients and leaf node connections of the account, plus the client connections the other servers report, reach the limit; each server counts only its own leaf nodes. So the count is the client connections summed over the servers that answered in the collection, plus the leaf node connections of the server that holds the most of them, which the detail names: "APP: 93 of 100 connections (93.0%): 85 client connections and 8 leaf node connections on nats-0, which counts its own leaf nodes against the limit."
- **Severity:** warning. Critical at `crit_ratio` of the limit or more.
- **Thresholds:** `warn_ratio` 0.9, `crit_ratio` 1 (at the limit).
- **Left out:** an account without a JWT (config mode), without a limit (-1) or with a limit of 0, an account the collection does not list, and one whose newest lookup is more than 24 hours older than the collection.

### `account-002` Account near its JetStream disk limit

- **Problem:** the account's JetStream file storage comes close to the disk limit of its account JWT. At the limit the servers refuse new messages to the account's file streams.
- **Fires when:** the largest file storage any server reports for the account, as on the Accounts page, is at `warn_ratio` of the limit or more. The limit is the one nats-server enforces, counted over the replicas.
- **Severity:** warning. Critical at `crit_ratio` of the limit or more.
- **Thresholds:** `warn_ratio` 0.9, `crit_ratio` 1 (at the limit).
- **Left out:** an account without a JWT, without a limit (-1), with JetStream disk off (a limit of 0), and as for `account-001`.

### `account-003` Account JWT expires soon

- **Problem:** the account JWT expires soon or has expired. Once it expires the servers stop accepting the account's connections.
- **Fires when:** the JWT expires within `warn_within` of the collection, has expired, or the server reports it expired.
- **Severity:** warning. Critical within `crit_within`, once expired, or when the server reports it expired.
- **Thresholds:** `warn_within` 14d, `crit_within` 3d.
- **Left out:** an account without a JWT, a JWT that never expires, and as for `account-001`.

### `account-004` User JWTs expire soon

- **Problem:** connections of the account use a user JWT that expires soon. When it expires the server closes those connections, and the client cannot connect again with the same credentials.
- **Fires when:** a connection of the collection carries a user JWT that expires within `warn_within` or has expired. One finding per account; the detail names each such user with its soonest expiry and the connections that hold it, soonest first, the first five users and a count of the rest.
- **Severity:** warning. Critical when one expires within `crit_within` or has expired.
- **Thresholds:** `warn_within` 14d, `crit_within` 3d.
- **Left out:** connections without a user JWT, user JWTs that never expire, and connections beyond `connz_limit` per server, which nats-lens does not collect. Tooling connections, nats-lens's own among them, count: their credentials expire too.

## Behavior and rules

- **The Accounts page's data.** The checks read the same numbers the Accounts page shows for the collection: connections summed over the servers, disk use the largest any server reports, and the account's newest lookup at or before the collection. `account-001` adds the leaf node connections of one server, as nats-server counts them; the page's Connections cell and its bar count client connections only, so with leaf nodes the finding can fire below the page's share.
- **A lookup counts for 24 hours.** Limits and the account JWT come from an ACCOUNTZ lookup, each account at most every 10 minutes. A lookup more than 24 hours older than the collection, which the page reads as "not looked up yet", fires no `account-001`, `account-002` or `account-003` finding.
- **Expiry is measured from the collection.** "Within 14 days" counts from the start of the collection, in UTC, whatever the time zone of the host.
- **Expiry warnings never head an outage.** `account-003`, `account-004` and `server-016` have the category `expiry`: their warnings rank after every other warning, in the Findings list, the systems list's and the Overview's worst finding and the cause cards, so `example-global` is headed by `nats-us-4`'s outage, not by a JWT that expires in 5 days. They still fire and alert as warnings. A critical expiry, expired or within 3 days, ranks as any critical finding. `account-001` and `account-002` rank as any other finding.
- **The value says how close.** `account-001` and `account-002` return the share of the limit (0.95 is 95%); `account-003` and `account-004` the days until the soonest expiry, negative once expired.

## Scenarios

### An account fills its connection limit

- **Given** the account `APP`, whose JWT allows 2,000 connections, with 1,200 on `nats-0` and 800 on `nats-1`
- **When** nats-lens collects
- **Then** `account-001` fires as critical for `APP`, with the detail "APP: 2000 of 2000 client connections (100.0%); at the limit, new client connections are refused."

### An account's disk use nears its limit

- **Given** an account whose JWT allows 100 GiB of JetStream disk, and the servers report 95 GiB of file storage for it
- **When** nats-lens collects
- **Then** `account-002` fires as a warning, with "JetStream file storage 95.0 GiB of 100.0 GiB (95.0%)."

### An account JWT expires in 12 days

- **Given** `example-global`'s `SUPPLIERS`, whose account JWT expires in 12 days
- **When** nats-lens collects
- **Then** `account-003` fires as a warning, with "SUPPLIERS: account JWT expires" followed by the date and "in 12 days"
- **And** it turns critical 3 days before the expiry

### A user's credentials run out

- **Given** three connections of the user `pos-terminal` in `APP`, whose user JWT expires in 5 days
- **When** nats-lens collects
- **Then** `account-004` fires as a warning for `APP`, naming `pos-terminal`, its expiry and "(3 connections)"

### A lookup is too old

- **Given** an account last looked up 25 hours before the collection, with a connection limit it exceeds
- **When** nats-lens collects
- **Then** no account finding fires for it, as the page reads it as not looked up yet

### Trying the checks from the command line

- **Surfaces:** `cli`
- **Given** a stopped database
- **When** an operator runs `natslens checks run -data-dir DIR`
- **Then** the account checks' failing accounts are printed with the other checks', and nothing is stored

## Not supported / out of scope

- **No other account limits.** The leaf node limit, subscription, payload, JetStream memory, stream and consumer limits are not checked.
- **No config-mode limits.** Without an account JWT nats-lens knows no account limit.
- **No bearer user JWTs.** The server reports a bearer JWT empty, so its expiry is not known.
- **No link to the Accounts page.** An account finding names the account; it does not open the Accounts page.
- **The Accounts tile does not follow these checks.** The Overview's Accounts tile has no checks behind it; see [overview-page.md](overview-page.md).
