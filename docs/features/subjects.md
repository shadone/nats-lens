# Subjects

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [streams.md](streams.md), [connections.md](connections.md), [micro-services.md](micro-services.md), [stream-checks.md](stream-checks.md)

## What it does

Planned; nothing here is built yet. The Subjects page will show where messages can go, by subject pattern: which streams capture a pattern, which consumers read it, which core subscriptions listen to it, and which service endpoints answer it. It is built only on data the NATS servers expose. The servers do not count messages per subject, so the page shows rates only where a stream stores the subject. All scenarios below describe intended behaviour.

## Behavior and rules

- **Patterns, not traffic.** One row per subject pattern: its account, the stream that stores it, its subscribers, the stored message rate, and a note.
- **Filters with counts.** All patterns, captured by a stream, stream without a consumer, overlapping streams, and wide subscriptions (`>`).
- **Where would a message go?** Typing a subject shows which stream would store it, through which pattern, which consumers would then read it, which mirrors copy it, and which core subscribers would receive it.
- **Rates only where a stream stores the subject.** A stored rate comes from the growth of the stream's last sequence. A subject that no stream stores has no rate.
- **Subscriptions refreshed less often.** Each server's subscription list is read every few minutes rather than every collection, to keep large systems cheap. nats-lens collects the lists with SUBSZ every `subscriptions_interval` (5 minutes by default) and stores them ([monitoring-snapshots.md](monitoring-snapshots.md)); stream subjects and consumer filters it stores with every collection.

## Scenarios

### Find where a message lands

- **Surfaces:** `ui`
- **Given** the stream `order-jobs` captures `orders.*.jobs.*`
- **When** I type `orders.eu.jobs.resize`
- **Then** the page shows `order-jobs` with the pattern that matched, its consumers, and any core subscribers

### Find a stream nobody reads

- **Surfaces:** `ui`
- **Given** a work-queue stream with no consumer
- **When** I choose "Stream without a consumer"
- **Then** that stream's patterns are listed

### List patterns from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client asks for the subject patterns of an account
- **Then** it receives each pattern with its stream and subscriber count

## Not supported / out of scope

- **No message rate per subject.** NATS servers do not report how many messages a subject carries; core publish and subscribe traffic is counted per connection only.
- **No message sampling.** nats-lens does not subscribe to application subjects to count them, as that would read application data and add load.
- **No permission view.** Which users may publish or subscribe to a subject comes with [accounts-and-auth.md](accounts-and-auth.md), not here.
