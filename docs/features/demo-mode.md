# Demo mode

- **Surfaces:** `ui`, `cli`
- **Status:** shipped
- **Related:** [overview.md](overview.md), [running-nats-lens.md](running-nats-lens.md), [overview-page.md](overview-page.md), [design.md](../design.md)

## What it does

`natslens demo` serves the web UI and the API with a fixed scenario, without NATS and without a database: a three-server cluster named `example-prod`, a few hours after a Kubernetes node upgrade restarted all three servers. It is for seeing what nats-lens shows before connecting it to a real system, and for working on the UI without a cluster.

## Behavior and rules

- **No NATS, no database.** The demo answers every API call from a built-in scenario; nothing is collected or stored on disk.
- **Shifted to now.** By default the scenario is moved in time so that its "now" is the current time, and the live ranges work as with real data. `-fixed` keeps the scenario at its own time, so every response is the same on every run.
- **The same shapes as real data.** The scenario uses the same event kinds, check codes and API messages as a real collector, with the online shop's assets of the project's examples (streams such as `order-jobs` and `order-archive`, consumers such as `resize-worker`).
- **Marked as a demo.** The header shows "demo" next to the cluster name.
- **Mutes in memory.** Mutes and lifts made in the demo work. A demo mute keeps its chosen end and stops applying when it expires or is lifted; mutes live in memory only and are gone when the demo stops. The mute dialog asks for a name, as without IAP.
- **Address.** `-listen` sets the address (`127.0.0.1:8080` by default), and `-allowed-host` the Host names the API accepts.

## Scenarios

### See nats-lens without a cluster

- **Surfaces:** `cli`, `ui`
- **Given** a built `natslens` binary
- **When** I run `natslens demo` and open http://127.0.0.1:8080
- **Then** the Overview shows the scenario's findings, tiles, timeline with the node upgrade window, events, streams and servers, with "demo" in the header

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

- **No other scenarios.** The demo has one built-in scenario; it cannot load a recorded database.
- **No collection.** The demo never connects to NATS.
- **No persistence.** Mutes made in the demo are lost when it stops.
