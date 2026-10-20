# Accounts and auth build plan

Working plan for the Accounts and auth page (`docs/features/accounts-and-auth.md`, board `Accounts.dc.html` of the design canvas). Everything here was proposed by the planning session of 2026-10-20 and is not yet confirmed by the owner. When a task lands, its lasting content moves into `docs/features/accounts-and-auth.md` and `docs/design.md`; this file then only tracks status.

The board's sample names (`PARTNER`, `REGISTRY`, `old-sdk-user`, `sdk-service`) come from a sketch. Use the example shop of `inventory.example.yaml` and the demo's systems instead.

## Status

| Task | Status |
|---|---|
| T1 Collector and store | built on `feat/accounts-page`, awaiting review |
| T2 API and demo | built on `feat/accounts-api`, awaiting review |
| T3 UI | not started |
| T4 Feature docs and audit | not started |

## Rules for every step

- Never keep a JWT or a credential. The collector reads what it needs from a JWT's claims (expiry, issue time, limits) and drops the token; the store holds no JWT, no signing key and no seed. User JWTs are already removed from stored event payloads.
- A value a server did not report is unset, never 0. "No limit" (a JWT limit of -1, or no JWT) and "not looked up yet" are different states and the page says which.
- The page shows the newest collection at or before the end of the page range (the epoch rule of `GetPlacement`), and failed logins over the 24 hours before that collection.
- nats-server publishes no event for a permission violation; the page does not claim to show them.

## What backs each part

| Part | Source | Rule |
|---|---|---|
| Mode | the accounts of the epoch | Operator mode when an account identifier is an account public key (`isAccountKey` of `internal/collect`: 56 characters starting with `A`), else config mode. |
| Accounts | `account_stats` of the epoch, `accounts` | One row per account any server reported in the epoch: connections, leaf nodes and subscriptions summed over the servers (account STATZ gives each server's share); JetStream file and memory use the largest any server reports (JSZ gives the account total on each server). |
| Streams | `stream_stats` (or `streams` with the epoch's rows) | Distinct streams of the account in the epoch, not the sum of `js_streams`, which counts replicas per server. |
| Limits, imports, exports, JWT | new table `account_details` (T1) | The newest lookup of the account at or before the epoch, if it is at most 24 hours older than the epoch; older or none: unset, read as "not looked up yet". |
| Connection and disk limits | `account_details` JWT limits, else `account_stats.js_reserved_storage` | "12 of 2,000" or "12 of unlimited"; in config mode the JetStream reservation is the disk limit when it is set, and the connection limit is unset. |
| Idle account | `account_stats` of the epoch | No connections, no leaf nodes and no streams now: "idle now". (The board's "unused for 30 days" needs history the page does not read; a leftover.) |
| Failed logins | `events` with `kind = 'client.auth_error'` | Grouped by (`client_user`, `client_host`, `reason`): count, last time, the account when the event names one. The 24 hours before the epoch, ending at the epoch. |
| Capture gaps | `events` of source `collector` (`collector.gap`, lost connections), the rule of Replay | When event capture was not running for part of the 24 hours, the counts can be low, and the page says so. |
| Server certificates | new `server_stats` columns (T1) | Each server's listener certificates of the epoch: client, cluster, gateway, leaf node, websocket, MQTT. |
| Account JWTs | `account_details` | `jwt_expires` of the newest lookup. |
| User JWTs | new `connections` columns (T1) | Per (account, user) of the epoch's connections that carry a user JWT: the soonest expiry and how many connections hold it. Bearer JWTs are redacted by the server, so their expiry is not reported. |

Order of the accounts table: the system account last; the others by connections, descending, then by name. Failed logins: count, descending, then the last time, newest first. Expiring: expired first, then the soonest.

"Expiring soon" lists what has expired or expires within 30 days; one line under the table counts the rest ("4 more expire later; 9 never expire"). Rows within 30 days are in the warning colour, expired rows in the critical colour. In config mode only certificates are listed.

Not backed, so not shown: permission violations; client certificates (CONNZ gives a client certificate's subject and hashes, not its expiry); bearer user JWTs; signing key rotation (the board's "signing key rotation: not set"; a JWT has no rotation date); "unused for 30 days" (a leftover).

## T1 Collector and store

- **Account details.** The ACCOUNTZ lookup of `internal/collect/accountnames.go` today asks only for accounts without a readable name. It becomes a lookup of every account ACCOUNTZ lists, in both modes: an account not looked up yet first, then the one asked longest ago, each at most every 10 minutes, at most 32 per epoch, 8 in flight, in the same window as today. A lookup reads from the first reply that holds the account: the name (as today), whether it is the system account, `expired`, the counts of imports and exports, and from the decoded JWT (`decoded_jwt`) the issue time, the expiry and the limits (connections, leaf nodes, subscriptions, data, payload, JetStream disk and memory, streams, consumers; tiered JetStream limits summed over the tiers, or the R1 tier alone if summing is wrong; the implementer decides and says why). The JWT itself is never read into a kept value.
- **Storage.** Migration `0027_account_details.sql`: table `account_details` (`account`, `looked_up_at`, `epoch`, `server_id` of the reply, `account_name`, `is_system`, `has_jwt`, `jwt_issued_at`, `jwt_expires`, `expired`, `imports`, `exports`, and the limit columns as BIGINT, NULL when there is no JWT, -1 for no limit as the JWT says), one row per answered lookup, with comments on every column. It follows `account_stats` in retention and in the hourly archive. The snapshot carries the lookups; `write_accounts.go` writes them.
- **Server certificates.** Migration (same file or `0028`): `server_stats` columns `tls_cert_not_after`, `cluster_tls_cert_not_after`, `gateway_tls_cert_not_after`, `leafnode_tls_cert_not_after`, `websocket_tls_cert_not_after`, `mqtt_tls_cert_not_after` (TIMESTAMP, NULL when the listener has no TLS or VARZ did not answer), from VARZ `tls_cert_not_after` and the same field of each listener block (nats-server 2.15 has them; an older server omits them).
- **User JWTs.** CONNZ is asked with `auth`, so each connection's `jwt` arrives (bearer JWTs come redacted). The collector reads the claims segment without verifying it (the server verified it at connect; nats-lens only displays) and keeps `connections.user_jwt` (BOOLEAN, a JWT was reported) and `connections.jwt_expires` (TIMESTAMP, NULL when it has none). A JWT that does not decode leaves `user_jwt` true and `jwt_expires` NULL, and is logged at debug level without its content.
- **Checks.** No new check in this task.
- **Docs.** `docs/design.md` (what is collected from ACCOUNTZ and how often, never keeping a JWT), `monitoring-snapshots.md` and the SQL access table list if it names tables.
- **Tests.** Lookups ordering and caps; the claims read from a sample `decoded_jwt` (expiry, limits, unlimited, no JWT, a config-mode account); a JWT that does not decode; the VARZ certificate fields; the migration applies on a store with data (`ALTER` on an archived table keeps the archive readable); an in-process cluster test where available.

### Where T1 departed from this plan

Each point was decided in the session that built T1: proposed, not yet confirmed.

- **Two migrations.** `0027_account_details.sql` creates `account_details`; `0028_cert_and_jwt_expiry.sql` adds the `server_stats` certificate columns and the `connections` JWT columns. `account_details` also has a unique index on (`epoch`, `account`), as the small tables of 0012 do.
- **Limit column names.** `limit_connections`, `limit_leafnodes`, `limit_subscriptions`, `limit_data`, `limit_payload`, `limit_js_disk`, `limit_js_memory`, `limit_js_streams`, `limit_js_consumers`. A limit the JWT leaves out reads as 0, which is what the JWT means: the JWT library omits zero values, and 0 JetStream disk and memory means JetStream is not enabled for the account.
- **Tiered JetStream limits are summed.** The account can use every tier at once, and a tier's disk and memory limits count replicated bytes as the account's usage in JSZ does; the R1 tier alone would understate an account with R3 streams. A tier with -1 makes the sum -1. With tiers set, the account-wide JetStream limits are ignored, as nats-server ignores them.
- **Imports leave out what the server adds.** nats-server gives every account service imports from the system account (subjects under `$SYS.REQ.`: the CONNZ and STATZ pings and the user info request) and a JetStream account an import of `$JS.API.>`; counted as they are, every config-mode account would show 4 or 5 imports. `imports` counts the others. `exports` counts all, so the system account's count includes the exports nats-server adds to it; the page shows "system account" there anyway.
- **`account_name`.** The name tag, else the JWT's name; in config mode the identifier; NULL for an operator account with neither.
- **Name lookups.** Looking up every account replaces the old name-only rules: the 24-hour recheck of a learnt name and "unnamed accounts first" are gone, and a name is now refreshed with the details, at most every 10 minutes.
- **CONNZ needed no change.** `ConnzOptions.Username` is the `auth` option and was already set. The user JWT is removed from the CONNZ reply as soon as it is applied, so no snapshot holds it; the snapshot carries only the expiry per connection ID (`ServerView.UserJWTs`). A bearer JWT arrives empty, so its connection has `user_jwt` false. Rows written before 0028 have `user_jwt` NULL.
- **Test dependencies.** `github.com/nats-io/jwt/v2` and `github.com/nats-io/nkeys` became direct dependencies in `go.mod`, used only by the operator-mode server test in `internal/collect`.
- **Docs beyond the list.** `docs/features/client-checks.md` (the account connection limit is now stored), the coverage line of monitoring snapshots in `docs/features/README.md`, and the "Built partly on stored data" and "No secrets shown" text of `accounts-and-auth.md`, whose status stays planned.

## T2 API and demo

`proto/natslens/v1/accounts.proto`, RPC `GetAccountsAndAuth(GetAccountsAndAuthRequest) returns (GetAccountsAndAuthResponse)`.

- Request: `system`, `range`.
- Response: `epoch`, `epoch_at`, `operator_mode` (bool), `repeated AccountRow accounts`, `int32 accounts_total`, `repeated FailedLogin failed_logins`, `int32 failed_login_groups_total`, `int64 failed_logins_total`, `failed_logins_window_start`, `bool capture_gaps`, `repeated Expiring expiring`, `int32 expire_later`, `int32 never_expire`.
- `AccountRow`: `account`, `account_name`, `is_system`, `optional int32 connections`, `optional int32 leafnodes`, `optional int64 subscriptions`, `optional int64 js_storage`, `optional int64 js_memory`, `optional int32 streams`, `optional int64 conn_limit` (-1 no limit), `optional int64 disk_limit` (-1 no limit), `optional int32 imports`, `optional int32 exports`, `bool details_known`, `optional google.protobuf.Timestamp jwt_expires`, `bool has_jwt`, `bool expired`, `optional google.protobuf.Timestamp details_at`.
- `FailedLogin`: `user` (empty for none), `host`, `reason`, `account`, `account_name`, `int64 count`, `last`.
- `Expiring`: `kind` (enum: server certificate with its listener, account JWT, user JWT), `subject` (server name and listener, account, or account and user), `expires`, `expired` (bool), `int32 connections` for a user JWT.
- At most 500 accounts (sorted as above) with the total, at most 100 failed-login groups with the totals, at most 100 expiring rows.
- Rules in `internal/api/accounts.go` (`AccountsFacts`, `FinishAccounts`), shared by the store (`storeprov/accounts_page.go`, SQL aggregates, no per-event reads into Go) and the demo, as `FinishTopology` is.
- Handler: the common range validation and `QueryTimeout` through `call()`. No epoch in the range: the empty response with `epoch` 0.
- **Demo.** `example-global` runs in operator mode: `APP` keeps its readable name everywhere but its identifier becomes an account public key on every page (`ListAccounts`, events, connections, streams), with the system account `SYS` and an idle account `SUPPLIERS` (no connections, an export of `inventory.changes`, its JWT expiring in 12 days); `APP` has limits (2,000 connections, 500 GiB disk) and a JWT without expiry. Failed logins: user `label-printer` from `10.20.4.31`, "User Authentication Revoked", every minute (about 1,440 in 24 hours); no user from `203.0.113.12`, "Authentication Timeout", 96 times. `nats-eu-0`'s client listener certificate expires in 16 days; the `pos-terminal` user JWT in 5 days on 3 connections. If switching `APP`'s identifier is too wide a change, the implementer keeps `APP` and gives only `SUPPLIERS` a key, and says so. `example-prod`: config mode, `APP` only, server certificates more than 30 days out. `example-staging`: as its scenario allows. The demo's auth errors appear on the Events page too.
- Conformance rows in `apitest`: operator mode with an account JWT expiring and one without expiry; config mode with certificates only; failed logins grouped with the count and last time; an account without details reads "not looked up yet"; the store fixture and the demo pass alike.
- Docs: `docs/design.md` (the RPC, limits, the 30-day window), `demo-mode.md`.

### Where T2 departed from this plan

Each point was decided in the session that built T2: proposed, not yet confirmed.

- **APP keeps its identifier.** Switching `APP` to an account public key on every demo page would change every stream, consumer and finding key of `example-global` (`fake.Account` is one constant that every scenario and many tests share), so only `SUPPLIERS` has a key (`fake.SuppliersAccount`); `APP` and `SYS` keep their names as identifiers. Operator mode reads true because one identifier is a key, the rule of the table above.
- **No disk limit in config mode.** `account_stats.js_reserved_storage` is the sum of the streams' `max_bytes` reservations (nats-server's `reservedStorage`), not the account's JetStream limit, so the plan's "the JetStream reservation is the disk limit" would show a stream setting as a limit. `disk_limit` is set from the account JWT only; config mode leaves it unset. Config-mode account limits would need JetStream account info, which nothing collects.
- **Expiring rows have fields, not one subject.** `Expiring` carries `server` and `listener` for a certificate, `account`, `account_name` and `user` for a JWT, in place of a composed `subject`, so the account name fills as in every response and the UI words the row. `expiring_total` counts the rows before the limit of 100, as the other lists have a total.
- **Accounts are `ListAccounts`' accounts.** The rows are the accounts the `accounts` table holds for the epoch, with `account_stats` joined, so this page and `ListAccounts` always list the same accounts; a conformance row checks it in every system. An account in `account_stats` alone cannot occur: the writer adds every reported account to `accounts`.
- **Limits from the JWT only when it is known.** `conn_limit` and `disk_limit` are set only for an account whose lookup is known and carries a JWT; `imports` and `exports` for any known lookup.
- **User JWTs counted.** A user JWT group is (account, user) of the epoch's connections with `user_jwt` true; its expiry is the soonest, `connections` the connections whose JWT expires then. A user whose connections report no expiry counts in `never_expire`; a JWT that did not decode is counted there too, as the store cannot tell the two apart.
- **Capture gaps include "never captured".** `capture_gaps` is also true when no event at all is stored up to the epoch, as Replay's "events not captured".
- **Failed-login window.** It ends at the epoch and starts 24 hours before it, or at the first collection when the history is shorter; events at its start count.
- **Demo details.** `example-global`'s timeouts from `203.0.113.12` come 8 at a time every 2 hours (96 a day), not every 15 minutes: one every 15 minutes is more than the 10-minute fold gap apart, so the Events page would show each as a row of its own, about 100 more rows. The test of the Events page's readable view now allows 135 rows instead of 120, for the label-printer row and the bursts. `SYS` has 0 connections, as the demo's connections list holds `APP`'s only. Three `checkout-service` connections on `nats-eu-0` to `nats-eu-2` authenticate as `pos-terminal`; every other connection as `app`, whose user JWT has no expiry. The other certificates of `example-global` expire in 75 days, `example-prod`'s in 58. `example-staging` has no lookup and no certificates.

## T3 UI

- Route `/accounts`, `web/src/pages/Accounts.tsx`; the `NAV` entry loses `planned`. Header "Accounts"; the line "Operator mode · 3 accounts · names from the account JWTs" or "Config mode · 2 accounts · names from the server configuration".
- Accounts table: Account (name, identifier in small mono under it, shortened in operator mode), Connections (with a bar against the limit when one is set), Subscriptions, JetStream disk (with a bar against the limit, warning colour at 80% or more), Streams, Imports / exports ("2 imports · 1 export", "none", "system account", "idle now"), JWT expires (date, "never", "expired" in critical, "not looked up yet", or an en dash in config mode).
- Two cards: "Failed logins, 24 h" (User, From, Reason, Count, Last; a row in the warning colour when it averages 60 or more attempts an hour over the window, as the board's first row), the board's sentence about revoked credentials, and the capture-gap line; "Expiring soon" (What, Expires, In) with the count line and the line on config mode.
- Empty states: no failed logins ("No failed logins in these 24 hours."), nothing expiring ("Nothing expires within 30 days.").
- Phone width: the cards stack; the accounts table scrolls inside its card.
- Data: `requests.getAccountsAndAuth`, `api/accounts.ts`, `useAccountsAndAuth()` keyed by system and range, refetched with the page's live interval.
- Docs: `navigation.md`, the UI section of `design.md`.

## T4 Feature docs and audit

`accounts-and-auth.md` to `Status: shipped` (or `partial`) with its scenarios and Not supported list, `docs/features/README.md` table and coverage map, `monitoring-snapshots.md` and `event-capture.md` where they describe what is collected, the audit, leftovers into `docs/plans/next.md`.

## Leftovers known now

- "Unused for 30 days, candidate to remove" from account history.
- A check for a certificate or account JWT that expires within 14 days.
- Account limits reached (connections at the limit, disk near the reservation) as findings.
