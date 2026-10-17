# Applications

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [connections.md](connections.md), [client-checks.md](client-checks.md), [kubernetes-context.md](kubernetes-context.md), [consumers.md](consumers.md)

## What it does

Planned; nothing here is built yet. The Applications page will group client connections by the application in their connection name, so an operator sees `resize-worker` with its four pods as one row instead of four connections spread over three servers. Each row says where the application runs, how many instances it has, which servers it uses, which client library it uses, and how its connections behave. All scenarios below describe intended behaviour.

## Behavior and rules

- **An application is the connection name before "@".** A service that names each connection `service@pod` is grouped by service. A connection without "@" is its own application; one without a name is grouped as `(unnamed)`.
- **Instances.** Instances are counted per pod where the name gives one, and per connection otherwise.
- **Columns.** Application, where it runs (from the Kubernetes watcher where it knows the pod), instances, the servers it is connected to, client library and version, median round trip time, subscriptions, pending bytes, reconnects in the last 24 hours, and messages in and out in the last 24 hours.
- **Spread across servers.** A summary says how the connections are spread over the servers, for example that a restart of one server would move most clients at once.
- **Client libraries.** A summary lists the libraries and versions in use.
- **Last 24 hours.** A summary counts slow consumers, stale connections and reconnect loops.
- **Tooling stays out.** Tooling connections are not listed as applications, as on the Connections page.
- **Built on stored data.** Connection names, servers, libraries, round trip times and counters are in the collections, and reconnects in the events.

## Scenarios

### See one service as one row

- **Surfaces:** `ui`
- **Given** connections named `resize-worker@resize-worker-7d9f` and `resize-worker@resize-worker-k2p4` on two servers
- **When** I open Applications
- **Then** `resize-worker` is one row with 2 instances on 2 servers

### Find the applications that reconnect

- **Surfaces:** `ui`
- **Given** `callback-delivery` reconnected six times in the last day
- **When** I sort by reconnects
- **Then** `callback-delivery` comes first, linking to its `client-004` finding

### List applications from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client asks for the applications of the newest collection
- **Then** it receives one entry per application with its instances and numbers

## Not supported / out of scope

- **No naming convention enforced.** Applications that do not name their connections are grouped by whatever name they give, or as `(unnamed)`.
- **No per-subject traffic.** Message counts are per connection, not per subject.
