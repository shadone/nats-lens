# Events

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [event-capture.md](event-capture.md), [kubernetes-context.md](kubernetes-context.md), [triage-and-mutes.md](triage-and-mutes.md), [replay.md](replay.md)

## What it does

The Events page lists every event in the time range, newest first: NATS advisories and system events, Kubernetes events, finding transitions and the collector's own records. Filters narrow it by source, kind, entity and client. An event opens in place to show the findings it raised, with their remediation, its fields, and its raw payload. Every event has its own link.

## Behavior and rules

- **Four sources.** NATS, Kubernetes, Findings and Collector. With none chosen ("All"), every source is shown; several can be chosen at once.
- **Filters.** Kinds as a comma-separated list (for example `consumer.deleted, stream.deleted`), and a client connection name. Links from a server, stream or consumer page add that entity as a filter, which "Clear" removes. A stream or consumer filter also matches events that carry the account's readable name instead of its ID.
- **Newest first, by time.** The list shows 50 events per page; "Newer" and "Older" move through the pages by time, so events that arrive while I read do not shift them.
- **One row per event.** A row shows the time, the source, a badge for finding events (NEW, CHANGED, FLAPPING, RESOLVED), the event's type or kind, and its summary.
- **Open an event.** Opening a row shows the findings it raised (each with severity, code, name, value, remediation and related settings), its fields (kind, server, account, stream, consumer, client, client host, reason, time and receive time), "Raw payload" with the stored JSON, and "Link to this event".
- **One event on top.** A link with `?id=` shows that event, opened, above the list.
- **Linked entities.** Server, stream and consumer names in an event link to their pages, and a client name links to its connections.
- **Payloads on request.** The list does not carry raw payloads; opening "Raw payload" fetches the one event.

## Scenarios

### Find who deleted a consumer

- **Given** a consumer was deleted in the last hour
- **When** I enter `consumer.deleted` as the kind and press Filter
- **Then** I see the `consumer.deleted` event with the client that made the call, when nats-lens captured its audit

### Follow a finding to its cause

- **Given** a `consumer.max_deliveries` event that raised `consumer-005`
- **When** I open the event
- **Then** I see the raised finding with its remediation and related settings, and the event's stream sequence in its raw payload

### Show only the collector's own records

- **Given** nats-lens was restarted and NATS was unreachable for a while
- **When** I choose the Collector source
- **Then** I see `collector.gap`, `collector.not_connected` and `collector.reconnected` events with their times

### Read events from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** I call `ListEvents` with sources and kinds, and `GetEvent` with one ID
- **Then** I receive the matching events without their payloads, and the one event with its raw payload

## Planned

- **Worth reading first.** The page opens on the events worth reading, with routine events (a controller's connect and close every few seconds, tooling on a loopback address) hidden behind a count, "Show them", and "Change what counts as routine".
- **Repeats folded.** Repeats of one thing fold into one row with a count, such as "×5 in 6 min".
- **Facets with counts.** The sources and servers show how many events each holds, and a strip over the range shows when events came.
- **Subject and where.** Columns for the subject an event concerns and where it happened (server or namespace).
- **New events wait.** Events that arrive while I read wait behind a "N new" pill instead of moving the list.

## Not supported / out of scope

- **No free-text search.** Filters are by source, kind, entity and client name.
- **No export.** Events can be read through the API or with `natslens sql`, but the page has no download.
- **No events older than `retention`.** Older events are in the archive only ([history-and-archive.md](history-and-archive.md)).
- **At most 10 sources and 50 kinds** in one filter.
