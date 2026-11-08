# Connections

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [client-checks.md](client-checks.md), [checks.md](checks.md), [events.md](events.md), [applications.md](applications.md), [monitoring-snapshots.md](monitoring-snapshots.md)

## What it does

The Connections page lists the client connections of the newest collection in the time range, one row per connection on a server, with who the client is and how it behaves: its round trip time, the bytes the server holds for it, its stalls and its traffic. It hides tooling connections by default, so that nats-lens's own connections and a port-forwarded CLI do not crowd the list.

## Behavior and rules

- **First visit.** The page reads data for the current scope while its view loads. The shell stays available, with loading and reload recovery described in [navigation](navigation.md).

- **One row per connection.** A connection is identified by its server and connection ID. The list shows the connections of the newest collection in the range, 100 per page.
- **Columns.** Name (a link to that client's events), finding status and links, server and connection ID, client library and version, account and user, address, time connected, time idle, round trip time, pending bytes, messages in and out, stalls and subscriptions. Pending bytes and stalls above zero are marked.
- **Finding status and direct links.** Each row links applicable client findings in the selected range, including resolved and muted findings, with the state returned by the findings API. `client-001` (RTT) and `client-008` (library) match the service before the first `@`; `client-002` to `client-005` match the full connection name. `client-007` applies to an empty service name. Credential-keyed authentication findings (`client-006`) are not inferred from a connection name. A service finding covers its pods; it does not claim that every pod violates the check. Proposed, not yet confirmed.
- **RTT and finding time.** RTT is one sample from the newest collection; finding state is at the selected range’s end, using the API’s current versus historical range rules. `client-001` follows the configured thresholds and persistence rules across collections. This page adds no RTT threshold. An earlier collection retained by pagination can therefore show a sample from before a finding changed state.
- **Unknown is not healthy.** Loading or failed finding reads show Unknown and failures offer a retry. One shared batch reads at most 500 findings across the system, including resolved and muted ones; it makes no per-row requests and is reused across pages of the collection. Live batches refresh on the collector interval; fixed-range batches refresh on invalidation, such as after muting. If more results exist, a note links to Findings and unmatched rows show Unknown (incomplete). With a complete batch, an unmatched row says No matching finding, which does not assert that all checks ran or passed. A new system, range or collection does not inherit another batch’s status. Proposed, not yet confirmed.
- **Filters.** By server, by part of the connection name, and by account. The filters live in the address; the page number does not.
- **The server filter offers every server seen in the range.** The choices come with the connections: `ListConnections` returns, in `servers`, the sorted names of every server seen in the range, the servers `ListServers` lists, whatever the filters and the page. Choosing one server keeps the others on offer, and a server without connections is offered too. The chosen server stays selected while the connections load, when the call fails, and when the range does not name it. The page makes no `ListServers` call.
- **Tooling is hidden.** nats-lens's own connections and clients from 127.0.0.1 or ::1 are hidden, and a note counts them: "N tooling connections hidden: nats-lens's own, or clients from a loopback address." "Include tooling" shows them, marked "tooling". A name filter also shows the tooling connections it matches, and says so. When `checks.loopback_is_tooling` is false, or the loopback guard has turned the loopback rule off ([checks.md](checks.md)), loopback clients are listed like any other client.
- **Single-page results.** The result count stays visible, but the page number and Previous/Next controls are omitted when the first page has no next cursor. A later page keeps its controls even when it is the last page.
- **Pages stay on one collection.** Paging keeps the collection of the first page, so new collections do not shift the pages. A page token whose collection has been removed by retention is refused, and the first page must be read again.
- **Stored connections only.** nats-lens stores at most `connz_limit` (4096) connections per server per collection; a server with more shows only those.
- **Grouped by application.** The [Applications](applications.md) page groups these connections by the application in their name, and each application's name links here with the name filter.

## Scenarios

### Find the client with a full queue

- **Given** a client whose server holds 6 MiB for it
- **When** I open the Connections page
- **Then** its row shows 6 MiB under Pending, marked
- **And** `client-002` fires for its name

### Find every connection of one service

- **Given** connections named `resize-worker@<pod>` on three servers
- **When** I type `resize-worker` into "name contains" and press Filter
- **Then** the list shows those connections on every server, with their round trip times

### Discover a service RTT finding from a pod

- **Given** `orders-service@orders-service-a` has a high RTT sample and `client-001` applies to `orders-service` in the selected range
- **When** I open Connections
- **Then** that row links the service finding and shows its state, with Muted if the finding is muted
- **And** the link retains the system and time range; the connection name still links to its events

### A sample does not establish persistent high RTT

- **Given** a connection has a high RTT in one collection but no matching finding in the complete batch
- **When** I open Connections
- **Then** the row says No matching finding and the page explains persistence across collections
- **And** loading, failed or incomplete finding data shows Unknown instead of implying healthy status

### Show my own CLI

- **Given** I run the `nats` CLI through a port-forward, so it connects from 127.0.0.1
- **When** I tick "Include tooling"
- **Then** its connection appears, marked "tooling"

### List connections from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** I call `ListConnections` with a server filter
- **Then** I receive that server's connections with the number of tooling connections hidden, and a page token for the next page
- **And** `servers` names every server seen in the range, not only the one I filtered by

## Not supported / out of scope

- **Only the newest collection.** The page shows one moment, not each connection's history; a connection that came and went between two collections is not listed. Its connect and disconnect events appear on [Events](events.md) only if nats-lens captured them in the selected range.
- **No subscription subjects here.** The list shows each connection's subscription count. nats-lens also collects each server's subscription list every few minutes, and the [Subjects](subjects.md) page shows the subjects by account, with the connections that subscribe to them.
- **No disconnecting.** nats-lens does not kick a client.
- **No search by address.** Filters are by server, name and account.
