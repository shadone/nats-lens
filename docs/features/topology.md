# Topology

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [servers.md](servers.md), [server-checks.md](server-checks.md), [multiple-systems.md](multiple-systems.md)

## What it does

Planned; nothing here is built yet. The Topology page will draw how the servers of a system connect: the routes inside each cluster, the gateways between clusters of a supercluster, and the leaf nodes at the edge. It shows the round trip time and drops of each link, so a slow or flapping link is found without logging in to each server. All scenarios below describe intended behaviour.

## Behavior and rules

- **Clusters and gateways.** Each cluster is a box with its servers. Gateways between clusters are lines labelled with their round trip time and drops per hour.
- **Leaf nodes by hub cluster.** Per cluster, how many leaf node sites connect to it now and how many seen in the last day are not connected.
- **Leaf nodes, worst first.** A table of leaf node sites: the hub server, round trip time, drops in the last 24 hours, subscriptions and account.
- **Routes inside a cluster.** From and to, round trip time, pending bytes, route pool size and pinned accounts.
- **Only what exists.** For a single cluster the page shows just the routes; without gateways or leaf nodes those sections stay hidden.
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
