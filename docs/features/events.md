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
- **One row per event.** A row shows the time, the source, a badge for finding events (NEW, CHANGED, RESOLVED; the demo also shows FLAPPING), the event's type or kind, and its summary.
- **Open an event.** Opening a row shows the findings it raised (each with severity, code, name, value, remediation and related settings), its fields (kind, server, account, stream, consumer, client, client host, reason, time and receive time), "Raw payload" with the stored JSON, and "Link to this event".
- **One event on top.** A link with `?id=` shows that event, opened, above the list.
- **Linked entities.** Server, stream and consumer names in an event link to their pages, and a client name links to its connections.
- **Payloads on request.** The list does not carry raw payloads; opening "Raw payload" fetches the one event.
- **Routine events, API only.** `ListEvents` marks routine events and, with `hide_routine`, leaves them out, counts them and lists what they were by kind and client (at most 10 groups, largest first). Routine is a fixed rule: the kinds `client.churn`, `consumer.ack_sampled`, `api.audit` and `account.connections`, and a connect or disconnect of a tooling client, judged as the client checks judge a connection in the epoch the event falls in. The Events page does not use it yet.
- **Repeats folded, API only.** With `fold_repeats`, events with the same source, kind, server, account, stream, consumer, client name and reason, each at most 10 minutes after the previous one, become one row: the newest event, with the count and the time of the oldest. A Kubernetes event counts the occurrences Kubernetes reported for it. Pages hold rows; a folded list keeps to the events that existed when its first page was read, so events that arrive meanwhile move no row between pages.
- **Facets and histogram, API only.** With `include_facets`, the response counts the events per source and per server, each dimension without its own filter, and the events of the range with no filter. With `histogram_buckets` (at most 120), it counts warnings, other events and routine events in equal buckets over the range, routine events included even when hidden.
- **Old requests unchanged.** A request without these flags gets the same response as before they existed.

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

### Read only what is worth reading from the API

- **Surfaces:** `api`
- **Given** a day with hundreds of short connections from one client and a pod that failed scheduling five times in six minutes
- **When** I call `ListEvents` with `hide_routine`, `fold_repeats`, `include_facets` and `histogram_buckets: 48`
- **Then** the short connections are counted in `routine_hidden` and named in `routine_groups`, the five scheduling failures are one row with `repeats` 5 and `first_time` six minutes before it, and the facets and the 48 buckets count the same events

### Repeats far apart stay apart

- **Surfaces:** `api`
- **Given** two identical events 11 minutes apart
- **When** I call `ListEvents` with `fold_repeats`
- **Then** I receive two rows, each with `repeats` 1

## Planned

- **Worth reading first.** The page opens on the events worth reading, with routine events (a controller's connect and close every few seconds, tooling on a loopback address) hidden behind a count, "Show them", and "Change what counts as routine". The API side is shipped; the page and a configurable rule are not.
- **Repeats folded.** Repeats of one thing fold into one row with a count, such as "×5 in 6 min". The API side is shipped.
- **Facets with counts.** The sources and servers show how many events each holds, and a strip over the range shows when events came. The API side is shipped.
- **Subject and where.** Columns for the subject an event concerns and where it happened (server or namespace).
- **New events wait.** Events that arrive while I read wait behind a "N new" pill instead of moving the list.

## Not supported / out of scope

- **No free-text search.** Filters are by source, kind, entity and client name.
- **No export.** Events can be read through the API or with `natslens sql`, but the page has no download.
- **No events older than `retention`.** Older events are in the archive only ([history-and-archive.md](history-and-archive.md)).
- **At most 10 sources and 50 kinds** in one filter.
