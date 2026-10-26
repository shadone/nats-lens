# Multiple systems and the Fleet view

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** partial — a cluster switcher inside a supercluster is planned
- **Related:** [overview.md](overview.md), [running-nats-lens.md](running-nats-lens.md), [monitoring-snapshots.md](monitoring-snapshots.md), [history-and-archive.md](history-and-archive.md), [sql-access.md](sql-access.md), [metrics-and-alerting.md](metrics-and-alerting.md), [navigation.md](navigation.md), [servers.md](servers.md), [topology.md](topology.md), [design.md](../design.md)

## What it does

One nats-lens watches several NATS systems at once, for example production, staging and an edge deployment of leaf nodes. Each system has its own credentials, its own collector and its own database, so one system's trouble or size never affects another's history. The API answers each call for the system it names, `ListSystems` summarises every system from collected data, `/metrics` labels every series with its system, the sidebar's switcher moves between systems, and the Fleet page shows every system on one screen, worst first.

## Behavior and rules

- **A system is one NATS deployment with its own system account:** a single server, a cluster, or a supercluster joined by gateways. The configuration lists systems under `systems`; a configuration without the list is one system, with the id `system_id` (`default` unless set).
- **One database per system.** Each system's collections, events, findings and mutes go to a database and an archive of their own, in the system's data directory. Losing or rebuilding one system's database touches no other.
- **A data directory belongs to one system.** On first start nats-lens writes the system's id into its data directory (`system.json`). A directory marked for another system refuses the start, so a mistyped id or directory never mixes two systems' data. A directory from a single-system nats-lens, without the file, is taken over.
- **Own credentials, own collector.** Each system has its own NATS URL, system account user, account connections, inventory file and Kubernetes watcher. The collect interval and the checks are shared.
- **Every system is checked before any starts.** A data directory of another system, or a missing file that the command reads (the system's credentials, TLS files and inventory file; with event capture also the account files and the kubeconfig), refuses the start with one error that names every broken system. A database that cannot be opened also stops nats-lens at start.
- **One system down stops only its own collection.** A system whose NATS cannot be reached, whose credentials are rejected, or whose setup fails after the start (for example a kubeconfig that does not parse) skips its own collections and is tried again; the other systems, the API and readiness go on. Its status says why, with the error.
- **Collection status per system.** nats-lens knows for each system whether it is starting, collecting, not connected, or closed for good after rejected credentials, the newest connection error, and when its newest collection started. `ListSystems` reports it in each system's summary.
- **Every API call is per system.** Each call names its system; an empty name reads the first configured system, and an unknown one fails with the list of known ids, except in `GetShellStatus`, which answers it. The web UI keeps the system in the address, so a shared link opens the same system.
- **Fleet summaries in the API.** `ListSystems` returns one summary per system from data already collected, so the Fleet view never waits for NATS: the collection state (collecting, not connected, connection closed) with its error and since when, the newest epoch's age, the numbers of servers expected and answering, clusters, streams and connections, the worst finding and the tally, and a strip of the worst status per bucket of the range. A system that fails or takes longer than 5 seconds gets an error on its own card; the others still return. Calls for the same range and strip that arrive while a summary runs share it, so the parallel calls of one page load all get the same data; a summary that runs past its 5 seconds is not started again until it returns, no system has more than two summaries running at once, and calls refused for either reason get the error at once. After a restart of nats-lens, a system that is not collecting is still reported from the collector's stored connection events.
- **Shell status in the API.** `GetShellStatus` is the one call the web UI's shell makes per system. It returns the system's freshness, servers and findings tally over a range, read as `GetOverview` reads them but without its tiles and windows; an unset range is the last two collect intervals. That part has 10 seconds; when it fails or runs out of time, the response says why in `error_code` and `error` and still carries everything else. With them come every system, in configuration order, with its badge state: the status, the collection state, whether it collects, an error when its summary could not be read, and `pending` while its first summary is still being read. The badge state comes from the system's summary over the last 24 hours with one strip cell, run as `ListSystems` runs summaries (5 seconds each, calls in flight shared). A summary that was read is kept for one collect interval, so shells that poll together read each system once per interval; once it expires it is served while one read in the background replaces it, so only a system with no summary kept makes a call wait, and at most half a second, after which it is `pending`. The runtime's collection status is applied on every call (proposed, not yet confirmed). A system that is not configured is no error here: the systems come back with `system_known` false, and nothing of that system is read.
- **A switcher.** The sidebar's switcher at the top moves between systems; a cluster switcher inside a supercluster is planned.
- **A switch shows nothing of the previous system** (proposed, not yet confirmed). After a switch of system, a page's lists, tiles, charts and matrices load afresh; the previous system's answer never stands in under the new system's name. A change of range, filter or page within one system keeps the previous answer on screen until the next one arrives, except a change of account on a list scoped to the account (Subjects, Micro services, Applications, Consumers, Connections, KV and object stores, and the Overview's busiest consumers and stream placement), which loads afresh ([navigation.md](navigation.md)). The system list and the Fleet badge cover every system, so they stay through a switch.
- **Info findings are notices.** Critical, warning, not collecting and summary unavailable are trouble. A system whose only firing findings are info is OK: its card says "ok", carries the number of info findings beside the state ("1 info"), and names the worst of them as its headline.
- **The Fleet page, worst first.** One card per system: critical, warning, not collecting, summary unavailable, no data, muted, then OK, with the systems that have info findings among the OK ones; systems in the same state keep their configuration order. A system that is not collecting, by its collection state or by its freshness, is "not collecting" whatever its stored findings say.
- **A Fleet card.** The name and environment; the state as a shape and a word; a headline with the worst finding and how many more fire, or the collection error and since when; the servers answering of expected (in red when fewer answer), clusters, streams and connections, with a dash for a number not observed; a strip of 48 cells, the worst status of each part of the range, with a legend above the cards that draws each kind of cell; and the freshness: "live · 6s" while collections arrive, "last data 3h ago" once they stop, the newest collection's time in a fixed range. A system that is not collecting shows its counts dimmed under "At last data, 3h ago", since they are from its newest epoch. Clicking a card opens that system's Overview with the range kept.
- **A summary that failed** says "summary unavailable" with the error, and its card still opens the system.
- **Fleet follows the header's range.** In a live range it asks again every 30 seconds, never while the previous call is still pending.
- **The Fleet badge.** The sidebar's Fleet entry carries the number of systems in trouble or without data, shaped by the worst; a system with only info findings is not counted. It reads the badge states of `GetShellStatus`, so it costs no call of its own ([navigation.md](navigation.md)).
- **Metrics name the system.** The `/metrics` series carry the system they belong to, so one alert rule covers every system.
- **Commands per system.** `natslens once -system ID` collects one system; `natslens sql` and `natslens checks run` take `-config FILE -system ID` to read one system's database.

## Scenarios

### Switch to another system

- **Surfaces:** `ui`
- **Given** the Streams page of `shop-prod` is open and `shop-staging` answers slowly
- **When** I pick `shop-staging` in the switcher
- **Then** the page shows that it is loading until `shop-staging` answers, and never lists `shop-prod`'s streams under `shop-staging`

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
- **When** I open the Fleet page
- **Then** production comes first with the finding as its headline, and staging says that its credentials are rejected and when its last data arrived

### A system with only info findings

- **Surfaces:** `ui`
- **Given** the edge system has one info finding and nothing else fires
- **When** I open the Fleet page
- **Then** its card says "ok" with "1 info" beside it and the finding as its headline, it is listed among the OK systems, and the Fleet badge does not count it

### A summary that cannot be read

- **Surfaces:** `ui`
- **Given** one system's summary fails with a timeout
- **When** I open the Fleet page
- **Then** its card says "summary unavailable" with the error, the other cards are complete, and clicking it opens that system's Overview

### Parallel calls share one summary

- **Surfaces:** `api`
- **Given** three calls to `ListSystems` for the last 24 hours arrive together
- **When** a system's summary takes two seconds
- **Then** every call gets that system's summary, and its database is read once

### A slow system does not hold up the Fleet

- **Surfaces:** `api`
- **Given** three systems, one of whose databases does not answer within 5 seconds
- **When** I call `ListSystems`
- **Then** that system's summary carries an error, and the other two summaries are complete

## Not supported / out of scope

- **No findings across systems.** Each check judges one system; no check compares systems with each other.
- **No shared inventory.** Each system has its own inventory file.
- **No high availability.** One nats-lens process still holds every system's database.
- **No system added while running.** The systems are read at start; a change to the list needs a restart.
