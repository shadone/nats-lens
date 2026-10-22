# Micro services

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [subjects.md](subjects.md), [connections.md](connections.md), [monitoring-snapshots.md](monitoring-snapshots.md), [event-capture.md](event-capture.md), [running-nats-lens.md](running-nats-lens.md), [navigation.md](navigation.md), [applications.md](applications.md), [design.md](../design.md), [micro services plan](../plans/micro-services.md)

## What it does

The Micro services page lists the services built on the NATS micro framework in a system, one card each, and shows the chosen service's endpoints and instances. nats-lens knows a service at one of two levels. In every account it sees the services in the servers' subscription lists, as each instance subscribes to the framework's `$SRV` subjects: the service's name, its instances and their connections. In each account listed under `accounts`, nats-lens also asks the services with `$SRV.INFO` and `$SRV.STATS`, and the page shows versions, descriptions, endpoints, request rates, average times, errors and the last error. nats-lens sends only those two requests and never calls an endpoint.

## Behavior and rules

- **One collection.** The page reads the newest collection at or before the end of the range, with the newest subscription collection within the 24 hours before it, as the Subjects page does. The line under the title names the collection's time. A past range works as a live one.
- **Two levels.** A service of an account listed under `accounts` is asked: its card shows its versions, description, requests per second and errors. A service of any other account is seen in subscriptions only: its card reads "seen in subscriptions only", its rates are dashes heard as "not asked", and its note says "list the account under `accounts` for versions, endpoints and timings". Its section says how many instances the subscription list shows, on which connections, and how to see more.
- **Grants.** To be asked, the account's user under `accounts` needs to publish on `$SRV.INFO` and `$SRV.STATS` and subscribe on `_INBOX.>` (see [running-nats-lens.md](running-nats-lens.md)). `micro: false` on an entry turns the asking off for that account.
- **One service per name and account.** Instances are counted by instance ID. For an asked account, the instances are those that answered the collection's `$SRV.STATS`; for an account seen only, those with `$SRV` subscriptions in the subscription list.
- **Cards.** Each card shows the name, the version ("1.4.2", or "2.0.0 / 1.9.3" when the instances differ), the description, the instances, requests per second, the error share and one note. A note that warns is in the warning colours and gives the card a warning edge; errors above 1% are in the critical colour. Three cards a row on a wide screen, two on a narrower one, one on a phone. Without an account scope and with services in more than one account, each card names its account.
- **Notes, the first that applies.** "instance missing: `<id>` since 10:05" (or "2 instances missing since 09:50"); "mixed versions: 3 on 2.0.0, 1 on 1.9.3"; "orders.get slower than usual since 08:12"; "errors 2.4% on orders.create"; "no answer to $SRV.STATS:" with the reason; "list the account under `accounts` …" for a service seen only; else "healthy".
- **Order.** Services with a warning note first (instance missing, mixed versions, slow endpoint, errors, no answer), the system account's services last, then by requests per second (most first, unknown last), then by name and account.
- **Choosing a service.** A card is a button; a click, Enter or Space chooses it, and the address keeps it as `?service=<account>/<name>`. A bare `?service=<name>` names the service of that name in the scoped account, or the first of that name without a scope. Without one, the first card is chosen. An address that names a service the collection does not hold says so and shows the first card.
- **Endpoints.** The chosen service's table lists each endpoint with its subject, queue group, requests per second, average time, error share and last error. Slow endpoints come first, then those with errors above 1% (highest first), then by name. A slow endpoint's row is in the warning colours and reads "avg time 10× its 7-day level since 08:12". A literal subject links to the Subjects page's tester in the service's account; a subject with a wildcard is plain text. Endpoints of one name on several subjects are summed into one row, which lists every subject, such as "orders.create, orders.create.v2".
- **Rates.** Requests per second, the error share and the average time cover the 15 minutes before the range's end. For each instance, nats-lens takes its newest run (one instance ID and start time, with counters that never fall) and its increase over that run's reports in the 15 minutes, then sums the instances. An instance that restarted counts its new run only, and an instance with one report so far adds nothing yet. The average time is the increase of processing time over the increase of requests. A value nats-lens could not work out is a dash, never 0.
- **The 7-day level and the slow rule.** The level is the median of the endpoint's hourly average times over every instance in the 7 days before the 15 minutes, from hourly sums nats-lens keeps as it writes each collection. With fewer than 24 such hours there is no level and nothing is slow. An endpoint is slow when its average time is at least 3 times its level and at least 5 ms above it. The slow spell starts after the newest hour before the current one that was not slow: at the first collection after the last one that was not slow in that hour and the next hour with requests, or, when that stretch ends with a fast collection, right after it; when every hour of the 7 days is slow, at the first of them. A fast collection seen later never moves the start back. The rule and its thresholds are proposed, not yet confirmed.
- **Missing instances.** An instance is missing while it is gone from the collection and its `$SRV` subscriptions are still in the subscription list (registered but not answering), for as long as the list shows it; or when it went within the hour before and the service has fewer instances than when it was last seen, listing at most that shortfall, the most recently gone first. In an asked account an instance must be absent from two answered requests. A rolling restart, where a new instance takes an old one's place, is not missing. Nothing is judged missing in a collection whose `$SRV.STATS` request did not succeed. The instances table lists a missing instance in the warning colours: "missing since 10:05, last answered 10:04; still subscribed, so registered but not answering".
- **Mixed versions.** When the instances report more than one version, the card counts them, most instances first.
- **Last error.** `$SRV.STATS` gives a last error no time, so the endpoint shows the first by instance ID among the instances that report one, with that instance.
- **Instances.** The section lists each instance with its version, start, connection (a link to the Connections page filtered by name and server) and server. The connection is the one that holds its `$SRV` subscriptions in the subscription list. The heading says where the instances come from: "Instances" when they answered the collection's `$SRV.STATS`, or when the service is listed only for its missing instances; "Instances seen in the subscription list" when none answered (an account not asked, or an asked one that did not answer); "Instances that last answered within the hour before this collection" when the account's request failed and no subscription list holds the service.
- **No answer.** When an asked account's `$SRV.STATS` request was refused, failed or timed out, its services' cards read "no answer to $SRV.STATS:" with the reason, with dashes for requests and errors, and the section lists the instances the subscription list holds, else those that last answered within the hour. An asked account with no service listed gets a notice above the cards with the server's text and the grants, such as "SUPPLIERS asked, no answer. The server refused $SRV.STATS …". An asked account where no service answered (no responders) is named in the footnote.
- **Labels and accounts.** Each entry under `accounts` has a label. nats-lens asks the servers which account the entry's user is in, so a service is listed under the account the subscription lists name. When that lookup is refused, nats-lens keeps the label, and the page still lists a service once when all its answering instances are seen under one other account. Two entries for one account ask it once.
- **Without a subscription list.** A notice says subscriptions were not collected: only the services of asked accounts are shown, and instances name no connection.
- **Caps.** At most 200 services, 100 endpoints and 100 instances per service; a cut list says "The first N of M … are listed."
- **The footnote.** It says how often nats-lens asks, the 15-minute window and the slow rule, the subscription list's time, and the accounts with no responders.

## Scenarios

### See a slow endpoint

- **Surfaces:** `ui`
- **Given** `example-global` in demo mode, whose `orders` endpoint `get` on `orders.get.*` has taken ten times its 7-day level since 08:12
- **When** I open Micro services
- **Then** the `orders` card is first, with "orders.get slower than usual since 08:12" in the warning colours
- **And** its endpoint table shows `get` first, in the warning colours, with "41 ms" and "avg time 10× its 7-day level since 08:12"

### See mixed versions

- **Surfaces:** `ui`
- **Given** `example-global`, where three `pricing` instances run 2.0.0 and one runs 1.9.3
- **When** I open Micro services
- **Then** the `pricing` card shows "2.0.0 / 1.9.3" and the note "mixed versions: 3 on 2.0.0, 1 on 1.9.3"

### An instance stops answering

- **Surfaces:** `ui`
- **Given** `example-global`, where one `inventory` instance stopped answering at 10:05 while it stayed subscribed
- **When** I choose a range that ends at 10:30 and the `inventory` card
- **Then** the card reads "instance missing: `<id>` since 10:05", and the instances table lists that instance in the warning colours, "still subscribed, so registered but not answering"

### An account refuses the requests

- **Surfaces:** `ui`
- **Given** `example-global`, whose account `SUPPLIERS` is listed under `accounts` and whose user may not publish on `$SRV.STATS`
- **When** I open Micro services scoped to `SUPPLIERS`
- **Then** a notice reads "SUPPLIERS asked, no answer." with the server's text and the grants the user needs
- **And** the page says "No micro service in SUPPLIERS in this collection."

### A request times out

- **Surfaces:** `ui`
- **Given** `example-global`, where the `$SRV` requests to `APP` timed out at 11:29:56 while the instances stayed subscribed
- **When** I choose a range that ends at that collection
- **Then** the cards `inventory`, `orders` and `pricing` read "no answer to $SRV.STATS: no reply within the collect window", with their instances and dashes for requests and errors
- **And** the chosen service's section says no endpoints are known and lists "Instances seen in the subscription list"

### Services seen in subscriptions only

- **Surfaces:** `ui`
- **Given** `example-prod`, whose account `APP` is not listed under `accounts`
- **When** I open Micro services
- **Then** the cards `catalog` and `ledger` read "seen in subscriptions only", with their instances and dashes for requests and errors
- **And** the chosen service's section says nats-lens does not ask `APP` and how to list it, then lists its instances with their connections

### No service at all

- **Surfaces:** `ui`
- **Given** `example-staging`, which asks no account and has no subscription list
- **When** I open Micro services
- **Then** the page says "No micro service known: no account is listed under `accounts`, and no subscription list within 24 hours before this collection."

### Open an endpoint's subject in the tester

- **Surfaces:** `ui`
- **Given** the `orders` endpoints in `example-global`
- **When** I follow the subject `orders.create`
- **Then** the Subjects page opens scoped to `APP`, with `orders.create` in the tester's field and what it reaches

### List services from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `GetMicroServices` with a range and an account
- **Then** it receives the services of the range's newest collection with their notes, instances, missing instances and endpoints, the asked accounts with how their requests ended, and the subscription list's time

## Not supported / out of scope

- **Only services built on the micro framework.** Request-reply services that do not answer `$SRV` requests are not found; their subjects appear on the Subjects page as queue subscriptions.
- **No calls to the endpoints.** nats-lens sends only `$SRV.INFO` and `$SRV.STATS`, never a request to an endpoint and never `$SRV.PING`.
- **Custom stats and metadata are stored, not shown.** The services' custom stats (`data` in STATS replies, at most 1 KB each) and endpoint metadata are in the database but not on the page.
- **Accounts not listed are not asked.** Their services are known from subscriptions only, without versions, endpoints or timings.
- **No history per service.** The page shows one collection; a chart of a service's rates over time is not built.
- **A wildcard subject has no tester link.** The Subjects page's tester takes literal subjects only.
- **Known limits.** A new instance whose first STATS reply comes a collection after its start loses its activity before that reply in the hourly sums. When the entry asking for an account loses its connection during the requests, the account has no answers in that collection. In operator mode an entry whose account lookup is refused cannot be matched with another entry of the same account, and both are stored. With event capture on, an account connection that cannot connect stops the system's start until it connects.
