# Connections

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [client-checks.md](client-checks.md), [checks.md](checks.md), [events.md](events.md), [applications.md](applications.md), [monitoring-snapshots.md](monitoring-snapshots.md)

## What it does

The Connections page lists the client connections of the newest collection in the time range, one row per connection on a server, with who the client is and how it behaves: its round trip time, the bytes the server holds for it, its stalls and its traffic. It hides tooling connections by default, so that nats-lens's own connections and a port-forwarded CLI do not crowd the list.

## Behavior and rules

- **One row per connection.** A connection is identified by its server and connection ID. The list shows the connections of the newest collection in the range, 100 per page.
- **Columns.** Name (a link to that client's events), server and connection ID, client library and version, account and user, address, time connected, time idle, round trip time, pending bytes, messages in and out, stalls and subscriptions. Pending bytes and stalls above zero are marked.
- **Filters.** By server, by part of the connection name, and by account. The filters live in the address; the page number does not.
- **The server filter offers every server seen in the range.** The choices come with the connections: `ListConnections` returns, in `servers`, the sorted names of every server seen in the range, the servers `ListServers` lists, whatever the filters and the page. Choosing one server keeps the others on offer, and a server without connections is offered too. The chosen server stays selected while the connections load, when the call fails, and when the range does not name it. The page makes no `ListServers` call (proposed, not yet confirmed).
- **Tooling is hidden.** nats-lens's own connections and clients from 127.0.0.1 or ::1 are hidden, and a note counts them: "N tooling connections hidden: nats-lens's own, or clients from a loopback address." "Include tooling" shows them, marked "tooling". A name filter also shows the tooling connections it matches, and says so. When `checks.loopback_is_tooling` is false, or the loopback guard has turned the loopback rule off ([checks.md](checks.md)), loopback clients are listed like any other client.
- **Pages stay on one collection.** Paging keeps the collection of the first page, so new collections do not shift the pages. A page token whose collection has been removed by retention is refused, and the first page must be read again.
- **Stored connections only.** nats-lens stores at most `connz_limit` (4096) connections per server per collection; a server with more shows only those.

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

## Planned

- **Grouped by application.** A page of clients grouped by the service in their connection name is planned in [applications.md](applications.md).

## Not supported / out of scope

- **Only the newest collection.** The page shows one moment, not each connection's history; a connection that came and went between two collections is not listed. Its connect and disconnect events are on the Events page.
- **No subscription subjects.** nats-lens asks the servers for subscription counts, not for the subjects of each subscription.
- **No disconnecting.** nats-lens does not kick a client.
- **No search by address.** Filters are by server, name and account.
