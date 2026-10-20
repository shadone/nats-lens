# Topology

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [servers.md](servers.md), [server-checks.md](server-checks.md), [multiple-systems.md](multiple-systems.md)

## What it does

Planned; nothing here is built yet. The Topology page will draw how the servers of a system connect: the routes inside each cluster, the gateways between clusters of a supercluster, and the leaf nodes at the edge. It shows the round trip time and drops of each link, so a slow or flapping link is found without logging in to each server. All scenarios below describe intended behaviour.

## Behavior and rules

- **Clusters and gateways.** Each cluster is a box with its servers, its region and a note (meta leader, streams led, missing servers, a dropping gateway); a missing server, or one whose gateway dropped in the last hour, is in the warning colour, and a server opens its page. Gateways between clusters are lines labelled with their round trip time, such as "gateway 84 ms", and "· 3 drops/h" in the warning colour and dashed when they dropped in the last hour. Up to four clusters sit in a row; more, or a phone, give a grid of boxes and a table of the gateways. A line under the map says drops are counted from the collections every collect interval.
- **Leaf nodes by hub cluster.** Under each cluster with leaf nodes, a box counts the sites seen on it, connected now or seen in the last day and not connected now, and says how many are not connected ("18 sites · 1 not connected", or "all connected").
- **Leaf nodes, worst first.** A table of leaf node sites: the hub server, round trip time, drops in the last 24 hours, subscriptions and account. A site not connected comes first, in the warning colour, with an en dash (–) for its round trip and subscriptions and "not connected, last seen 09:12 on nats-eu-1". A site connected to several hubs lists each of them. A site that may be connected on a server whose LEAFZ did not answer is listed next, not in the warning colour, as "state unknown: LEAFZ did not answer on nats-us-4; last seen 09:12 on nats-us-1", with en dashes for its round trip and subscriptions. When the server cut the list, the page says how many of how many sites it lists.
- **Routes inside a cluster.** From and to, round trip time, pending bytes, route pool size and pinned accounts, for the cluster picked with a select or a click on its box (until one is picked, a cluster with a slow route, else the meta leader's; the choice is in the address). A slow route comes first, marked "slow". A route only one end reports says which end.
- **Only what exists.** For a single cluster without leaf nodes the page shows just the routes; without gateways there are no gateway lines, and without leaf nodes no leaf node boxes or table. A value a server did not report shows as an en dash (–), never as 0. Servers whose ROUTEZ, GATEWAYZ or LEAFZ did not answer are named under the map.
- **Built on stored data.** ROUTEZ, GATEWAYZ and LEAFZ are collected and stored every collection today. NATS sends no event when a gateway or leaf node disconnects, so drops are counted from the collections, and a drop shorter than one collect interval is not seen.

## Scenarios

### Find a flapping leaf node

- **Surfaces:** `ui`
- **Given** a leaf node site that dropped its connection 17 times in a day
- **When** I open Topology
- **Then** the site is first in the leaf node table, with its drops and its hub server

### See a slow route

- **Surfaces:** `ui`
- **Given** one route inside a cluster with a much higher round trip time than the others
- **When** I open Topology
- **Then** that route stands out in the routes table with its round trip time

### Read the links from the API

- **Surfaces:** `api`
- **Given** a supercluster of three clusters
- **When** a client asks for the topology of the newest collection
- **Then** it receives each cluster's routes, the gateways between clusters and the leaf nodes, with their round trip times

## Not supported / out of scope

- **No changes to links.** nats-lens does not add, remove or reconfigure routes, gateways or leaf nodes.
- **No traffic between leaf nodes and their local clients.** A leaf node's own clients are seen only if nats-lens watches that leaf node's system too.
