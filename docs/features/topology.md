# Topology

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [servers.md](servers.md), [server-checks.md](server-checks.md), [capacity.md](capacity.md), [streams.md](streams.md), [multiple-systems.md](multiple-systems.md), [monitoring-snapshots.md](monitoring-snapshots.md), [demo-mode.md](demo-mode.md), [navigation.md](navigation.md), [running-nats-lens.md](running-nats-lens.md), [design.md](../design.md), [the build plan](../plans/topology.md)

## What it does

The Topology page, under Cluster in the sidebar, draws how the servers of a system connect in the newest collection of the page's range: the routes inside each cluster, the gateways between the clusters of a supercluster, and the leaf node sites at the edge. It shows the round trip time of each link and how often it dropped, counted from the collections of the 24 hours before, so a slow or flapping link is found without logging in to each server. It also shows each server's memory and the streams that mirror or source a stream in another cluster. `GetTopology` serves the same to any API client.

## Behavior and rules

- **First visit.** The page reads data for the current scope while its view loads. The shell stays available, with loading and reload recovery described in [navigation](navigation.md).

- **Clusters and gateways.** Each cluster is a box with its servers, its region, a routes line and a note (meta leader, streams led, missing servers, a dropping gateway, a dropping or down route, and the clusters its streams copy from, such as "mirrors from eu · sources from us"); a missing server, or one whose gateway dropped in the last hour, is in the warning colour, and a server opens its page. The box is in the warning colour too when a route of the cluster dropped in the last hour or is down. Gateways between clusters are lines labelled with their round trip time, such as "gateway 84 ms", and "· 3 drops/h" in the warning colour and dashed when they dropped in the last hour. A pair whose gateways are all down is still drawn, as "gateway down · 0 of 4 up"; a cluster that was removed shows so too, until its last gateway connection is more than 24 hours old. Up to four clusters sit in a row; more, or a phone, give a grid of boxes and a table of the gateways. A line under the map says drops are counted from the collections every collect interval.
- **Leaf nodes by hub cluster.** Under each cluster with leaf nodes, a box counts the sites seen on it, connected now or seen in the last day and not connected now, and says how many are not connected ("18 sites · 1 not connected", or "all connected"). Sites are counted by name, so a site bound to two accounts counts once.
- **Leaf nodes, worst first.** A table of leaf node sites: the hub server, round trip time, drops in the last 24 hours, subscriptions and account. A site not connected comes first, in the warning colour, with an en dash (–) for its round trip and subscriptions and "not connected, last seen 09:12 on nats-eu-1". A site connected to several hubs lists each of them. When a server of the cluster a site was last seen on did not answer LEAFZ, the site may be connected there: it is listed next, not in the warning colour, as "state unknown: LEAFZ did not answer on nats-us-2; last seen 09:12 on nats-us-1", naming only the servers of its own cluster, with en dashes for its round trip and subscriptions. A failure in another cluster leaves a known disconnect as it is. When the server cut the list at 500 sites, the page says how many of how many it lists.
- **Routes in the cluster box.** Under the servers, a line reads "routes 0.46 ms", the median round trip of the cluster's pairs, and adds "· 2 drops/h" and "· 1 down" in the warning colour: the route drops of the last hour summed over the pairs, and the pairs down.
- **Routes inside a cluster.** From and to, round trip time, pending bytes, drops in the last hour and the last 24 hours, route pool size and pinned accounts, for the cluster picked with a select or a click on its box (until one is picked, among the clusters with a slow route the meta leader's, else the first by name, a cluster of unknown name after every named one; the choice is in the address). A pair that is down comes first, then slow routes, marked "Slow". A route that dropped in the last hour is in the warning colour. A route only one end reports says which end. The servers of a cluster form a full mesh, so a pair is down when both servers, of one cluster and one JetStream domain, answered ROUTEZ in the shown collection and in the one before it, without restarting in between, neither lists a route to the other in either collection, and either the two had a route in the last 24 hours or both list routes and are joined through others (proposed, not yet confirmed). So a server that lost its routes, and a cluster split into halves each meshed inside, show every lost pair down for 24 hours after its last route; after that only pairs still joined through other routes show. Sites that share a cluster name but never route to each other, single-server sites included, are not paired. Such a pair reads "down: neither server lists a route to the other", with dashes for what it does not have.
- **Slow-route findings.** `server-025` uses the same pairing and classifier as this page. It warns on both ends of each persistently slow pair, naming the peer and current RTT; this does not identify the responsible server. See [server checks](server-checks.md). Attribution and severity are proposed, not yet confirmed.
- **Slow routes.** A route is slow when it stands out from the cluster's normal round trip, never under 2 ms. Two servers count as near when their round trip is under 2 ms, or under 3 times the typical round trip from a server to its nearest neighbour; near servers, directly or through others, form a group. The measured servers are those with at least one round trip reported. The cluster is judged only when its largest group holds more than half of the measured servers: a server outside that group, measured against it, is slow with all its routes that report a round trip, and a route inside the group is slow at 3 times the typical round trip of the group's other routes. A cluster split into groups of similar size, such as two zones of three servers 10 ms apart, has no normal group and nothing in it is slow. So one slow server, at 3 times the cluster's typical nearest round trip or more, stands out in a cluster of 3 or more, two in a cluster of 5 or more (two of 4 are not judged), a small group far from the rest is slow, and one slow link stands out in a cluster of 5 or more; zones under 2 ms apart, jitter and two servers alone are never slow. A route is slow only when the shown collection and the one before it both find it slow, so one spike cannot mark it. The one before is the newest earlier collection with routes, at most three collect intervals earlier, also for a past range whose start lies between the two (proposed, not yet confirmed). Without such a collection, as for the first collection after nats-lens starts or after a gap in collecting, nothing is slow; a route that collection did not measure, as when a server was restarting, is not slow either; and when the collect interval is not known, no collection counts as the one before and nothing is slow (each proposed, not yet confirmed). Each collection is judged by the full rule on its own routes, so a collection before in which only some servers reported routes is judged on those. A round trip close to a threshold can still make a route slow on one refresh and not on the next, but only after two collections on the slow side. In a cluster split into groups of similar size, a server that reports no round trip at all (during its restart, or when its ROUTEZ failed) leaves the measured servers, so for that collection the cluster is judged and the other group is slow.
- **Drops.** NATS sends no event when a route, gateway or leaf node disconnects, so nats-lens counts drops from the collections: between two collections at most three collect intervals apart, a gateway connection whose ID changed or that went away, and a leaf node connection that went away or moved to another hub, while the server that reported it answered both times. A gap in the collections, a failed answer and a server restart count no drop. A drop and reconnect within one collect interval shows only as a new connection ID. A count that no two such collections cover is shown as not observed, not as 0.
- **Route drops.** A route drops when a route connection (its route ID on the server that lists it) is gone in the next collection, while both servers of the pair answered ROUTEZ both times under the same server IDs; a restart of either server, a failed ROUTEZ of either and a gap count none. Nor does a pair of collections in which either server's process had started less than the checks' `restart_window` (10 minutes by default) before, as a cluster replaces routes while it settles after a start (proposed, not yet confirmed). Both ends list every route, and with route pooling (NATS 2.10 and later) a pair has several routes, plus one per pinned account. A pair counts one drop per pair of collections, however many of its routes went: one replaced connection of a pool of three is one drop, and the whole pool reconnecting at once is one drop too, not three or six (proposed, not yet confirmed).
- **Leaf-site findings.** The system-level `server-026` check shares the stored connection-drop rule. It uses stricter observation coverage, excludes hub handoffs, and reports sustained absence or repeated drops as described in [server checks](server-checks.md). Page drop counts still include hub moves.
- **Memory.** Each server chip shows the server process's resident memory under its name, such as "107 MiB", and, when its JetStream memory store holds anything, its share of the limit, such as "JS 4%"; screen readers hear both, with the store against its limit ("JetStream memory 80 MiB of 2 GiB"), which the title gives too. The warning comes from the checks, not from thresholds of the page's own: when `server-008` (memory far above the other servers) fires for the server, or `server-007` (JetStream store usage high) fires while its memory store is at least as full as its file store, the chip is in the warning colour and the finding's code sits under it as a link to the finding. A muted or accepted finding is linked but not in the warning colour, and screen readers hear "(muted)" or "(accepted)". A finding puts the chip in the warning colour even when the process memory itself was not reported. nats-server does not report its memory limit, so there is no comparison with one. A line under the map says JetStream memory stores are on [Capacity](capacity.md). A missing server shows no figure. A single cluster without gateways or leaf nodes has no map and so no memory figures.
- **Replication between clusters.** A table lists every stream that mirrors or sources a stream placed in another cluster: from (origin stream and cluster) to (stream and cluster), with the subject filter when one is set ("filter catalog.eu.>"), so two sources of one origin are told apart; mirror or source; lag in messages and when it last heard from its origin, as the copying stream's leader reports; and the account. Both streams link to their pages. Without a leader's report (no leader, or its server did not answer), lag and last heard read "– leader not reported"; when the leader's report lacks a copy's figures they read "– not in the leader's report". The warning comes from the stream checks: a row is in the warning colour, listed first, when a finding about the copy fires (`stream-017`, else `stream-015`, for a mirror; `stream-023`, else `stream-022`, for a source when the finding names that source or names none), with the finding's code as a link and its detail under the name; a muted or accepted finding is shown without the warning colour, marked "(muted)" or "(accepted)". An error the leader reports shows under the name as plain text, "error reported by the leader: …", unless the finding's detail already holds it; it does not by itself put the row in the warning colour. Stream findings are keyed by account and stream without the JetStream domain, so when two domains hold a stream of one name in one account, a finding about either shows on both rows. A stream is placed by the JetStream domain and cluster its leader reports, else any replica's; its origin is looked up in the same account and domain. Mirrors and sources inside one cluster, external ones (in another account or JetStream domain) and those whose origin no server reports are not listed. The table shows only when there is such a copy.
- **Only what exists.** For a single cluster without leaf nodes the page shows just the routes; without gateways there are no gateway lines, and without leaf nodes no leaf node boxes or table. A value a server did not report shows as an en dash (–), never as 0. Servers whose ROUTEZ, GATEWAYZ or LEAFZ did not answer are named under the map. Servers that report no cluster are placed in the cluster of a server whose route names them, else shown as "unknown cluster".
- **Built on stored data.** ROUTEZ, GATEWAYZ and LEAFZ are collected and stored every collection; memory comes from the stored VARZ and JSZ numbers, and mirrors and sources from the stored stream reports and configurations, so nothing new is collected. A past range shows its own newest collection. Reading a day of a system with hundreds of leaf node sites is estimated to need more memory than the base deployment's 512 MiB; the [Memory size](../deploy.md#memory-size) table in the deploy docs gives the proposed sizes (see also [running-nats-lens.md](running-nats-lens.md)).

## Scenarios

### Find a flapping leaf node

- **Surfaces:** `ui`
- **Given** a leaf node site that dropped its connection 17 times in a day and is not connected now
- **When** I open Topology
- **Then** the site is first in the leaf node table, in the warning colour, with its drops and when and on which hub it was last seen

### See a slow route

- **Surfaces:** `ui`
- **Given** a cluster of nine servers in which one server's routes take 4 ms and the others' about 0.4 ms
- **When** I open Topology
- **Then** the routes table opens on that cluster with the slow server's routes first, each marked "Slow"

### One spike does not mark a route slow

- **Surfaces:** `ui`, `api`
- **Given** a cluster of five servers whose routes take about 0.4 ms, one of which took 4 ms in the newest collection only
- **When** I open Topology, or call `GetTopology`
- **Then** no route is marked slow; when the next collection measures the same route at 4 ms again, it is marked "Slow"

### See a gateway pair that is down

- **Surfaces:** `ui`, `api`
- **Given** two clusters whose gateway connections were all up an hour ago and are all gone in the newest collection
- **When** I open Topology, or call `GetTopology`
- **Then** the pair is first among the gateways, drawn as "gateway down · 0 of 4 up" with both clusters marked, and `GetTopology` gives the drops it counted in the last hour

### Find a flapping route

- **Surfaces:** `ui`, `api`
- **Given** a cluster in which one of the three pooled routes between nats-eu-3 and nats-eu-5 was replaced twice in the last hour and once earlier today
- **When** I open Topology, or call `GetTopology`
- **Then** the eu box reads "· 2 drops/h" in the warning colour on its routes line and "route nats-eu-3 – nats-eu-5 dropping" in its note, the routes table shows 2 and 3 drops for the pair, and `GetTopology` gives `drops_1h` 2 and `drops_24h` 3

### A restart is not a route drop

- **Surfaces:** `api`
- **Given** a server that restarted between two collections, so every route to it reconnected
- **When** a client calls `GetTopology`
- **Then** no route of that server counts a drop for that pair of collections

### See a mirror falling behind across clusters

- **Surfaces:** `ui`, `api`
- **Given** a stream in ap that mirrors a stream in eu, for which `stream-015` (Mirror lagging) fires
- **When** I open Topology
- **Then** the replication table lists it first, in the warning colour, with "stream-015" linking to the finding and the finding's detail under its name, and the ap box's note reads "mirrors from eu"

### Spot a server using far more memory

- **Surfaces:** `ui`
- **Given** a cluster whose servers use about 210 MiB and one that uses 1 GiB, for which `server-008` fires
- **When** I open Topology
- **Then** each chip shows its memory, and the 1 GiB server's chip is in the warning colour with "server-008" under it, linking to the finding

### Tell a site of unknown state from a disconnected one

- **Surfaces:** `ui`
- **Given** a leaf node site last seen on a cluster one of whose servers did not answer LEAFZ in the newest collection
- **When** I open Topology
- **Then** the site is listed after the sites not connected, not in the warning colour, as "state unknown" naming the server whose LEAFZ did not answer

### Read the links from the API

- **Surfaces:** `api`
- **Given** a supercluster of three clusters
- **When** a client calls `GetTopology` for the newest collection
- **Then** it receives each cluster's routes, the gateways between clusters and the leaf nodes, with their round trip times and the drops of the 24 hours before

## Not supported / out of scope

- **No route drop per connection.** A pair counts one drop per pair of collections however many of its pooled or pinned routes went; the number of connections replaced is not shown.
- **No replication through a leaf node, another account or another domain.** External mirrors and sources, and copies whose origin no server of the system reports, are not listed; nor are copies inside one cluster.
- **No memory limit.** nats-server does not report the memory limit of its process or container, so the page compares a server's memory with the other servers', not with a limit.
- **No traffic between leaf nodes and their local clients.** A leaf node's own clients are seen only if nats-lens watches that leaf node's system too.
- **No changes to links.** nats-lens does not add, remove or reconfigure routes, gateways or leaf nodes.
- **Clusters split into groups of similar size are not judged.** No route of such a cluster is called slow, even when one group is far from the other; the same holds when no group holds more than half of the servers.
- **No drops shorter than a collect interval.** A drop and reconnect between two collections counts only when the connection ID changed; one that changed nothing is not seen.
