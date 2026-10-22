# Subjects

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [streams.md](streams.md), [connections.md](connections.md), [monitoring-snapshots.md](monitoring-snapshots.md), [navigation.md](navigation.md), [micro-services.md](micro-services.md), [design.md](../design.md), [subjects plan](../plans/subjects.md)

## What it does

The Subjects page shows where messages can go in a system: each stream with the subjects it stores and who reads it, the client subscriptions by subject, each account's reply inboxes and its JetStream API. A field beside the filters says what a message on one subject would reach: the streams that store it, their consumers, mirrors and sourcing streams, and the core subscribers. Everything comes from what the NATS servers report; nats-lens never subscribes to an application subject. The servers do not count messages per subject, so the page shows a rate only where a stream stores the subject.

## Behavior and rules

- **One collection.** The page reads the newest collection at or before the end of the range, and the newest subscription collection at or before it, within the 24 hours before it. The line under the title names the collection's time. A past range works as a live one.
- **One row per stream.** A stream, KV bucket or object store has one row with every subject of its configuration in the first cell, one per line. A mirror has no subjects of its own: its pattern cell is a dash and its note reads "mirror of `<origin>`". A KV bucket's subjects are `$KV.<bucket>.>`, an object store's `$O.<store>.C.>` and `$O.<store>.M.>`. A row lists at most 20 subjects, then "+N more". One row per stream rather than per pattern is proposed, not yet confirmed.
- **Subscription rows.** One row per account and subject of client subscriptions: "3 connections in queue group order-service (order-service)", "1 connection (ops-monitor)". The queue group is named only when every subscription of the row shares it, so a subject with two queue groups reads "5 connections" with no group named. A row names at most three connections, then "+N". Subscriptions on `_INBOX.` subjects fold into one row per account, `_INBOX.>`, noted "reply inboxes". A push consumer's deliver subject is not a row of its own: the stream's row names the connection that reads it, "1 ephemeral (inventory-service)".
- **The JetStream API row.** Each account that uses JetStream has a `$JS.API.>` row, read by "server", with a note such as "JetStream API: 4.6 req/s, 0.1% errors". The rate is the increase of the account's API request counter over the 15 minutes before the range's end. Every server reports the account's total for the whole cluster, so the rate is one server's increase, the highest, not a sum, with that server's error share. nats-lens leaves out the first report of each server and account, and a server's reports in the first 60 seconds after it started, as a starting server reports 0 until it has learned the cluster's total. When no server reported twice, the note reads "JetStream API" alone. Taking the highest server is proposed, not yet confirmed.
- **Subscribers.** A stream's consumers: "6 durable consumers", "1 ephemeral", "2 durable, 1 ephemeral", "none"; a KV bucket's ephemeral consumers are "watchers". The count links to the Consumers list of that stream. A connection name links to the Connections list filtered by that name.
- **Stored msgs/s.** The increase of the stream leader's last sequence per second over the 15 minutes before the range's end, in the newest run of one leader and one creation time. With fewer than two such reports it is a dash, heard as "not observed". A row that is not a stream's has a dash heard as "no stored rate".
- **Notes.** The first that applies: "overlaps `<stream>`" (another stream of the account whose subjects overlap, wildcards included); "stream without a consumer" with what that means for its retention ("messages are dropped", "messages wait for a consumer", "messages are kept but nobody reads them"); "work queue, empty"; "interest retention: kept only while someone listens"; "mirror of `<origin>`"; "sourced into `<stream>`"; "mirrored by `<stream>`"; "receives every message in the account" for a subscription on `>`; "reply inboxes"; the JetStream API's rate. Stream names in a note link to their streams.
- **Warnings.** A row with an overlap, or a stream without a consumer, is shown in the warning colours and listed first. A stream counts as without a consumer only when no other stream mirrors or sources it, and never when it is a mirror, a KV bucket or an object store (proposed, not yet confirmed).
- **Order.** Rows with a warning first; then by account, the system account last and marked "system"; within an account, streams before the other rows, streams by stored rate (most first, unknown last) and then pattern, the other rows by connections (most first) and then pattern.
- **Filters with counts.** All patterns, Captured by a stream, Stream without a consumer, Overlapping streams and Wide subscriptions (`>`), a subscription row whose subject ends in `>` (the inbox row is not counted). Each pill counts every row of the scope whatever the filter; a range without a collection shows no counts. The filter is in the address as `?filter=stored|no-consumer|overlapping|wide`.
- **Account scope.** The page applies the header's account scope ([navigation.md](navigation.md)) to the rows, the counts and the tester. Without a scope, an account cell links to the page scoped to that account, keeping the filter.
- **At most 1,000 rows.** Beyond that the table says "The first 1,000 of 1,204 rows are listed."; the counts still count every row.
- **The servers' own subscriptions are left out.** Each server lists the subscriptions of its internal JetStream, system and account clients too. A subscription whose connection is not in its server's connection list of the same collection is the server's own, when that list came and was complete; it is left out and counted in the footnote. When the server's connection list was cut at `connz_limit` or did not come, such a subscription is counted as left out because its connection cannot be told from the server's own. The tester leaves both kinds out.
- **The footnote.** Under the table: rates come from streams; subscriptions come from each server's subscription list, refreshed every few minutes. Then, each on its own line: "Subscriptions as of 14:05:00 UTC.", the servers that did not answer for their subscription list, the lists cut at the limit ("Lists cut at the limit: nats-eu-3 listed 10,000 of 14,212."), and the subscriptions left out.
- **No subscription collection.** Without a subscription list within 24 hours before the collection, a notice says "Subscriptions not collected." The table then lists stream subjects and the JetStream API only, streams name no connections, and the footnote leaves out the sentence on subscription lists.
- **Where would a message go?** The subject field to the right of the filters asks 300 ms after the last keystroke and shows the answer only for the subject in the field now. The subject must be literal: a wildcard, an empty token, white space, a control character or more than 255 bytes is refused, and the field shows the server's reason, such as `subject "order.*" has a wildcard; give a literal subject`. A link can fill the field with `?subject=`, as the Micro services page does for an endpoint's subject; the tester asks for it at once, and typing in the field removes the parameter from the address.
- **The tester answers per account.** A message reaches only the account it is published in. With an account scope, the card answers for that account, also when nothing matches. Without one, it has a part for each account where something matches, headed "Published in APP", the system account last. The system account answers without a scope only for a subject that starts with `$SYS.` (proposed, not yet confirmed).
- **What a part lists.** Each stream that stores the subject, "via" the pattern that matched; its durable consumers whose filter matches (no filter matches all), by name, and its ephemeral ones counted ("22 watchers" on a KV bucket); its mirrors and the streams it is sourced into. "no consumer reads it" is a warning only on a stream that nothing copies. "the account's JetStream API answers it" for a `$JS.API.` subject in an account that uses JetStream. Core subscribers: a queue group counted once ("one of 3 in queue group order-service"), other subscriptions by connections, each "via" its subject; "none", or "not collected" without a subscription collection.
- **Nothing reached.** "no stream stores it and no subscriber listens"; without a subscription collection, the card says subscribers are not known. A range without a collection reads "No collection in this range." once, under the filters; the card is not shown.
- **Accessibility.** The card's body and the field's reason are polite live regions, so a screen reader hears each answer.
- **Phone width.** The pills wrap and the subject field takes a line of its own. The table scrolls inside its card with the pattern column kept in view, wide enough for about 18 characters: a shorter pattern stays whole, and a longer one breaks at any character, so it never covers the next cell. Above phone width the column is at least wide enough for about 28 characters, and a longer pattern breaks inside it rather than widening the table. A connection or queue group name, with its brackets, comma and "+N", stays on one line unless it is wider than its box.
- **API.** `GetSubjects` takes a system, a range, an account and a filter, and returns the rows, the counts, each server's subscription list outcome and the subscriptions left out. `MatchSubject` takes a system, a range, an account and a subject, and returns one group per account. With no collection in the range both answer with epoch 0 and nothing else.

## Scenarios

### Find a stream nobody reads

- **Surfaces:** `ui`
- **Given** `example-global`, whose limits stream `checkout-load-test` has no consumer and no stream mirrors or sources it
- **When** I choose "Stream without a consumer"
- **Then** the table lists `checkout-load-test` in the warning colours, with "none" as its subscribers and the note "stream without a consumer: messages are kept but nobody reads them"
- **And** the pill keeps its count, 2, and the other pills theirs

### Find where a message lands

- **Surfaces:** `ui`
- **Given** `example-global` with no account scope, where `order-events` stores `order.events.>` and `order-archive` mirrors it
- **When** I type `order.events.created` in the subject field
- **Then** the card shows "Published in APP" with the stream `order-events` via `order.events.>`, its four durable consumers, the mirror `order-archive`, and "Core subscribers none"
- **And** the system account `SYS`, whose `ops-monitor` connection listens on `>`, has no part, as the subject does not start with `$SYS.`

### Ask what a system subject reaches

- **Surfaces:** `ui`
- **Given** `example-global` with no account scope
- **When** I type `$SYS.SERVER.nats-eu-0.STATSZ`
- **Then** the card shows "Published in SYS", marked "system", with `natslens-example-global` via `$SYS.SERVER.>` and `ops-monitor` via `>` as core subscribers

### Ask about the JetStream API

- **Surfaces:** `ui`
- **Given** `example-global`, whose account `APP` uses JetStream
- **When** I type `$JS.API.INFO`
- **Then** the card's part for `APP` says "the account's JetStream API answers it"

### Type a wildcard

- **Surfaces:** `ui`
- **Given** the Subjects page
- **When** I type `order.*`
- **Then** no card is shown, and the field says `subject "order.*" has a wildcard; give a literal subject`

### See a queue group and reply inboxes

- **Surfaces:** `ui`
- **Given** `example-global`, where three `order-service` connections share the queue group `order-service` on `orders.place`
- **When** I open the Subjects page
- **Then** the row `orders.place` reads "3 connections in queue group order-service (order-service)", and `order-service` links to the Connections list filtered by that name
- **And** the clients' reply inboxes are one row, `_INBOX.>`, noted "reply inboxes"

### Read a cut or incomplete collection

- **Surfaces:** `ui`
- **Given** `example-global`, where `nats-eu-3`'s subscription list was cut at 10,000 of 14,212 and `nats-us-4` did not answer
- **When** I read the footnote
- **Then** it names the subscription collection's time, says `nats-us-4` did not answer for its subscription list, says "Lists cut at the limit: nats-eu-3 listed 10,000 of 14,212.", and counts the servers' own subscriptions left out

### A system without subscription lists

- **Surfaces:** `ui`
- **Given** `example-staging`, which collected no subscription list
- **When** I open the Subjects page
- **Then** a notice says "Subscriptions not collected.", the table lists its streams with their consumers and the `$JS.API.>` row, and no subscription or inbox row
- **And** typing `catalog.changes.x` shows the stream `catalog-changes` and "Core subscribers not collected"

### Ask from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `MatchSubject` with the account `APP` and a subject that nothing in `APP` matches
- **Then** it receives one group, for `APP`, with no stream, no subscriber and `jetstream_api` false

## Not supported / out of scope

- **No message rate per subject.** NATS servers do not report how many messages a subject carries; core publish and subscribe traffic is counted per connection only. Rates come from streams and from the JetStream API counters.
- **No delivered rate per subscription.** The servers count messages per subscription, and nats-lens stores that count, but the page does not show it yet.
- **No message sampling.** nats-lens does not subscribe to application subjects to count them, as that would read application data and add load.
- **No permission view.** Which users may publish or subscribe to a subject comes with [accounts-and-auth.md](accounts-and-auth.md), not here.
- **No service endpoints.** nats-lens does not collect `$SRV` replies; endpoints come with [micro-services.md](micro-services.md). A service's endpoints appear here as its connections' queue subscriptions.
- **No imports and exports in the tester.** A message published in one account reaching a stream or subscriber of another through an import is not shown; each account is answered on its own.
- **Sources by name only.** A stream's sources are matched by name within its own account, without their account or filter: a source from another account links to a same-named stream of the stream's own account.
- **Subscription lists can be cut.** A server lists at most `subsz_limit` subscriptions per collection; which ones a cut list keeps is up to the server, so a cut list can miss subscribers.
- **The JetStream API rate can read high.** For up to 15 minutes after an account first stores a message, every server takes in the others' totals at once, so the rate reads high. Routes that stay down across several collections, and clock skew beyond the rules above, can do the same.
