# Demo mode

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** shipped
- **Related:** [overview.md](overview.md), [running-nats-lens.md](running-nats-lens.md), [overview-page.md](overview-page.md), [multiple-systems.md](multiple-systems.md), [design.md](../design.md)

## What it does

`natslens demo` serves the web UI and the API with three fixed systems, without NATS and without a database. `example-prod`, the default, is a three-server cluster a few hours after a Kubernetes node upgrade restarted all three servers. `example-global` is a supercluster of 24 servers in the clusters `eu`, `us` and `ap`, joined by gateways, after a node upgrade left one server down. `example-staging` has been watched for nine hours and has stopped collecting: its credentials were rejected three hours ago. The demo is for seeing what nats-lens shows before connecting it to a real system, and for working on the UI without a cluster.

## Behavior and rules

- **No NATS, no database.** The demo answers every API call from built-in scenarios; nothing is collected or stored on disk.
- **Three systems.** `ListSystems` lists `example-prod`, `example-global` and `example-staging` in that order, and a call without a `system` reads `example-prod`. Each system has its own servers, streams, consumers, findings, events, timeline and series, and its own mutes.
- **example-prod.** Three servers in one cluster; the node upgrade restarted each of them, an expected durable consumer was deleted (`consumer-010`), one consumer's copies on the servers were created at different times (`consumer-009`), and a backlog grows (`consumer-002`, flapping).
- **example-global.** 24 servers: 9 in `eu`, 9 in `us` and 6 in `ap`, with the meta leader in `eu`. A node upgrade of the `us` pool restarted three servers and left `nats-us-4` down, so two streams run on two of their three replicas. `nats-ap-2`'s store is 71% full and full in under 14 days at its growth (`server-014`), two `ap` servers run an older version (a muted finding), `nats-ap-3` runs with a larger maximum payload than the others, and a client is dropped as a slow consumer on `nats-us-3`. `nats-us-4`'s pod is Pending on no node. `nats-ap-2` also runs at about 70% CPU, so it is the busiest server of the CPU band.
- **Every placement square.** `example-global` also shows the replica states that only arise in trouble: `nats-ap-4` kept an old `max_age` of `customer-notifications` (a disagreeing square, "servers disagree" and `stream-007`), `nats-us-3` came back from the upgrade without its replica of `return-requests` (a missing square and `stream-010`), and `basket-events` has no leader in the newest collection (squares of unknown state, no finding until a second collection without one).
- **A story to replay.** `example-global`'s `us` pool upgrade, 08:44 to 09:20 in the scenario's frame, holds what the Replay page tells: `nats-us-1` to `nats-us-3` announce lame duck mode a second after their pods are killed, the stream and consumer leaders they held are elected on other servers, their clients disconnect and connect to the new process, and each comes back with the streams and consumers it held before, or fewer. `nats-us-4`'s leaders are elected elsewhere when it shuts down. The durables `restock-alerts` and `stock-audit-export` on `inventory-changes` are held by `nats-us-0` to `nats-us-2` until `nats-us-2`'s restart and by no server after, without a delete event; `consumer-013` fires for each a minute later and resolves an hour after their last report, after the window. `nats-us-4` stops answering and takes `KV_label-templates`, its only replica, with it. `example-prod`'s deleted `analytics-aggregator` is a gone asset with a delete event that names the client, and a moment after `example-staging`'s data ended is a time without collections. Proposed, not yet confirmed.
- **Links to draw.** `GetTopology` answers for every demo system; the Topology page itself is not built yet. `example-global`: a gateway from every server to each other cluster, `eu`-`us` 84 ms, `eu`-`ap` 166 ms and `ap`-`us` 212 ms, with `nats-ap-1`'s gateway to `us` reconnecting three times in the last hour (13:41, 13:58, 14:17); the gateways to `nats-us-1` to `nats-us-4` moved to other servers when the upgrade stopped them, drops of their reporters on the day. Three pooled routes between any two servers of a cluster at 0.3 to 0.6 ms, `nats-eu-3`'s at 4 ms, so they stand out as slow. 30 warehouses and pickup points of the shop as leaf node sites in account `APP` on `eu` and `us`: `pickup-0417` dropped 17 times in the last day and has not connected since 13:52, `warehouse-eu-03` reconnected twice, `pickup-0420` connected in the newest collection, so its drops are not counted yet, and the sites on the `us` servers the upgrade stopped moved to other hubs without a drop. `example-prod` and `example-staging` are one cluster each with routes only, `example-prod` with a route of its own for `APP` besides the pool; `example-staging` reads its last collection, three hours old. The Server page lists the same routes and round trips and counts each server's leaf node sites. Proposed, not yet confirmed.
- **Assets and leaders over time.** The vanished durables and `example-prod`'s deleted consumer are listed on the Consumers and Placement pages and in the stream detail for a range whose newest collection is before they went, and not after; the durable consumer count counts them until then. Every page reads the leaders of the range's newest collection, and they change only at an election event: when a leader stops answering, its assets have no leader until one is elected, by the scenario's own events or, where it writes none within 90 seconds, by the demo, which elects the first replica that answers (a consumer its stream's new leader) and writes that election as an event. The leader's "since" is the election that made it leader, or its process's start. Every other asset is the same in every range. Proposed, not yet confirmed.
- **Kubernetes from its events.** Each node is cordoned, drained as its NATS pod leaves, upgraded and uncordoned, each event with the node, its node pool and its kubelet version in the payload as the watcher writes them, so Replay counts nodes and node pools. The timeline's Kubernetes lane and the node upgrade windows come from these events by the store's rules (`example-prod` 09:16 to 09:44, `example-global` 08:44 to 09:20), and the NATS lane draws the kinds the store draws. A node pool's own report of its upgrade names no node and is kept as a Kubernetes event of no kind of its own (proposed, not yet confirmed).
- **Changed findings.** `nats-us-4` announced its shutdown, so its `server-001` and the findings for the assets on it (`stream-002`, `stream-011`) fired as info while that counted as a planned restart and changed to warning when the restart window ended; they show the changed state, both transitions and info on the timeline before the change.
- **Every delivery rate.** Besides consumers that deliver, `example-global` has `reorder-planner`, paused between its nightly runs, at 0 per second, and `invoice-renderer`, created in the newest collection, without a rate yet. The missing consumer of `example-prod` has none either.
- **Growth as the store computes it.** Growth per day is the median of the slopes between pairs of points of each stream's own bytes over the range, as for real data: it is not shown for a range under 6 hours, nor for `example-staging`, whose data in any range spans less than 6 hours. `KV_session-cache` in `example-global` shrinks, so its growth is negative.
- **example-staging.** Three servers whose connections were closed after their credentials were rejected three hours ago: the collector's and the two that capture events, so Replay says events were not captured since then. Its summary says "not collecting" with the error and when it began, and its data, timeline and series end three hours before now. Its history began nine hours before now, inside the default range: its series, strip, finding and events start then, and `GetLensInfo` gives that time as `history_start`. The other systems' history reaches 30 days back. It has no Kubernetes watcher, so its servers have no pod placement.
- **Shifted to now.** By default the scenario is moved in time so that its "now" is the current time, and the live ranges work as with real data. `-fixed` keeps the scenario at its own time, so every response is the same on every run.
- **The same states as real data.** One test table lists the states of the API's responses that the UI shows in its own way (each placement square, growth set, unset and negative, each delivery rate, paused and missing consumers, each finding state, a partial collection, the newest collection of a past range, a collector that lost its connection, and Replay's restart, quorum, gone, gap and after-B steps, gone assets with and without a delete event, counts that agree with the steps, and A before the retention refused, and Topology's gateway pair with its round trips, a gateway that dropped in the last hour, a slow route, a leaf node site not connected and one whose drops are not counted yet, and a single cluster with routes only). The tests find every row both in the demo and in a test database whose tables, findings and inventory are written row by row to hold each state. The demo's collecting systems say demo where a real one says collecting.
- **The same shapes as real data.** The scenario uses the same event kinds, check codes and API messages as a real collector. Every finding, timeline row and finding event is a real check, with its code, its name and a detail worded as the check's query writes it, with the online shop's assets of the project's examples (streams such as `order-jobs` and `order-archive`, consumers such as `resize-worker`).
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

### See the states trouble brings

- **Surfaces:** `ui`, `api`
- **Given** the demo is running
- **When** I open the Overview and the KV Stores page of `example-global`, and call `ListConsumers` for it
- **Then** the Overview's streams show a disagreeing, a missing and an offline replica and a stream without a leader, `KV_session-cache` shrinks, and `reorder-planner` is paused at 0 per second while `invoice-renderer` has no rate

### Replay the node pool upgrade

- **Surfaces:** `api`
- **Given** the demo is running with `-fixed`
- **When** I call `GetReplay` for `example-global` with A 08:44:04 and B 09:20:30 UTC, its upgrade window
- **Then** the steps tell the three restarts with their lame duck announcements and the leaders they gave up, two durables on `inventory-changes` gone without a delete event after `nats-us-2`'s restart, the quorum loss of `inventory-changes`, `nats-us-4` stopping with `KV_label-templates` and the leaders it held, and after B the `consumer-013` findings resolving; three nodes of one pool were upgraded and four drained, and 150 clients reconnected

### Read the links of a supercluster

- **Surfaces:** `api`
- **Given** the demo is running with `-fixed`
- **When** I call `GetTopology` for `example-global`
- **Then** it lists the clusters `ap`, `eu` and `us` with `nats-us-4` missing, the `ap`-`us` gateway first with 3 drops in the last hour from `nats-ap-1`, `nats-eu-3`'s routes first as slow, and `pickup-0417` first among 30 leaf node sites, not connected, with 17 drops and its last hub `nats-eu-1`

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
