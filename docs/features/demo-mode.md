# Demo mode

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** shipped
- **Related:** [overview.md](overview.md), [running-nats-lens.md](running-nats-lens.md), [overview-page.md](overview-page.md), [multiple-systems.md](multiple-systems.md), [design.md](../design.md)

## What it does

`natslens demo` serves the web UI and the API with three fixed systems, without NATS and without a database. `example-prod`, the default, is a three-server cluster a few hours after a Kubernetes node upgrade restarted all three servers. `example-global` is a supercluster of 24 servers in the clusters `eu`, `us` and `ap`, joined by gateways, after a node upgrade left one server down. `example-staging` has stopped collecting: its credentials were rejected three hours ago. The demo is for seeing what nats-lens shows before connecting it to a real system, and for working on the UI without a cluster.

## Behavior and rules

- **No NATS, no database.** The demo answers every API call from built-in scenarios; nothing is collected or stored on disk.
- **Three systems.** `ListSystems` lists `example-prod`, `example-global` and `example-staging` in that order, and a call without a `system` reads `example-prod`. Each system has its own servers, streams, consumers, findings, events, timeline and series, and its own mutes.
- **example-prod.** Three servers in one cluster; the node upgrade restarted each of them, an expected durable consumer was deleted, and a backlog grows.
- **example-global.** 24 servers: 9 in `eu`, 9 in `us` and 6 in `ap`, with the meta leader in `eu`. A node upgrade of the `us` pool restarted three servers and left `nats-us-4` down, so two streams run on two of their three replicas. `nats-ap-2`'s store is 71% full, two `ap` servers run an older version (a muted finding), `nats-ap-3` runs with a larger maximum payload than the others, and a client is dropped as a slow consumer on `nats-us-3`. `nats-us-4`'s pod is Pending on no node. `nats-ap-2` also runs at about 70% CPU, so it is the busiest server of the CPU band.
- **example-staging.** Three servers whose collector connection was closed after its credentials were rejected three hours ago. Its summary says "not collecting" with the error and when it began, and its data, timeline and series end three hours before now. It has no Kubernetes watcher, so its servers have no pod placement.
- **Shifted to now.** By default the scenario is moved in time so that its "now" is the current time, and the live ranges work as with real data. `-fixed` keeps the scenario at its own time, so every response is the same on every run.
- **The same shapes as real data.** The scenario uses the same event kinds, check codes and API messages as a real collector, with the online shop's assets of the project's examples (streams such as `order-jobs` and `order-archive`, consumers such as `resize-worker`).
- **Marked as a demo.** The system switcher at the top of the sidebar shows "demo" under the system name, and the collecting systems' summaries say demo as their collection state.
- **Mutes in memory.** Mutes and lifts made in the demo work. A demo mute keeps its chosen end and stops applying when it expires or is lifted; mutes live in memory only and are gone when the demo stops. The mute dialog asks for a name, as without IAP.
- **Address.** `-listen` sets the address (`127.0.0.1:8080` by default), and `-allowed-host` the Host names the API accepts.

## Scenarios

### See nats-lens without a cluster

- **Surfaces:** `cli`, `ui`
- **Given** a built `natslens` binary
- **When** I run `natslens demo` and open http://127.0.0.1:8080
- **Then** the Overview shows the scenario's findings, tiles, timeline with the node upgrade window, events, streams and servers, with "demo" in the sidebar

### See several systems

- **Surfaces:** `api`
- **Given** the demo is running
- **When** I call `ListSystems`
- **Then** it lists `example-prod`, `example-global` with 24 servers in 3 clusters of which 23 answer, and `example-staging` as not collecting, with "nats: authorization violation" since three hours ago

### Get the same responses every time

- **Surfaces:** `cli`
- **Given** a test that compares API responses
- **When** I run `natslens demo -fixed`
- **Then** every response is the same on every run, at the scenario's own time

### Try a mute

- **Surfaces:** `ui`
- **Given** the demo is running
- **When** I mute a finding with a reason and my name
- **Then** the finding shows as muted until the mute ends, I lift it, or I stop the demo

## Not supported / out of scope

- **No other scenarios.** The demo has three built-in systems; it cannot load a recorded database, and its systems cannot be configured.
- **No collection.** The demo never connects to NATS.
- **No persistence.** Mutes made in the demo are lost when it stops.
