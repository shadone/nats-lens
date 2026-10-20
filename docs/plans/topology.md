# Topology build plan

Working plan for the Topology page (`docs/features/topology.md`, board `Topology.dc.html` of the design canvas). Everything here was proposed by the planning session of 2026-10-20 and is not yet confirmed by the owner. When a task lands, its lasting content moves into `docs/features/topology.md` and `docs/design.md`; this file then only tracks status.

The board's sample names (`acme-prod`, `eu-west`, `store-0417`, `RETAIL`, `IOT`) come from a sketch. Use the example shop of `inventory.example.yaml` and the demo's systems instead.

## Status

| Task | Status |
|---|---|
| T1 API, store and demo | built on `feat/topology-page`, awaiting review |
| T2 UI | not started |
| T3 Feature docs and audit | not started |

## Rules for every step

- No migration: `routes`, `gateways`, `leafnodes`, `epoch_endpoints`, `servers` and `server_stats` hold every value below.
- The page shows the newest collection at or before the end of the page range (the epoch rule of `GetPlacement`), and counts drops over the 24 h before that collection and over its last hour.
- A value a server did not report is unset, never 0. A server whose ROUTEZ, GATEWAYZ or LEAFZ did not answer is named in `unobserved`, and its links are left out rather than drawn as down.
- Drops are seen only from collections: NATS sends no event when a route, gateway or leaf node disconnects. A disconnect and reconnect within one collect interval shows only as a changed connection ID; one that changes nothing between two collections is not seen. The page says so in one line.
- Drops are counted only between two consecutive collections in which the reporting server answered that endpoint; a collection gap or a failed endpoint counts no drop.

## What backs each part

| Part | Source | Rule |
|---|---|---|
| Clusters | `servers` (`cluster`, `tags`) of the servers expected in the epoch (`epochs.expected_names`) | One box per cluster, servers sorted by name. A server in `missing_names` is drawn missing. Place: the value of a `region:` tag shared by every server of the cluster, else none. |
| Cluster note | `server_stats.meta_leader` (majority view), `js_streams_leader` | "JetStream meta leader" when the meta leader is in the cluster; "N streams led". |
| Gateways | `gateways` rows of the epoch, `servers.cluster` of the reporter | One link per unordered pair of clusters: connections up now (outbound and inbound rows), connections expected (servers of both clusters that answered GATEWAYZ, one outbound each way), the highest and the median RTT of the outbound rows, pending bytes summed. |
| Gateway drops | `gateways` across epochs | Per (reporting `server_id`, `gateway`, `direction`): a `cid` that changed, or a row that went missing, between two consecutive epochs in which that server's GATEWAYZ answered ok. A server restart (new `server_id`) is no drop of its gateways; its restart is on the Servers page. Counted per pair over the last hour and the last 24 h. |
| Routes | `routes` rows of the epoch, both ends in the same cluster | One row per unordered pair of servers: RTT the highest of its routes, pending bytes summed, pool size the count of routes with an empty `account`, pinned accounts the distinct non-empty `account`. Each end's report is shown when both report; a pair that one end reports and the other does not is drawn as one-sided. |
| Slow route | the routes of one cluster | A route stands out when its RTT is at least 3 times the cluster's median route RTT and at least 1 ms. |
| Leaf nodes | `leafnodes` rows of the epoch | One row per (remote `name`, `account`): hub server (the reporter), RTT, subscriptions, compression. A site seen in the 24 h but not in the epoch is listed as not connected, with its last hub and when it was last seen, and RTT and subscriptions unset. |
| Leaf drops | `leafnodes` across epochs | Per (`name`, `account`): the connection (`server_id`, `lid`) changed, or the site went missing, between two consecutive epochs in which the old hub's LEAFZ answered ok. A move to another hub counts. |
| Leaf nodes by hub cluster | the leaf rows | Per cluster: sites connected now, sites not connected now. |
| Unobserved | `epoch_endpoints` of the epoch | Servers whose ROUTEZ, GATEWAYZ or LEAFZ is missing or in error, per endpoint. |

Order of the leaf table, worst first: not connected now, then drops in 24 h descending, then RTT descending, then name. Routes: slow first, then RTT descending. Gateways: drops in the last hour descending, then RTT.

Not backed, so not shown: why a link dropped; a leaf node's own clients (unless that leaf's system is watched too); stream mirrors between clusters (the board's "mirrors from eu-west"; a leftover); route drops (routes have `start_time`, a leftover).

## Proto

`proto/natslens/v1/topology.proto`, RPC `GetTopology(GetTopologyRequest) returns (GetTopologyResponse)` in the service.

- `GetTopologyRequest`: `system`, `range` (the common `TimeRange`, as `GetPlacement`).
- `GetTopologyResponse`: `epoch` (int64), `epoch_at`; `repeated TopologyCluster clusters`; `repeated TopologyGateway gateways`; `repeated TopologyRoute routes`; `repeated TopologyLeafnode leafnodes`; `repeated TopologyUnobserved unobserved`; `drops_window_start` (the start of the 24 h counted, later than 24 h back when history is shorter).
- `TopologyCluster`: `name`, `place`, `repeated TopologyServer servers` (`name`, `missing`, `meta_leader`), `meta_leader` bool, `optional int32 streams_led`, `optional int32 leafnodes_connected`, `optional int32 leafnodes_not_connected`.
- `TopologyGateway`: `cluster_a`, `cluster_b` (sorted), `int32 connections`, `int32 expected`, `optional int64 rtt_ns_max`, `optional int64 rtt_ns_median`, `optional int64 pending_bytes`, `optional int32 drops_1h`, `optional int32 drops_24h`, `repeated string dropping_servers` (servers whose connection dropped in the last hour).
- `TopologyRoute`: `cluster`, `server_a`, `server_b` (sorted), `optional int64 rtt_ns`, `optional int64 pending_bytes`, `optional int32 pool`, `repeated string pinned_accounts`, `bool one_sided`, `bool slow`.
- `TopologyLeafnode`: `name`, `account`, `hub_server`, `hub_cluster`, `bool connected`, `optional int64 rtt_ns`, `optional int64 subscriptions`, `optional int32 drops_24h`, `last_seen` (for a site not connected), `compression`.
- `TopologyUnobserved`: `endpoint` (ROUTEZ, GATEWAYZ, LEAFZ), `repeated string servers`.

Unset means not reported; counts of drops are unset when no two consecutive answers cover the window.

## Server side

- `internal/api/topology.go`: Go structs `TopologyFacts` (rows per epoch as plain values) and `FinishTopology(facts) *v1.GetTopologyResponse`, which pairs, sums, marks slow routes, counts drops and sorts, so the store and the demo share the rules. Same split as `GetPlacement`/`FinishPlacement` and `GetReplay`/`FinishReplay`. `Provider` gains `Topology(ctx, req) (*TopologyFacts, error)`.
- Handler `GetTopology`: the common range validation, the 30 s `QueryTimeout` through `call()`. No epoch in the range: the empty response with `epoch` 0, as `GetPlacement` does.
- `internal/api/storeprov/topology.go`: the epoch's rows, and for drops the `gateways` and `leafnodes` rows of the 24 h before it with the `epoch_endpoints` states. At most 500 leaf node rows in the response (sorted worst first), with `leafnodes_total`.
- Demo, `internal/api/fake/topology.go`: `TopologyFacts` from the scenario. `example-global`: the three clusters with full gateway mesh; ap–us gateway drops 3 times in the last hour from nats-ap-1 (from 13:20 on in the fixed frame), RTTs eu–us 84 ms, us–ap 212 ms, eu–ap 166 ms; routes in each cluster at 0.3–0.6 ms, with nats-eu-3's routes at 4 ms (slow, matching its CPU); about 30 leaf node sites of the shop at the edge (warehouses and pickup points, accounts `APP` and a new `EDGE` only if `ListAccounts` stays consistent, else `APP`), one site (`pickup-0417`) dropped 17 times in the 24 h and not connected now, one with 2 drops. `example-prod`: one cluster, routes only, no gateways or leaf nodes. `example-staging`: as its scenario allows (stale collections give the empty or old epoch). Past ranges follow the range's epoch where the scenario has the data.
- Conformance rows in `internal/api/apitest`: a gateway pair with both RTTs; a slow route; a not-connected leaf node with drops and a last hub; a single-cluster system without gateways or leaf nodes; drops unset when the window has no two consecutive answers; the store fixture and the demo pass alike.

## UI

- Route `/topology`, `web/src/pages/Topology.tsx`; the `NAV` entry loses `planned`. h1 "Topology", line "N clusters joined by gateways, M leaf node sites at the edge." (only the parts that exist).
- Map (`components/TopologyMap.tsx`): one box per cluster with its servers as chips (missing or dropping in the warning colour), place and note; gateway lines labelled "gateway 84 ms", with "· 3 drops/h" and the warning colour and dash when it dropped in the last hour; a "Leaf nodes on <cluster>" box per hub cluster ("28 sites · 1 not connected", or "all connected"). Up to 4 clusters in the board's layout; more clusters, or a phone width, as a grid of boxes and a gateway table. Clicking a cluster box picks the routes table's cluster.
- "Leaf nodes, worst first" table: Site, Hub, RTT, Drops 24 h, Subs, Account; not-connected rows in the warning colour with RTT "—" and "last seen 09:12 on nats-eu-1".
- "Routes inside <cluster>" table with a cluster select: From → to, RTT, Pending, Pool, Pinned accounts; slow rows marked; one-sided rows say which end reports.
- Hidden sections: no gateways, no map lines; no leaf nodes, no leaf boxes or table; a single cluster shows the routes only, with the board's sentence.
- One line under the map: "Drops are counted from collections every <interval>; a drop and reconnect within one interval is seen only as a new connection ID." Unobserved servers listed as "ROUTEZ did not answer on nats-eu-4".
- Data: `requests.getTopology`, `api/topology.ts`, `useTopology()` keyed by system and range, refetched with the page's live interval like Placement.

## Where T1 departed from this plan

Each departure was made by the session that built T1: proposed, not yet confirmed.

- **Durations, not nanoseconds.** Round trips are `google.protobuf.Duration` (`rtt_max`, `rtt_median`, `rtt`), as `RouteView.rtt` and the rest of the API carry them, not `int64 rtt_ns`.
- **Gateways from outbound rows.** Every gateway connection is one server's outbound and another's inbound, so connections, round trips and drops are read from the outbound rows only (pending bytes from both); counting both directions would count one disconnect twice and give twice the expected connections. `expected` stays one outbound per server of either cluster that answered GATEWAYZ.
- **Consecutive collections.** Two collections count as consecutive when they are at most 3 collect intervals apart, the gap rule of Replay. A pair counts in a window (1 hour, 24 hours) when its earlier collection lies in it; `drops_window_start` is the oldest collection read.
- **Route pairs.** The two ends are folded into one row rather than shown each: the highest round trip of either end, pending summed, the pool the larger count either end reports (not the sum), pinned accounts the union, and `reported_by` naming the ends that report the pair. A pair is one-sided only when the silent end answered ROUTEZ; a pair whose other end did not answer is listed from the end that did. The median for the slow rule is the usual one, the mean of the middle two for an even count.
- **Spoke rows left out.** nats-server sets `is_spoke` on the server that opened the connection to a hub (`leafnode.go`: `isSpoke` when the remote is not a hub or declares itself one), so a hub lists its sites with `is_spoke` false. Rows with `is_spoke` true are the observed server's own uplinks and are not sites. The column's comment in `0001_epochs_servers.sql` says the opposite (a leftover).
- **Sites not connected.** A site seen in the 24 hours and not in the epoch is listed as not connected only when its last hub's server answered LEAFZ in the epoch; otherwise its state is not known and it is left out, by the rule for unobserved links. A site without a name is keyed by its IP address.
- **Cluster leaf counts.** `leafnodes_connected` and `leafnodes_not_connected` are set, 0 included, when a server of the cluster answered LEAFZ, else unset.
- **Unobserved.** A server that answered nothing is drawn missing in its cluster and not named in `unobserved` again.
- **Added fields.** `leafnodes_total`, `collect_interval` (for the page's line on drops), `account_name` on a site (the handler fills it as for other messages), `reported_by` on a route.
- **Demo.** Every site is in `APP`, as `ListAccounts` lists only `APP`. A gateway or site whose server the upgrade stopped moves to the next server of its cluster that never stops; the gateway move is a drop of its reporter (`eu`-`us` 4 and `ap`-`us` 7 in 24 hours, with `nats-ap-1`'s 3 in the last hour), the site's is none. The demo writes no inbound gateway rows. To keep the Server page in agreement, its route list now holds the pooled and pinned routes per remote (3 per remote in `example-global`, 4 in `example-prod` with its `APP` route) with the Topology page's round trips, its leaf node count the sites on the server, and `example-global`'s account connection events are worded as the capture words them (`internal/events/decode_system.go`), the total counting the server's leaf nodes as NATS does.

### Leftovers of T1

- The comment on `leafnodes.is_spoke` in `0001_epochs_servers.sql` has the meaning reversed; correcting it takes a new migration with a `COMMENT ON COLUMN`.
- `example-prod`'s account connection events keep the demo's own wording ("account APP: N connections, 0 leaf nodes"), which the capture does not write; the leaf node count agrees, as `example-prod` has none.
- No demo system has an endpoint that failed, so `unobserved` is empty in the demo; the store tests cover it.
- Measured on a synthetic store (24 servers, 1,000 leaf node sites, a day at 30 seconds): 1.8 seconds to read and 1.2 seconds in `FinishTopology`, within the 30 seconds; the leaf drop count builds a map per collection and could reuse runs of unchanged collections if larger systems need it.

## Tasks

- **T1 API, store and demo.** `topology.proto`, the RPC, `make proto` with generated code committed; `internal/api/topology.go` with tests (pairing, medians, slow rule, drop counting across gaps and failed endpoints, a restart is no drop, sorting, the leaf cap); handler tests; `storeprov/topology.go` with tests over fixture rows; the demo for the three systems; the conformance rows; `docs/design.md` (the RPC, the drop rule, limits) and `demo-mode.md`.
- **T2 UI.** The nav entry, the route, `api/topology.ts`, `requests.ts`, `queries.ts`, the map and the two tables with tests (hidden sections, slow route, not-connected leaf, unset values, cluster pick); phone width; `navigation.md` and the UI section of `design.md`.
- **T3 Feature docs and audit.** `topology.md` to `Status: partial` (or `built`) with its scenarios and Not supported list, `docs/features/README.md` table and coverage map, the audit, leftovers into `docs/plans/next.md`.
