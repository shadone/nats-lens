# Topology

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [servers.md](servers.md), [server-checks.md](server-checks.md), [multiple-systems.md](multiple-systems.md), [monitoring-snapshots.md](monitoring-snapshots.md), [demo-mode.md](demo-mode.md), [navigation.md](navigation.md), [running-nats-lens.md](running-nats-lens.md), [design.md](../design.md), [the build plan](../plans/topology.md)

## What it does

The Topology page, under Cluster in the sidebar, draws how the servers of a system connect in the newest collection of the page's range: the routes inside each cluster, the gateways between the clusters of a supercluster, and the leaf node sites at the edge. It shows the round trip time of each link and how often it dropped, counted from the collections of the 24 hours before, so a slow or flapping link is found without logging in to each server. `GetTopology` serves the same to any API client.

## Behavior and rules

- **Clusters and gateways.** Each cluster is a box with its servers, its region and a note (meta leader, streams led, missing servers, a dropping gateway); a missing server, or one whose gateway dropped in the last hour, is in the warning colour, and a server opens its page. Gateways between clusters are lines labelled with their round trip time, such as "gateway 84 ms", and "· 3 drops/h" in the warning colour and dashed when they dropped in the last hour. A pair whose gateways are all down is still drawn, as "gateway down · 0 of 4 up"; a cluster that was removed shows so too, until its last gateway connection is more than 24 hours old. Up to four clusters sit in a row; more, or a phone, give a grid of boxes and a table of the gateways. A line under the map says drops are counted from the collections every collect interval.
- **Leaf nodes by hub cluster.** Under each cluster with leaf nodes, a box counts the sites seen on it, connected now or seen in the last day and not connected now, and says how many are not connected ("18 sites · 1 not connected", or "all connected"). Sites are counted by name, so a site bound to two accounts counts once.
- **Leaf nodes, worst first.** A table of leaf node sites: the hub server, round trip time, drops in the last 24 hours, subscriptions and account. A site not connected comes first, in the warning colour, with an en dash (–) for its round trip and subscriptions and "not connected, last seen 09:12 on nats-eu-1". A site connected to several hubs lists each of them. When a server of the cluster a site was last seen on did not answer LEAFZ, the site may be connected there: it is listed next, not in the warning colour, as "state unknown: LEAFZ did not answer on nats-us-2; last seen 09:12 on nats-us-1", naming only the servers of its own cluster, with en dashes for its round trip and subscriptions. A failure in another cluster leaves a known disconnect as it is. When the server cut the list at 500 sites, the page says how many of how many it lists.
- **Routes inside a cluster.** From and to, round trip time, pending bytes, route pool size and pinned accounts, for the cluster picked with a select or a click on its box (until one is picked, among the clusters with a slow route the meta leader's, else the first by name, a cluster of unknown name after every named one; the choice is in the address). A slow route comes first, marked "slow". A route only one end reports says which end.
- **Slow routes.** A route is slow when it stands out from the cluster's normal round trip, never under 2 ms. Two servers count as near when their round trip is under 2 ms, or under 3 times the typical round trip from a server to its nearest neighbour; near servers, directly or through others, form a group. The measured servers are those with at least one round trip reported. The cluster is judged only when its largest group holds more than half of the measured servers: a server outside that group, measured against it, is slow with all its routes that report a round trip, and a route inside the group is slow at 3 times the typical round trip of the group's other routes. A cluster split into groups of similar size, such as two zones of three servers 10 ms apart, has no normal group and nothing in it is slow. So one slow server, at 3 times the cluster's typical nearest round trip or more, stands out in a cluster of 3 or more, two in a cluster of 5 or more (two of 4 are not judged), a small group far from the rest is slow, and one slow link stands out in a cluster of 5 or more; zones under 2 ms apart, jitter and two servers alone are never slow. A route is slow only when the shown collection and the one before it both find it slow, so one spike cannot mark it. The one before is the newest earlier collection with routes, at most three collect intervals earlier, also for a past range whose start lies between the two (proposed, not yet confirmed). Without such a collection, as for the first collection after nats-lens starts or after a gap in collecting, nothing is slow; a route that collection did not measure, as when a server was restarting, is not slow either; and when the collect interval is not known, no collection counts as the one before and nothing is slow (each proposed, not yet confirmed). Each collection is judged by the full rule on its own routes, so a collection before in which only some servers reported routes is judged on those. A round trip close to a threshold can still make a route slow on one refresh and not on the next, but only after two collections on the slow side. In a cluster split into groups of similar size, a server that reports no round trip at all (during its restart, or when its ROUTEZ failed) leaves the measured servers, so for that collection the cluster is judged and the other group is slow.
- **Drops.** NATS sends no event when a route, gateway or leaf node disconnects, so nats-lens counts drops from the collections: between two collections at most three collect intervals apart, a gateway connection whose ID changed or that went away, and a leaf node connection that went away or moved to another hub, while the server that reported it answered both times. A gap in the collections, a failed answer and a server restart count no drop. A drop and reconnect within one collect interval shows only as a new connection ID. A count that no two such collections cover is shown as not observed, not as 0.
- **Only what exists.** For a single cluster without leaf nodes the page shows just the routes; without gateways there are no gateway lines, and without leaf nodes no leaf node boxes or table. A value a server did not report shows as an en dash (–), never as 0. Servers whose ROUTEZ, GATEWAYZ or LEAFZ did not answer are named under the map. Servers that report no cluster are placed in the cluster of a server whose route names them, else shown as "unknown cluster".
- **Built on stored data.** ROUTEZ, GATEWAYZ and LEAFZ are collected and stored every collection. A past range shows its own newest collection. Reading a day of a system with hundreds of leaf node sites needs more memory than the base deployment's 256 MiB (see [running-nats-lens.md](running-nats-lens.md)). The owner changed this on 2026-10-26, still to be built: the base will ask for 512 MiB ([design.md](../design.md#owner-review-of-proposed-decisions-2026-10-26)).

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
- **Then** the routes table opens on that cluster with the slow server's routes first, each marked "slow"

### One spike does not mark a route slow

- **Surfaces:** `ui`, `api`
- **Given** a cluster of five servers whose routes take about 0.4 ms, one of which took 4 ms in the newest collection only
- **When** I open Topology, or call `GetTopology`
- **Then** no route is marked slow; when the next collection measures the same route at 4 ms again, it is marked "slow"

### See a gateway pair that is down

- **Surfaces:** `ui`, `api`
- **Given** two clusters whose gateway connections were all up an hour ago and are all gone in the newest collection
- **When** I open Topology, or call `GetTopology`
- **Then** the pair is first among the gateways, drawn as "gateway down · 0 of 4 up" with both clusters marked, and `GetTopology` gives the drops it counted in the last hour

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

- **No route drops.** Drops are counted for gateways and leaf nodes only; a route that reconnected is not counted.
- **No stream mirrors between clusters.** The design board's "mirrors from" note is not shown.
- **No traffic between leaf nodes and their local clients.** A leaf node's own clients are seen only if nats-lens watches that leaf node's system too.
- **No changes to links.** nats-lens does not add, remove or reconfigure routes, gateways or leaf nodes.
- **Clusters split into groups of similar size are not judged.** No route of such a cluster is called slow, even when one group is far from the other; the same holds when no group holds more than half of the servers.
- **No drops shorter than a collect interval.** A drop and reconnect between two collections counts only when the connection ID changed; one that changed nothing is not seen.
