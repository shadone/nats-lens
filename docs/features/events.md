# Events

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [event-capture.md](event-capture.md), [kubernetes-context.md](kubernetes-context.md), [triage-and-mutes.md](triage-and-mutes.md), [replay.md](replay.md)

## What it does

The Events page lists the events of the time range, newest first: NATS advisories and system events, Kubernetes events, finding transitions and the collector's own records. It opens on the events worth reading, with routine events hidden behind a count and repeats folded into one row. A histogram shows when events came, and the sources and servers show how many events each holds. Filters narrow the list by source, server, kind, entity and client. An event opens in place to show the findings it raised, with their remediation, its fields, and its raw payload. Every event has its own link.

Slow-consumer investigations separate recorded disconnects from aggregate counter increases and expose capture limitations. Selecting one counter observation keeps the full supporting event interval ([servers.md](servers.md)).

## Behavior and rules

- **Chart descriptions.** The histogram legend has [chart help](chart-help.md) explaining captured event counts and the scale when routine events are hidden.

- **Worth reading first.** The page opens on "Worth reading": routine events are hidden and repeats are folded. "Everything" shows every event, one row each. Each choice shows its number of events, which follows the source and server filters.
- **Hidden as routine.** While routine events are hidden, a line above the list reads "Hidden as routine:" with their number and the groups the API names (at most 10, largest first, each with its count), and "Show them" switches to "Everything".
- **Empty results.** An empty feed explains the selected system, range, client and other active event filters before the facets. It distinguishes hidden routine events, no filter matches, and no captured events in the selected system and range. It offers "Show routine events" when routine events are hidden, and "Clear client filter" when a client is selected. Clearing the client preserves the system, account, range and other filters, synchronizes the visible client field, and returns to page one. Connections describes snapshots; Events describes captured history. A connection may predate capture, or its events may fall outside the range; an empty feed does not establish either cause. Loading, failed requests and previous-filter placeholders do not claim an empty result. A failed refresh of an empty result shows the error and Retry.
- **Histogram.** The page omits a histogram whose counts are all zero. Bars over the range count warnings (warning and critical events) on top, other notable events below them, and routine events at the bottom. In "Worth reading" the bars are scaled to warnings and notable events, and routine events stand faint behind them. The legend draws each bar. A bar's title gives its time and its three counts.
- **OCSP rejection warnings.** `server.ocsp_peer_rejected` appears as a non-routine NATS warning, filtered by its emitting server. Its raw peer object contains only the certificate fingerprint; it names no account, client, stream, or consumer.
- **Four sources, with counts.** NATS, Kubernetes, Findings and Collector, each with its number of events. With none chosen ("All"), every source is shown; several can be chosen at once.
- **Servers, with counts.** The busiest 8 servers with their numbers of events, then "N more" for the rest. Choosing a server filters the list to the events it reported; choosing it again clears it. The servers are not offered while a stream or consumer filters the list.
- **Filters.** Kinds as a comma-separated list (for example `consumer.deleted, stream.deleted`), and a client connection name. Links from a server, stream or consumer page add that entity as a filter; a stream or consumer filter shows with "Clear". A stream or consumer filter also matches events that carry the account's readable name instead of its ID.
- **Folded rows.** In "Worth reading", repeats of one thing are one row with the count and the time they span, such as "×5 in 6 min"; "×5" when they share one time. Opening a folded row shows the number of repeats and the time of the first. Finding events fold only with the same finding, and Kubernetes events only with the same object, so five stuck pods are five rows.
- **New events wait.** On a live range's first page, the list stays as it was read. Every 30 seconds nats-lens counts the matching events that arrived since, and shows them as an "N new" pill, which a screen reader announces; pressing it reloads the list.
- **Newest first, by time.** The list shows 50 rows per page; "Newer" and "Older" move through the pages by time, and the pager names only the page. The first page omits the pager when no older page exists. Later pages keep "Newer", including an empty last page. Going back to the first page shows it as it was read.
- **One row per event or fold.** A row shows the time, the source, a badge for finding events (New, Changed, Resolved; the demo also shows Flapping), the event's type or kind, its summary, and the repeats of a folded row.
- **Copy event text.** Event types and summaries are selectable in Events, entity feeds and finding timelines, whether a row is open or closed. Dragging to select text leaves the row unchanged. A subsequent click, Enter, Space or tap opens or closes it. Long text wraps within the row. Server recent-event summaries are selectable links.
- **Open an event.** Opening a row shows the findings it raised (each with severity, code, name, value, remediation and related settings), its fields (kind, server, account, stream, consumer, client, client host, reason, repeats and first time of a folded row, time and receive time), "Raw payload" with the stored JSON, and "Link to this event".
- **One event on top.** A link with `?id=` shows that event, opened, above the list.
- **Linked entities.** Server, stream and consumer names in an event link to their pages, and a client name links to its connections.
- **Payloads on request.** The list does not carry raw payloads; opening "Raw payload" fetches the one event.
- **Routine events.** `ListEvents` marks routine events and, with `hide_routine`, leaves them out, counts them and lists what they were by kind and client (at most 10 groups, largest first). Routine is a fixed rule: the kinds `client.churn`, `consumer.ack_sampled` and `account.connections`; an `api.audit` record of a call that succeeded and changes nothing (a failed call, or a create, update, delete, purge, snapshot, restore, pause, step-down or peer removal, is always shown); and a connect or disconnect of a tooling client, judged as the client checks judge a connection in the epoch the event falls in. `checks.include_tooling` does not change it: hiding routine events only changes what is shown. Hidden `client.churn` events still count in `client-005`, which fires for a client that opens many short-lived connections.
- **Repeats folded.** With `fold_repeats`, events with the same source, kind, server, account, stream, consumer, client name and reason, each at most 10 minutes after the previous one, become one row: the newest event, with the count and the time of the oldest. A Kubernetes event counts the occurrences Kubernetes reported for it. `total_count` and the facet counts count events, not rows. Pages hold rows; a folded list keeps to the events that existed when its first page was read, so events that arrive meanwhile move no row between pages.
- **Facets and histogram.** With `include_facets`, the response counts the events per source and per server, each dimension without its own filter, and the events of the range with no filter. With `histogram_buckets` (at most 120; the page asks for 48), it counts warnings, other events and routine events in equal buckets over the range, routine events included even when hidden.
- **Old requests unchanged.** A request without these flags gets the same response as before they existed.

- **Acknowledgement events.** Saving an acknowledgement writes a non-routine `finding.acknowledged` event in the same transaction. Events and the finding timeline can show its author and note. Acknowledgement events are records of saves, not a list of release or replacement receipts; those receipts in finding details last only for the current visit.
- Timeline event links carry a captured event boundary. The selected record and fixed-range feed honor it; an incompatible server that cannot acknowledge it is rejected. Exact window/overlap batches use the separate bounded `GetEventBatch` read; the ordinary feed remains broader time/filter context. Event membership is not an immutable snapshot or a guarantee of retained payloads.

## Scenarios

### Copy text from an event

- **Surfaces:** `ui`
- **Given** an open or closed event in Events, an entity feed or a finding timeline
- **When** I drag across its type or summary and copy the selection
- **Then** the selected text is available to paste and the row stays open or closed as it was
- **And** clicking the row afterwards, or pressing Enter or Space on its control, toggles its details

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

### Read what is worth reading

- **Surfaces:** `ui`
- **Given** a day with hundreds of short connections from one client and a pod that failed scheduling five times in six minutes
- **When** I open Events
- **Then** "Worth reading" is chosen, the short connections are counted on the "Hidden as routine" line under the label the API gives them, and the scheduling failures are one row reading "×5 in 6 min"

### See the routine events too

- **Surfaces:** `ui`
- **Given** routine events are hidden
- **When** I press "Show them"
- **Then** "Everything" is chosen and every event is its own row, routine ones included

### Narrow to one server

- **Surfaces:** `ui`
- **Given** the servers `nats-0`, `nats-1` and `nats-2` show 4, 3 and 3 events
- **When** I choose `nats-1`
- **Then** the list shows the events `nats-1` reported, and the link holds `?server=nats-1`

### Keep reading while events arrive

- **Surfaces:** `ui`
- **Given** I read the first page of a live range
- **When** three more matching events arrive
- **Then** the list stays where it is and a "3 new" pill appears; pressing it reloads the list with them

### Follow a connection with no matching event history

- **Surfaces:** `ui`
- **Given** the client name `media-worker` is selected and no captured events match it in the range
- **When** I open Events from Connections
- **Then** the empty state names the client and range above the facets and explains the difference between snapshots and captured history
- **And** an all-zero histogram and a first-page pager without an older page are absent
- **When** I press "Clear client filter"
- **Then** the client filter and visible client field clear, while the system, account, range and other filters stay selected

### Reveal an empty feed's hidden routine events

- **Surfaces:** `ui`
- **Given** only routine events match the active filters and Worth reading is selected
- **When** I open Events
- **Then** the empty state explains that matching events are hidden and offers "Show routine events"
- **And** the routine counter stays visible and the page does not claim that no events were captured

### Read a range without captured events

- **Surfaces:** `ui`
- **Given** no captured events exist in the selected system and range
- **When** Events finishes loading successfully
- **Then** the page says "No events captured in this range", names the scope and suggests a wider range
- **And** a failed request shows an error with Retry instead of claiming the range is empty

## Planned

- **Change what counts as routine.** Routine is a fixed rule today; a configurable rule and a link to it are planned.
- **Subject and where.** Columns for the subject an event concerns and where it happened (server or namespace).

### Read an acknowledgement event

- **Given** an operator saves an acknowledgement in a stored system
- **When** another operator reads Events in that time range
- **Then** a `finding.acknowledged` event records the author and note
- **And** the event is not hidden as routine

### Read an OCSP rejection

- **Given** a server rejected a TLS peer connection through OCSP verification
- **When** I filter Events by NATS, the server, and `server.ocsp_peer_rejected`
- **Then** I see the warning, its peer kind and reason, with no inferred client or account

## Not supported / out of scope

- **No free-text search.** Filters are by source, kind, entity and client name.
- **No export.** Events can be read through the API or with `natslens sql`, but the page has no download.
- **No events older than `retention`.** Older events are in the archive only ([history-and-archive.md](history-and-archive.md)).
- **At most 10 sources and 50 kinds** in one filter.
