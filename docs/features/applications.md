# Applications

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [connections.md](connections.md), [client-checks.md](client-checks.md), [monitoring-snapshots.md](monitoring-snapshots.md), [event-capture.md](event-capture.md), [events.md](events.md), [navigation.md](navigation.md), [overview-page.md](overview-page.md), [demo-mode.md](demo-mode.md), [design.md](../design.md), [applications plan](../plans/applications.md)

## What it does

The Applications page groups the client connections of a system by the application in their connection name, so an operator sees `resize-worker` with its four pods as one row instead of four connections spread over three servers. Each row gives the application's instances, its pods or client addresses, the servers it uses, its client library, its median round trip, subscriptions, pending bytes, its reconnects in the last 24 hours and its messages in and out per hour over the last 24 hours. Three summary cards above the table say how the connections spread over the servers, which client libraries are in use, and what the last 24 hours held. `GetApplications` gives the same in the API. The page reads only what nats-lens already stored and sends no request to NATS.

## Behavior and rules

- **Chart descriptions.** The traffic column has an independent [chart help](chart-help.md) control describing whole-hour counts, directions, per-row scaling and gaps without sorting the table.

- **First visit.** The page reads data for the current scope while its view loads. The shell stays available, with loading and reload recovery described in [navigation](navigation.md).

- **Replay context.** “Replay this window” opens fixed A and B from the selected range, using at most its last 24 hours and ending no later than now. The page explains that Replay covers all accounts and compares observed libraries alongside system-wide reconnects, findings and server upgrades. Library changes do not prove an application deployment. Replay collection evidence links back with an exact application-name filter and account scope; summary cards still cover the account scope. When the application is absent or outside the first 500 listed, the page explains that it is not listed. See [replay.md](replay.md) for comparison coverage and limits.

- **One collection.** The page reads the newest collection at or before the end of the range. The 24-hour counts cover the 24 hours before that collection, and the traffic the 24 whole hours before the collection's hour. The line under the title reads, for example, "Client connections grouped by the application in their name. 187 connections, 10 applications, in the collection of 14:29:56 UTC." A past range works as a live one. A range without a collection says "No collection in this range."
- **An application is the connection name before the first "@".** A service that names each connection `service@pod` is grouped by service. A connection without "@" is its own application. A connection without a name, or with nothing before the "@" (`@pod-1`), is grouped as `(unnamed)`. One name is one application across accounts: a row lists every account its connections use.
- **Instances and pods.** A pod is the part of the name between the first "@" and the next. Instances count the distinct pods of the connections whose name gives one, plus one per connection whose name gives none. The Pods column lists the first two pod names and the rest behind "+N more"; with three pods it lists all three, as a disclosure for one name takes as much room as the name. Where no name gives a pod, the column lists the client addresses instead, in a quieter colour: one address, the rest behind "+N more", and two addresses both. The API lists at most 10 pods and 10 addresses per application, with their totals; the ones beyond read "N not listed".
- **The name and its line.** The application's name links to the Connections page filtered by that name, which lists every connection whose name contains it (so `order-service` also lists an `order-service-v2`). A tooling row's link includes tooling. `(unnamed)` links nowhere. Under the name, a line gives the connections and, where the names give pods, the pods ("4 connections, 4 pods"); without an account scope, and with rows in more than one account, it also names the accounts. The application's client findings follow as links to their finding pages, a muted one marked "(muted)".
- **Servers.** Each server the application's connections are on is a chip with the server's name and the application's connections on it, most first, linking to the server's page. Three chips show and the rest open behind "+N more"; with four servers all four show.
- **Library.** One client library and version, such as "nats.js 3.0.2", or "mixed" in the warning colour with each version and its connections beneath it. A connection that reports neither language nor version counts for no library.
- **Round trip, subscriptions and pending.** RTT p50 is the median round trip over the connections that report one: "0.69 ms" below 1 ms, "2.4 ms" below 10 ms, whole milliseconds above, and "<0.01 ms" below 0.005 ms. Subs sums the subscriptions. Pending sums the bytes the servers wait to write to the connections, with the "max" of one connection beneath it when the application has more than one connection and the sum is above 0. A value no connection reports is a dash, never 0.
- **Reconnects in 24 hours.** The count follows `client-004`'s rule for each connection name, then sums the names. Per name, it is the smaller of the drops (disconnects for a reason other than "Client Closed" and "Server Shutdown") and the connects (`client.connect` events and the connects folded into `client.churn`). A pod that drops and a new pod that connects, as in a rolling deploy, are not a reconnect. A name pairs its events across accounts, as `client-004` does, so a page scoped to an account counts each name of the application that has an event in that account. Names seen only in events count for their application, a pod gone by the collection among them. The events counted have their `time` in the 24 hours before the collection. A count above 0 is in the warning colour.
- **Where a reconnect count links.** Only a count above 0 links. It links to the application's `client-004` finding (the first unmuted one, else the first). Without one, it links to the Events page filtered by the name, but only when every connection is named exactly as the application: the Events page filters by the whole connection name, so names with pods would find nothing. Otherwise the count is plain text.
- **Slow consumers, stale and short-lived connections.** For the same 24 hours, the API counts per application the disconnects whose reason contains "slow consumer" in any case (as `client-003` does), those with the reason "Stale Connection", and the connects folded into `client.churn` (as `client-005` does). The Last 24 h card shows the sums of the slow consumers and stale connections, not the per-row counts; the short-lived counts are in the API only.
- **Messages in and out over 24 hours.** Each row draws the 24 whole hours before the collection's hour from the hourly rollup ([monitoring-snapshots.md](monitoring-snapshots.md)). In is what the application's connections sent to the servers, out what the servers delivered to them. Messages in are a solid line and messages out a dashed line, with the totals beneath. The collection's own hour is partial and left out, so the line lags by up to an hour. An hour without a measurement is a gap in the line, never 0; a lone measured hour is a dot, and a row without any measured hour draws a dashed baseline with "not measured" beneath it. Each sparkline is scaled to its own row's busiest hour, so the lines of two rows do not compare; the numbers do. The accessible name gives the hours, both totals with their bytes, and "N of 24 hours measured" when not all were. When no row has all 24 hours measured, a note above the table says so, with the most hours any one row has.
- **Traffic that is lost.** A connection that closed between two collections loses what it did after the last one, and a server whose connection list was cut at `connz_limit` adds only the listed connections. The footnote says the first; the cut-list notice says the second when a list was cut.
- **Findings and order.** A row lists the client findings firing at the collection whose entity key is the application (`client-001`, `client-007` on the `(unnamed)` row, `client-008`) or one of its connection names (`client-002` to `client-005`); `client-006` is keyed by a user and an account, so it is listed on no row, unmuted first, then worst severity first. A row with an unmuted finding has the warning background and comes first. A muted finding is listed on its row and does not lift it. Then the rows go by connections, most first, then by name.
- **Sorting.** Every column sorts. A first click sorts Application and Library from A and the other columns largest first; a second click reverses. The address keeps the order as `?sort=`. Values that are not known sort last.
- **Spread card.** It names the server with the most of the listed connections and its share: "nats-0 serves 100 of 187 connections on 3 servers." When that server holds more than half and the applications use more than one server, the card has an info edge and adds "A restart of nats-0 moves 100 of 187 connections at once."; otherwise it adds "No server holds more than half." With one server, it says that a restart of it moves every connection.
- **Client libraries card.** The versions by the applications that use them, most first, at most four, the others counted: "nats.js 3.0.2 in 6 applications, go 1.54.0 in 4."
- **Last 24 h card.** The slow consumer disconnects and stale connections summed over the applications, and the applications with at least one reconnect: "0 slow consumers · 6 stale connections · 1 application reconnecting".
- **Tooling stays out.** nats-lens's own connections, and clients from a loopback address while the collection's loopback decision holds, are left out by the Connections page's rule, and a line counts them: "1 tooling connection hidden: nats-lens's own, or clients from a loopback address." **Include tooling** lists them, marked "tooling", and the address keeps the choice as `?tooling=1`. An application is tooling when every connection of it is; one with both kinds lists only its other connections while tooling is hidden. Events and rollup hours follow the same rule. When the scope holds only tooling connections, the page says so: "Only tooling connections in account APP in this collection: 1 connection. Include tooling to list them."; with no connection at all it says "No client connection in this collection."
- **Account scope.** The page offers the header's account scope ([navigation.md](navigation.md)). Scoped, it reads the connections and traffic hours of that account, and the events as the reconnect rule above says: a name pairs its events across accounts. A connection without an account reads as `$G`. The previous answer stays on screen while the next loads only for the same system and account.
- **Partial collection.** When the collection lacks a server's connection list (its CONNZ request did not end ok, or it answered nothing), a warning notice names the server: "nats-us-4 did not answer in this collection: its connections are not counted, and the counts, the spread and the traffic cover the servers that answered." The line under the title adds "over the servers that answered", and the Spread card counts only those servers and never says what a restart moves. A failure of another request, such as JSZ, leaves the page whole.
- **Cut connection list.** When a server's list was cut at the collector's `connz_limit`, a warning notice says that its other connections are not counted, nor their traffic.
- **Capture gaps.** When event capture stopped for part of the 24 hours (nats-lens stopped, or a connection that captures events was lost), an info notice says the counts can be low. The Last 24 h card reads "at least N", and a reconnect cell "≥N".
- **Events not captured.** When no event was captured up to the collection, an info notice says the 24-hour counts are not known, the reconnect cells are dashes, and the Last 24 h card reads "Not known: no event captured up to this collection."
- **At most 500 applications.** The API lists the first 500 in the page's order, with the total; the line under the title adds "the first 500 listed". The summaries count every application before the limit.
- **Phone width.** The cards stack in one column, and the table scrolls sideways in its own box.
- **The footnote** gives the "@" rule, what instances and pods count, that tooling is left out, where the traffic comes from and what it loses, how reconnects pair, and that each sparkline has its own scale.
- **Decisions.** The owner decided on 2026-10-26 to defer "Runs on" and to back the 24 hours of traffic with an hourly rollup. The owner confirmed the other rules of this page on 2026-10-26.

## Scenarios

### Open incident context and return to a library observation

- **Surfaces:** `ui`
- **Given** Applications over a fixed one-hour range
- **When** I follow “Replay this window”
- **Then** Replay uses that bounded hour and system, with explicit system-wide context
- **And** a client-library collection evidence link returns to the exact application and account at the observed collection


### See one service as one row

- **Surfaces:** `ui`
- **Given** `example-prod` in demo mode, where `resize-worker` names each pod's connection `resize-worker@<pod>`
- **When** I open Applications
- **Then** `resize-worker` is one row with 4 instances, "4 connections, 4 pods" under its name, two pod names and "+2 more" in Pods, and the chips `nats-0 2`, `nats-1 1` and `nats-2 1` in Servers

### Find the application that reconnects

- **Surfaces:** `ui`
- **Given** `example-prod`, where `callback-dispatcher` stopped answering pings six times in the last day ("Stale Connection") and came back each time, so `client-004` fires for it
- **When** I open Applications
- **Then** `callback-dispatcher` is the first row, on the warning background, with its `client-004` finding under its name
- **And** its reconnect count 6 is in the warning colour and links to that finding
- **And** the Last 24 h card reads "0 slow consumers · 6 stale connections · 1 application reconnecting"

### See that one server holds most clients

- **Surfaces:** `ui`
- **Given** `example-prod`, where the clients that left `nats-1` and `nats-2` during the upgrade stayed on `nats-0`
- **When** I open Applications
- **Then** the Spread card has the info edge and reads "nats-0 serves 100 of 187 connections on 3 servers. A restart of nats-0 moves 100 of 187 connections at once."

### See mixed client library versions and a muted finding

- **Surfaces:** `ui`
- **Given** `example-global`, where `shipping-service` on the `ap` servers still runs nats.js 2.29.3 and its muted `client-001` finding fires
- **When** I open Applications
- **Then** the `shipping-service` row reads "mixed" in Library, with "nats.js 3.0.2 on 120" and "nats.js 2.29.3 on 24" beneath it
- **And** its `client-001` finding is listed under its name, marked "(muted)", and the row keeps its place by connections

### A server did not answer

- **Surfaces:** `ui`
- **Given** `example-global`, where `nats-us-4` did not answer the newest collection
- **When** I open Applications
- **Then** a warning notice reads "nats-us-4 did not answer in this collection: its connections are not counted, and the counts, the spread and the traffic cover the servers that answered."
- **And** the Spread card says that it counts only the servers that answered, and says nothing of a restart

### Count reconnects while event capture had a gap

- **Surfaces:** `ui`
- **Given** `example-staging`, whose events connection lost its server for 50 seconds within the 24 hours
- **When** I open Applications
- **Then** an info notice says the counts can be low, the Last 24 h card reads "at least" before each count, and `catalog-api`'s reconnect cell reads "≥10"
- **And** as every `catalog-api` connection is named exactly so, the count links to the Events page filtered by `catalog-api`
- **And** a note says that no application has all 24 hours measured, and that the most any has is 6

### List the tooling connections too

- **Surfaces:** `ui`
- **Given** `example-global`, where the line above the table reads "1 tooling connection hidden: nats-lens's own, or clients from a loopback address."
- **When** I check **Include tooling**
- **Then** `natslens-example-global` is listed in account `SYS`, marked "tooling", and the address keeps `?tooling=1`

### A connection without a name

- **Surfaces:** `ui`
- **Given** `example-prod`, where a script connected without a name at 10:05
- **When** I open Applications
- **Then** it is the `(unnamed)` row, without a link, and its sparkline's accessible name says that 4 of 24 hours were measured

### A range before event capture

- **Surfaces:** `ui`
- **Given** `example-prod` and a range five days back, before any captured event
- **When** I open Applications
- **Then** a notice says that no event was captured, so the 24-hour counts are not known
- **And** the reconnect cells are dashes, and the Last 24 h card reads "Not known"

### List applications from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `GetApplications` with a range and an account
- **Then** it receives the applications of the range's newest collection in the page's order, at most 500, each with its instances, pods, addresses, servers, libraries, round trip, subscriptions, pending bytes, 24-hour counts, findings and 24 hours of traffic
- **And** the totals, the tooling connections hidden, the three summaries, and whether the list was cut, whether events were captured, whether capture had gaps, and which servers are missing

## Not supported / out of scope

- **No "Runs on".** The owner deferred on 2026-10-26 the namespace, workload and node of each client pod, as the Kubernetes watcher keeps no client pods. The page shows the pod names that the connection names give, and the client addresses.
- **No naming convention enforced.** Applications that do not name their connections are grouped by whatever name they give, or as `(unnamed)`.
- **No per-subject traffic.** The servers count messages per connection, not per subject.
- **The Overview counts applications differently.** Its Clients tile leaves out `(unnamed)`, so `example-prod`'s tile counts 9 applications where this page lists 10 ([overview-page.md](overview-page.md)).
- **No per-row slow consumer, stale or short-lived counts.** The API gives them per application. The page shows only the sums of slow consumers and stale connections; short-lived counts are in the API only.
- **No Events link for names with pods.** The Events page filters by the whole connection name, so the reconnects of `resize-worker@<pod>` link only to a `client-004` finding when one fires.
- **One collection and one day.** The page shows the newest collection of the range and the 24 hours before it; a longer history per application is not built.
