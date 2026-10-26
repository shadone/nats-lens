# nats-lens feature documentation

What nats-lens does **today**, and what it is meant to do next. Shipped docs describe present-tense, observable behaviour. Planned docs describe intended behaviour and say so in their `Status:` line. Each file documents one capability. This is not a plan, not a design spec, and not developer internals: the decisions and their reasons are in [design.md](../design.md), and the reference for configuration keys, metrics and tables is the project [README](../../README.md).

Each doc is two layers: a skimmable `What it does` header plus a Gherkin-style `Scenarios` section. It serves a reader browsing features, an agent reasoning about correct behaviour, and (later) an author deriving end-to-end test scenarios.

> **This directory is the authoritative feature index.** The per-capability docs here are the source of truth for what each feature does. The coverage map at the bottom lists every capability by area and flags the partial and planned ones.

Start with [overview.md](overview.md): what nats-lens is, which problem it solves, and its nouns.

## How to add a feature doc

1. Copy `_TEMPLATE.md` to `<capability>.md` (kebab-case, named for the capability, not the page or button it lives on).
2. Document shipped behaviour as shipped only after you verify each scenario against the source under `cmd/`, `internal/`, `checks/`, `web/src/` and `proto/`. Mark behaviour that is partly built `Status: partial — <what is missing>`, and list what is not built under `Planned` or `Not supported`.
3. A capability that is designed but not built gets its own doc with `Status: planned`. Its scenarios describe the intended behaviour, and its `What it does` says that they are planned. The feature-docs audit script warns `bad-status` on every `Status: planned` doc; those warnings are expected, and every other finding is fixed.
4. Tag surfaces from the legend below. The doc-level `Surfaces:` line must equal the union of the scenarios' surface tags: every surface listed there has to appear in at least one scenario.
5. These are product-feature descriptions with a little technical detail, not implementation docs. **Do not link to source code** anywhere, including `Related:`. Refer to symbols as inline code (`` `checks.Engine` ``). Link only design docs and sibling feature docs, as clickable relative Markdown links.
6. Add a row to the **Capabilities** table **and** an entry to the **coverage by area** map. The two sections drift independently, so updating only one leaves the index misleading.
7. Use the terms below with one meaning each. Write short sentences in the active voice that name who acts. Quote text that nats-lens prints exactly.

## Keeping docs honest

These docs drift as the code changes. Re-verify a doc against the source after you change a documented feature, and before you rely on a doc you did not write. Every user-facing change updates the capability doc and both README sections in the same change.

Run the mechanical checks with:

```bash
python3 <path to the feature-docs skill>/scripts/audit_feature_docs.py docs/features/ --map-refs require
```

## Surface-tag legend

| Tag | Surface |
|---|---|
| `ui` | The web UI that `natslens run` and `natslens demo` serve at `/`. |
| `api` | The Connect API `natslens.v1.LensService` under `/api/`, and the plain HTTP endpoints beside it (`/metrics`, `/healthz`, `/readyz`). |
| `cli` | The `natslens` commands (`run`, `once`, `sql`, `checks`, `demo`), their flags and the configuration file. |

## Terms

| Term | Meaning |
|---|---|
| system | One NATS deployment with its own system account: a single server, a cluster, or a supercluster joined by gateways. One nats-lens can watch several systems, each with its own collector and database; the UI names the one a page shows in `?system=`. |
| server | One `nats-server` process, known by its stable server name. A restart gives the server a new server ID but keeps its name. |
| cluster | The servers joined by routes under one cluster name. |
| account | A NATS account, as the servers report it (the account ID in operator mode), shown with its readable name when nats-lens knows it. |
| epoch | One snapshot of the whole system: every server's monitoring reports, taken once per collect interval and stored in one transaction. These docs and the UI call it a **collection** (for example "the newest collection of the range"); the API, the database and the configuration call it an epoch (`Freshness.epoch`, the `epochs` table). |
| asset | A JetStream object a server holds: a stream, a consumer, a KV bucket or an object store. A KV bucket is the stream `KV_<bucket>` and an object store the stream `OBJ_<bucket>`. |
| check | One rule that nats-lens evaluates after every epoch, with a code such as `stream-001`, a severity and thresholds. |
| finding | One check failing for one entity (a server, stream, consumer, client or the whole system). A finding fires, can change severity, and resolves. |
| event | One thing that happened at a moment: a NATS advisory or system event, a Kubernetes change, a finding transition, or a record of the collector itself. |
| inventory | The optional file that lists the assets each account must have, with the settings that must have a given value. |
| mute | A decision to hide one check's findings, for one entity or for every entity, with a reason and an end time. |
| tooling connection | A client connection that is not an application: nats-lens's own connections, and by default clients connected from a loopback address such as a port-forwarded `nats` CLI. |

## Capabilities

| Capability | Surfaces | Status |
|---|---|---|
| [What nats-lens is](overview.md) | `ui`, `api`, `cli` | shipped |
| [Monitoring snapshots](monitoring-snapshots.md) | `ui`, `cli` | shipped |
| [Event capture](event-capture.md) | `ui`, `api`, `cli` | shipped |
| [Kubernetes context](kubernetes-context.md) | `ui`, `cli` | shipped |
| [History and archive](history-and-archive.md) | `ui`, `cli` | shipped |
| [Checks and findings](checks.md) | `ui`, `api`, `cli` | partial — accepting a finding is built for one entity; related entities, a check everywhere and a review list are planned |
| [Server checks](server-checks.md) | `ui`, `cli` | shipped |
| [Stream checks](stream-checks.md) | `ui`, `cli` | shipped |
| [Consumer checks](consumer-checks.md) | `ui`, `cli` | shipped |
| [Client checks](client-checks.md) | `ui`, `cli` | shipped |
| [System checks](system-checks.md) | `ui`, `cli` | shipped |
| [Inventory](inventory.md) | `ui`, `cli` | shipped |
| [Navigation, time range and theme](navigation.md) | `ui`, `api` | shipped |
| [Overview page](overview-page.md) | `ui`, `api` | shipped — tile links, cause evidence, a shared hover and more lanes are planned |
| [Servers](servers.md) | `ui`, `api` | shipped |
| [Streams](streams.md) | `ui`, `api` | shipped — replica placement is in the API only |
| [Consumers](consumers.md) | `ui`, `api` | shipped — delivery rate and busiest-first order are in the API only |
| [Connections](connections.md) | `ui`, `api` | shipped |
| [Events](events.md) | `ui`, `api` | shipped |
| [Triage and mutes](triage-and-mutes.md) | `ui`, `api` | shipped |
| [Metrics and alerting](metrics-and-alerting.md) | `api` | shipped |
| [Running nats-lens](running-nats-lens.md) | `api`, `cli` | shipped |
| [SQL access](sql-access.md) | `cli` | shipped |
| [Demo mode](demo-mode.md) | `ui`, `api`, `cli` | shipped |
| [Multiple systems and the Fleet view](multiple-systems.md) | `ui`, `api`, `cli` | partial — a cluster switcher inside a supercluster is planned |
| [Placement](placement.md) | `ui`, `api` | partial — how long a state other than a leader's has lasted, and a playbook link, are not built |
| [Replay](replay.md) | `ui`, `api` | partial — "Replay this window" links and a replica catch-up step are not built |
| [Topology](topology.md) | `ui`, `api` | shipped |
| [Applications](applications.md) | `ui`, `api` | shipped |
| [Capacity and retention](capacity.md) | `ui`, `api` | shipped |
| [Subjects](subjects.md) | `ui`, `api` | shipped |
| [Accounts and auth](accounts-and-auth.md) | `ui`, `api` | shipped |
| [KV and object stores](kv-and-object-stores.md) | `ui`, `api` | shipped |
| [Micro services](micro-services.md) | `ui`, `api` | shipped |
| [Command bar](command-bar.md) | `ui` | partial — search by name is shipped; findings, events and prefixes are planned |
| [On call on the phone](mobile.md) | `ui` | planned |
| [Data history](data-history.md) | `ui`, `api` | planned |
| [Notifications](notifications.md) | `api`, `cli` | planned |

## Feature coverage by area

Every capability, grouped by area. `[x]` shipped, `[~]` partial (qualifier inline), `[ ]` planned. Keep each entry to one scannable line with its doc in parentheses: detail belongs in the capability doc, not here.

**The product**
- [x] What nats-lens is, the problem it solves, and its nouns (overview.md)

**Collection**
- [x] One snapshot of every server per interval, partial collections kept, account details, the expiry of account JWTs, user JWTs and listener certificates, subscription lists every few minutes, the replies of micro services in the listed accounts, and each client application's traffic per hour (monitoring-snapshots.md)
- [x] NATS advisories and system events, short-lived connections folded into `client.churn` and flagged by `client-005` when frequent, and account connections that also ask micro services and, when one cannot connect, are dialled again without stopping the system (event-capture.md)
- [x] Kubernetes pod, node and client-pod events on the same clock, with node upgrade and drain windows (kubernetes-context.md)
- [x] Retention, the hourly Parquet archive, and the rebuild from it (history-and-archive.md)
- [~] Several NATS systems in one nats-lens, with a system switcher, the Fleet page and its badge, and the shell's status of every system in one call — a cluster switcher inside a supercluster is planned (multiple-systems.md)

**Checks**
- [~] Findings, severities, restarts, thresholds, remediation file, mutes, and one entity's finding accepted until a review date — accepting related entities or a check everywhere is planned (checks.md)
- [x] Server checks `server-001` to `server-015` (server-checks.md)
- [x] Stream, KV bucket and object store checks `stream-001` to `stream-014` (stream-checks.md)
- [x] Consumer checks `consumer-001` to `consumer-013` (consumer-checks.md)
- [x] Client checks `client-001` to `client-005` (client-checks.md)
- [x] System checks `system-001` to `system-003` (system-checks.md)
- [x] The inventory of assets that must exist (inventory.md)
- [ ] Findings sent to a channel such as a webhook (notifications.md)

**Web UI**
- [x] Sidebar, system switcher, header with account scope and search, the global time range and the theme menu, all fed by one status call per system (navigation.md)
- [x] Overview: status line, nine metric boxes with descriptions, cause cards and muted findings, trends, timeline lanes with the status timeline, busiest consumers, streams with placement, several series in one API call (overview-page.md)
- [x] Servers for 20 or more servers: data points, charts across servers, a map of every server, a grouped list; server pages with metric boxes, placement, limits and recent events (servers.md)
- [x] Streams list and stream detail; replica placement in the API only (streams.md)
- [x] Consumers list and consumer detail, a stream filter that is not polled; delivery rate and busiest-first order in the API only (consumers.md)
- [x] Client connections of the newest collection, the server filter's choices sent with them (connections.md)
- [x] Every event, filtered by source, kind, entity and client (events.md)
- [x] Events worth reading first: routine events hidden, repeats folded, counts per source and server, a histogram, new events behind a pill (events.md)
- [ ] A configurable routine rule; subject and where columns (events.md)
- [x] Findings, finding detail with server copies, start times and events around the start, mutes, accepted findings read as accepted (triage-and-mutes.md)
- [ ] Why a finding has its severity, a CLI check ready to copy, accepting from the finding page or for related entities (triage-and-mutes.md)
- [~] Every stream and durable consumer on every server in one matrix, with filters, a moment to compare with and a side panel — how long most states have lasted and a playbook link are not built (placement.md)
- [~] What changed between two moments A and B, in order, with counts, gone assets, evidence links, a shared link and an incident log — "Replay this window" links and a replica catch-up step are not built (replay.md)
- [x] How the servers connect: routes, gateways and leaf node sites, with round trips, drops counted from the collections, slow routes (slow in two collections in a row) and sites not connected (topology.md)
- [x] Client connections grouped by the application in their name: instances, pods, servers, library, round trip, reconnects and 24 hours of traffic per application, with the spread over the servers, the libraries in use and the last day (applications.md)
- [x] What each stream keeps, what bounds it, and when disks fill, with an unbounded stream accepted as by design (capacity.md)
- [x] Where messages can go: each stream with its subjects and readers, client subscriptions by subject, reply inboxes and the JetStream API, filters with counts, and a tester that says what a message on a subject would reach in each account (subjects.md)
- [x] Accounts with their limits, imports, exports and JWT expiry, the failed logins of 24 hours, and the certificates and JWTs that expire within 30 days (accounts-and-auth.md)
- [x] KV buckets and object stores as their own pages, with placement and missing buckets (kv-and-object-stores.md)
- [x] Services built on the NATS micro framework: a card per service with its versions, instances, rate, errors and a note, its endpoints with their slow spells and last errors, its instances with the missing ones, asked in the listed accounts and seen in subscriptions in all (micro-services.md)
- [~] Search and jump to anything from one box — findings, events and prefixes are planned (command-bar.md)
- [ ] A finding on the phone, for the person on call (mobile.md)
- [ ] What history exists and where it has gaps (data-history.md)

**Operations**
- [x] Findings and collector health at `/metrics` for Prometheus alerting (metrics-and-alerting.md)
- [x] `run`, `once` for every system or one, configuration, health and readiness, the Kubernetes manifests and their memory sizes (running-nats-lens.md)
- [x] Read-only SQL on a stopped database with `natslens sql`, one system at a time (sql-access.md)
- [x] Three fixed systems without NATS with `natslens demo`, whose findings are the real checks, with a node pool upgrade to replay, links of a supercluster to draw from `GetTopology`, accounts and failed logins from `GetAccountsAndAuth`, the stores and streams of the Capacity page from `GetCapacity`, the client applications from `GetApplications`, and which show every state real data shows, tested against the store with one table of states (demo-mode.md)
