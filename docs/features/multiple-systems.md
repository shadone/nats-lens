# Multiple systems and the Fleet view

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** partial — the Fleet summaries in the API, the Fleet page and the system switcher in the UI are planned
- **Related:** [overview.md](overview.md), [running-nats-lens.md](running-nats-lens.md), [monitoring-snapshots.md](monitoring-snapshots.md), [history-and-archive.md](history-and-archive.md), [sql-access.md](sql-access.md), [metrics-and-alerting.md](metrics-and-alerting.md), [navigation.md](navigation.md), [servers.md](servers.md), [topology.md](topology.md), [design.md](../design.md)

## What it does

One nats-lens watches several NATS systems at once, for example production, staging and an edge deployment of leaf nodes. Each system has its own credentials, its own collector and its own database, so one system's trouble or size never affects another's history. The API answers each call for the system it names, and `/metrics` labels every series with its system. A Fleet view that lists every system worst first, and a switcher in the UI that moves between them, are planned.

## Behavior and rules

- **A system is one NATS deployment with its own system account:** a single server, a cluster, or a supercluster joined by gateways. The configuration lists systems under `systems`; a configuration without the list is one system, with the id `system_id` (`default` unless set).
- **One database per system.** Each system's collections, events, findings and mutes go to a database and an archive of their own, in the system's data directory. Losing or rebuilding one system's database touches no other.
- **A data directory belongs to one system.** On first start nats-lens writes the system's id into its data directory (`system.json`). A directory marked for another system refuses the start, so a mistyped id or directory never mixes two systems' data. A directory from a single-system nats-lens, without the file, is taken over.
- **Own credentials, own collector.** Each system has its own NATS URL, system account user, account connections, inventory file and Kubernetes watcher. The collect interval and the checks are shared.
- **Every system is checked before any starts.** A data directory of another system, or a missing file that the command reads (the system's credentials, TLS files and inventory file; with event capture also the account files and the kubeconfig), refuses the start with one error that names every broken system. A database that cannot be opened also stops nats-lens at start.
- **One system down stops only its own collection.** A system whose NATS cannot be reached, whose credentials are rejected, or whose setup fails after the start (for example a kubeconfig that does not parse) skips its own collections and is tried again; the other systems, the API and readiness go on. Its status says why, with the error.
- **Collection status per system.** nats-lens knows for each system whether it is starting, collecting, not connected, or closed for good after rejected credentials, the newest connection error, and when its newest collection started. The API does not show it yet; the planned Fleet summaries will.
- **Every API call is per system.** Each call names its system; an empty name reads the first configured system, and an unknown one fails with the list of known ids. The web UI passes the `?system=` of its address on.
- **Metrics name the system.** The `/metrics` series carry the system they belong to, so one alert rule covers every system.
- **Commands per system.** `natslens once -system ID` collects one system; `natslens sql` and `natslens checks run` take `-config FILE -system ID` to read one system's database.
- **The Fleet view (planned).** One card per system, worst first: its name and environment label, its state (critical, warning, info, ok, or not collecting), a headline with its worst finding (or why it is not collecting, such as rejected credentials and when data was last received), the numbers of servers, clusters, streams and connections, its freshness, and a strip of its worst finding per half hour over the last 24 hours.
- **A switcher (planned).** The sidebar's switcher at the top moves between systems. Inside a supercluster a second switcher limits the pages to one cluster.

## Scenarios

### Watch production and staging from one nats-lens

- **Surfaces:** `cli`
- **Given** a configuration with two systems, each with its NATS URL and system account credentials
- **When** I start nats-lens
- **Then** it collects both, each into its own database and archive

### One system is unreachable

- **Surfaces:** `api`
- **Given** two systems, one of which cannot be reached
- **When** I ask the API about the other one
- **Then** its servers, findings and metrics are complete and current, and nats-lens stays ready

### A missing credentials file

- **Surfaces:** `cli`
- **Given** two systems, one of which names a credentials file that does not exist
- **When** I start nats-lens
- **Then** it refuses to start, and the error names that system and the file

### A data directory of another system

- **Surfaces:** `cli`
- **Given** a data directory that nats-lens marked for `shop-staging`
- **When** I start nats-lens with a system `shop-prod` that uses that directory
- **Then** nats-lens refuses to start and names both systems

### See which system needs attention

- **Surfaces:** `ui`
- **Given** production has a critical stream finding and staging is not collecting because its credentials were rejected
- **When** I open the Fleet view (planned)
- **Then** production comes first with the finding as its headline, and staging says that its credentials are rejected and when its last data arrived

## Not supported / out of scope

- **No findings across systems.** Each check judges one system; no check compares systems with each other.
- **No shared inventory.** Each system has its own inventory file.
- **No high availability.** One nats-lens process still holds every system's database.
- **No system added while running.** The systems are read at start; a change to the list needs a restart.
