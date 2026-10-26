# Topology build plan

Working plan for the Topology page (`docs/features/topology.md`, board `Topology.dc.html` of the design canvas). Everything here was proposed by the planning session of 2026-10-20; the owner confirmed it on 2026-10-26, except two points the owner changed ([design.md](../design.md#owner-review-of-proposed-decisions-2026-10-26)): a route is slow only when the last two collections both measured it slow, still to be built, and the base deployment's memory goes from 256 MiB to 512 MiB, which is built. When a task lands, its lasting content moves into `docs/features/topology.md` and `docs/design.md`; this file then only tracks status.

The board's sample names (`acme-prod`, `eu-west`, `store-0417`, `RETAIL`, `IOT`) come from a sketch. Use the example shop of `inventory.example.yaml` and the demo's systems instead.

## Status

| Task | Status |
|---|---|
| T1 API, store and demo | done (three review rounds) |
| T2 UI | done |
| T3 Feature docs and audit | done: `docs/features/topology.md` shipped, README table and coverage map, the audit; leftovers moved to item 5 of `docs/plans/next.md` |

## Rules for every step

- No new table or column: `routes`, `gateways`, `leafnodes`, `epoch_endpoints`, `servers` and `server_stats` hold every value below. One migration, `0026_leafnodes_is_spoke_comment.sql`, only corrects the comment on `leafnodes.is_spoke` (fix round 1).
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
| Slow route | the routes of one cluster | Changed in T1's fix round 4: servers are near under the larger of 2 ms and 3 times the median of each server's fastest round trip, and near servers form groups; when the largest group holds more than half of the measured servers (those with a reported round trip), a server outside it with a pair to it is slow with all its routes that report a round trip, and a pair inside it is a slow link at 2 ms and 3 times the median of at least 2 group pairs that touch neither of its servers; otherwise nothing in the cluster is slow. Earlier rules (the cluster's median; the pairs not touching a route; a core grown from the fastest pair, also with a majority) missed one slow server in a cluster of 3 or 4, flagged zone layouts, or followed whichever group held the fastest pair, so uneven splits flipped between refreshes. |
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
- "Leaf nodes, worst first" table: Site, Hub, RTT, Drops 24 h, Subs, Account; not-connected rows in the warning colour with RTT "–" (an en dash; see "The dash" under T2's departures) and "last seen 09:12 on nats-eu-1".
- "Routes inside <cluster>" table with a cluster select: From → to, RTT, Pending, Pool, Pinned accounts; slow rows marked; one-sided rows say which end reports.
- Hidden sections: no gateways, no map lines; no leaf nodes, no leaf boxes or table; a single cluster shows the routes only, with the board's sentence.
- One line under the map: "Drops are counted from collections every <interval>; a drop and reconnect within one interval is seen only as a new connection ID." Unobserved servers listed as "ROUTEZ did not answer on nats-eu-4".
- Data: `requests.getTopology`, `api/topology.ts`, `useTopology()` keyed by system and range, refetched with the page's live interval like Placement.

## Where T1 departed from this plan

Each departure was made by the session that built T1 and confirmed by the owner on 2026-10-26.

- **Durations, not nanoseconds.** Round trips are `google.protobuf.Duration` (`rtt_max`, `rtt_median`, `rtt`), as `RouteView.rtt` and the rest of the API carry them, not `int64 rtt_ns`.
- **Gateways from outbound rows.** Every gateway connection is one server's outbound and another's inbound, so connections, round trips and drops are read from the outbound rows only (pending bytes from both); counting both directions would count one disconnect twice and give twice the expected connections. `expected` stays one outbound per server of either cluster that answered GATEWAYZ.
- **Consecutive collections.** Two collections count as consecutive when they are at most 3 collect intervals apart, the gap rule of Replay. A pair counts in a window (1 hour, 24 hours) when its earlier collection lies in it; `drops_window_start` is the oldest collection read.
- **Route pairs.** The two ends are folded into one row rather than shown each: the highest round trip of either end, pending summed, the pool the larger count either end reports (not the sum), pinned accounts the union, and `reported_by` naming the ends that report the pair. A pair is one-sided only when the silent end answered ROUTEZ; a pair whose other end did not answer is listed from the end that did. The median for the slow rule is the usual one, the mean of the middle two for an even count.
- **Spoke rows left out.** nats-server sets `is_spoke` on the server that opened the connection to a hub (`leafnode.go`: `isSpoke` when the remote is not a hub or declares itself one), so a hub lists its sites with `is_spoke` false. Rows with `is_spoke` true are the observed server's own uplinks and are not sites. The column's comment in `0002_server_stats.sql` said the opposite; migration `0026_leafnodes_is_spoke_comment.sql` corrects it.
- **Sites not connected.** A site seen in the 24 hours and not in the epoch is listed as not connected with its last hub and time. When a server of the cluster the site was last seen on failed LEAFZ in the epoch, the site may be connected there, so it is marked `connection_unknown`; a failure in another cluster leaves a known disconnect known (a field added in fix round 1, scoped to the cluster in fix round 2; the first version left it out when its last hub's LEAFZ failed, and showed it on its old hub when it had moved to the failed one). A server that answered nothing is drawn missing and does not make sites unknown. A site without a name is keyed by its IP address; a site on several hubs at once is one row naming them all (`hub_servers`); clusters count sites by name.
- **Cluster leaf counts.** `leafnodes_connected` and `leafnodes_not_connected` are set, 0 included, when a server of the cluster answered LEAFZ, else unset.
- **Unobserved.** A server that answered nothing is drawn missing in its cluster and not named in `unobserved` again.
- **Added fields.** `leafnodes_total`, `collect_interval` (for the page's line on drops), `account_name` on a site (the handler fills it as for other messages), `reported_by` on a route.
- **Demo.** Every site is in `APP`, as `ListAccounts` lists only `APP`. A gateway or site whose server the upgrade stopped moves to the next server of its cluster that never stops; the gateway move is a drop of its reporter (`eu`-`us` 4 and `ap`-`us` 7 in 24 hours, with `nats-ap-1`'s 3 in the last hour), the site's is none. The demo writes no inbound gateway rows. To keep the Server page in agreement, its route list now holds the pooled and pinned routes per remote (3 per remote in `example-global`, 4 in `example-prod` with its `APP` route) with the Topology page's round trips, its leaf node count the sites on the server, and `example-global`'s account connection events are worded as the capture words them (`internal/events/decode_system.go`), the total counting the server's leaf nodes as NATS does.

- **Fix round 1** (from the review of T1). Drops are counted in SQL by the store (`storeprov/topology_drops.go`) and by `api.CountTopologyDrops` in the demo, the same rule, and `TopologyFacts` carries the counts (`TopologyDrops`) instead of the collections: reading every leaf node row of a day allocated 2.4 GB in Go for 1,000 sites at 30 seconds, now 2 MB and 0.4 seconds. Every pair of clusters with an outbound gateway row in the 24 hours is listed, so a pair down in the epoch shows 0 connections and sorts first. The slow route rule compares with the pairs that do not touch the route's servers (see `markSlowRoutes` and design.md), as the cluster's median let one slow server set the reference in a cluster of 3 or 4. A round trip of 0 counts as not reported. A server without a cluster is placed by the routes that name it, else in the cluster with an empty name. The demo's Server page reads the range's epoch; `ap`'s routes take about 4.6 ms, so the conformance row can tell a slow route from a high round trip.

- **Fix round 2** (from the second review). The slow rule above, with a test matrix. The leaf drop query aggregates connections stored without a break and reads only the others row by row, and sightings aggregate each site's newest collection, so no query sorts the day's rows: on a 14-day file store with 1,000 sites and DuckDB at 60% of 256 MiB, 0.3 s (from 1.25 s and a peak RSS of 641 MB); the peak RSS, measured again in fix round 3, varies from 174 to 365 MB on the clean store and reaches 343 and 427 MB when each server's LEAFZ fails once in the day, so up to about 450 MB at 1,000 sites, and failed LEAFZ answers cost more; sizing for hundreds of sites is in the README's "Memory size". Gaps are compared in microseconds in SQL, as in Go. A randomized test compares the SQL count with `CountTopologyDrops`. `connection_unknown` is set only by a LEAFZ failure in the cluster the site was last seen on. A server placed by its routes brings its rows into that cluster. A removed cluster shows as a down gateway pair until its last outbound row leaves the 24 hours.

- **Fix round 3** (from the third review). Slow routes are judged only against a core of more than half the cluster's measured servers, with seeded split and jitter cases in the matrix. A duplicate leaf row sends its connection to the row-by-row count, so it cannot hide a gap. The memory figures above were measured again.

- **Fix round 4** (from the final review). The slow rule by groups of near servers (the Slow route row above), with uneven and even splits and unreported pairs run over 200 seeds of jitter, each giving the same result on every seed; a cluster of 30 servers is judged in under a millisecond. The plan's departure in fix round 3 (the core) is replaced by it. Its review found that a server reporting no round trip at all leaves the measured servers, so an even split with one such server is judged for that collection; documented, not changed.

### Leftovers of T1

Moved to item 5 of `docs/plans/next.md` (Topology), with those of T2.

## Where T2 departed from this plan

Each departure was made by the session that built T2 and confirmed by the owner on 2026-10-26.

- **The heading.** The header's "Topology" is the page's one heading, as on every sidebar page; the page starts with the muted line ("3 clusters joined by gateways, 30 leaf node sites at the edge, in the collection of 05:22:17 UTC."). A single cluster reads "One cluster, example-prod, of 3 servers".
- **The dash.** Unset values use the app's en dash (–, read out as "not observed"), as every other page does, not the em dash (—) of the board and of this plan's first version; in a row not connected it reads "not connected", in a row of unknown state "state unknown". Decided in the session that built T2 and kept by the coordinator of the build; confirmed by the owner on 2026-10-26.
- **Gateway label.** "gateway 84 ms" is the median round trip; the highest is in the title and screen reader text with the connections and the drops in 1 and 24 hours. "· 4 of 6 up" is added and the line drawn in the warning colour when connections are down, as for a drop in the last hour.
- **Far pairs.** A gateway between clusters not next to each other is a bracket under the boxes, rather than the board's single diagonal; four clusters need up to three of them.
- **Leaf node boxes.** The number is the sites seen (connected now and not connected), with "· N not connected" or "· all connected". In the grid layout the box sits inside the cluster box.
- **Grid layout.** The board's row is used for up to 4 clusters (4:3 box to gap, so a 3-cluster map at a 1440 px window holds "gateway 84 ms · 1 drop/h" on one line) while each gap of the map, inside the card's padding, is at least 116 px, enough for "gateway 166 ms" on one line, and each box at least 150 px; below that, or with more clusters, the grid and the gateway table. At a 1010 px window three clusters give the grid.
- **Cluster note.** "JetStream meta leader · 5 streams led", plus "1 missing" and "gateway to us dropping" where they apply (the board's "mirrors from" is not backed). A cluster box is in the warning colour when a server is missing or its gateway dropped in the last hour.
- **Routes table.** The cluster is `?cluster=` in the URL; without it, among the clusters with a slow route (all clusters when none has one), the meta leader's, else the first by name, the unknown cluster after every named one; never the first route's alone, which changes with refetches. From and to are both full server names, each a link. A pair with no pooled route shows "none" in Pool, as does an empty Pinned accounts. A pair one end reports while the other answered says "only nats-eu-1 reports it; nats-eu-2 does not"; one whose other end did not answer says "as nats-eu-1 reports it; nats-eu-2 did not answer ROUTEZ".
- **Drops in the leaf table.** Shown in the warning colour only in a row not connected, as on the board.
- **Unknown cluster.** Servers that report no cluster name are shown as "unknown cluster" in the box, the select, the routes title, the leaf box, the gateway table and the notes.
- **Gateway pair down.** A pair with connections expected and none up reads "gateway down · 0 of N up", marks both clusters' boxes with "gateway to X down", and the page's line says "1 gateway pair down", or "every gateway pair down" instead of "joined by gateways".
- **Label placement and wrapping.** Each part of a gateway label and of a leaf box stays on one line, and every part after the first starts with "· ", so a wrapped line starts with the separator. A far gateway's label starts just right of its bracket's first leg and wraps between its parts to end before the next leaf node connector or deeper leg; each level of brackets is as tall as its tallest label. The Hub column wraps between hubs, server names in a leaf row's note stay whole, and a note without a last-seen time reads "not connected; last hub nats-eu-1" or "state unknown; last hub nats-eu-1". The page's stylesheet styles only its own cards: every rule names a `tm-` or `tp-` class, and a test checks this. Lines and the light warning border use `--tp-line` and `--tp-warn-line`, at 3:1 or more against the card.
- **Short durations.** `fmtShortDuration`, which Connections and the Server page share, shows a value under 0.005 ms as "<0.01 ms" and one that rounds to 1000 ms as "1 s".
- **Leaf node hubs and unknown state.** The Hub column lists every hub of a connected site (`hub_servers`), each a link. A site with `connection_unknown` gets a row of its own state on the quiet surface colour, not the warning colour: "state unknown: LEAFZ did not answer on nats-us-2; last seen 09:12 on nats-us-1", naming only the servers of the site's own last cluster whose LEAFZ did not answer, its drops not coloured, round trip and subscriptions an en dash read as "state unknown".
- **Map width.** The map is measured through a callback ref (`useElementWidth`), so a map that appears only after a refetch, or again after it was gone, is measured too.
- **Short history.** When `drops_window_start` is more than a minute later than 24 hours before the collection, the drops line adds when the history starts.
- **Round trips.** The adapter reads durations with their fraction of a millisecond, as `durationMs` of the protobuf library rounds to whole milliseconds and showed routes as "0 ms".
- **The board's sentence.** "For a single cluster this page shows just the routes; with no gateways or leaf nodes those sections stay hidden." is shown under the routes table only when the page shows nothing else.

### Leftovers of T2

Moved to item 5 of `docs/plans/next.md` (Topology).

## Tasks

- **T1 API, store and demo.** `topology.proto`, the RPC, `make proto` with generated code committed; `internal/api/topology.go` with tests (pairing, medians, slow rule, drop counting across gaps and failed endpoints, a restart is no drop, sorting, the leaf cap); handler tests; `storeprov/topology.go` with tests over fixture rows; the demo for the three systems; the conformance rows; `docs/design.md` (the RPC, the drop rule, limits) and `demo-mode.md`.
- **T2 UI.** The nav entry, the route, `api/topology.ts`, `requests.ts`, `queries.ts`, the map and the two tables with tests (hidden sections, slow route, not-connected leaf, unset values, cluster pick); phone width; `navigation.md` and the UI section of `design.md`.
- **T3 Feature docs and audit.** `topology.md` to `Status: partial` (or `built`) with its scenarios and Not supported list, `docs/features/README.md` table and coverage map, the audit, leftovers into `docs/plans/next.md`.
