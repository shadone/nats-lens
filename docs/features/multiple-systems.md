# Multiple systems and the Fleet view

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** planned
- **Related:** [overview.md](overview.md), [monitoring-snapshots.md](monitoring-snapshots.md), [history-and-archive.md](history-and-archive.md), [navigation.md](navigation.md), [servers.md](servers.md), [topology.md](topology.md)

## What it does

Planned, for the current build phase; nothing here is built yet. One nats-lens will watch several NATS systems at once, for example production, staging and an edge deployment of leaf nodes. Each system keeps its own credentials, its own collector and its own database, so one system's trouble or size never affects another's history. A Fleet view lists every system on one page, worst first, and a switcher moves between them. All scenarios below describe intended behaviour.

## Behavior and rules

- **A system is one NATS deployment with its own system account:** a single server, a cluster, or a supercluster joined by gateways.
- **One database per system.** Each system's collections, events, findings and mutes go to a database and an archive of their own. Losing or rebuilding one system's database touches no other.
- **Own credentials, own collector.** Each system has its own NATS URL, system account user, account connections and collect interval. One system that cannot be reached stops only its own collection.
- **The Fleet view.** One card per system, worst first: its name and environment label, its state (critical, warning, info, ok, or not collecting), a headline with its worst finding (or why it is not collecting, such as rejected credentials and when data was last received), the numbers of servers, clusters, streams and connections, its freshness, and a strip of its worst finding per half hour over the last 24 hours.
- **A switcher.** The sidebar's switcher at the top moves between systems. Inside a supercluster a second switcher limits the pages to one cluster.
- **Every page is per system.** The Overview, the lists and the findings show the chosen system; links carry the system, so a shared link opens the same system.
- **Metrics name the system.** The `/metrics` series carry the system they belong to, so one alert rule covers every system.

## Scenarios

### Watch production and staging from one nats-lens

- **Surfaces:** `cli`
- **Given** a configuration with two systems, each with its NATS URL and system account credentials
- **When** I start nats-lens
- **Then** it collects both, each into its own database and archive

### See which system needs attention

- **Surfaces:** `ui`
- **Given** production has a critical stream finding and staging is not collecting because its credentials were rejected
- **When** I open the Fleet view
- **Then** production comes first with the finding as its headline, and staging says that its credentials are rejected and when its last data arrived

### One system is unreachable

- **Surfaces:** `ui`, `api`
- **Given** three systems, one of which cannot be reached
- **When** I open the other two
- **Then** their pages, findings and API answers are complete and current

## Not supported / out of scope

- **No findings across systems.** Each check judges one system; no check compares systems with each other.
- **No shared inventory.** Each system has its own inventory file.
- **No high availability.** One nats-lens process still holds every system's database.
