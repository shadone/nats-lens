# Triage and mutes

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [overview-page.md](overview-page.md), [events.md](events.md), [metrics-and-alerting.md](metrics-and-alerting.md), [mobile.md](mobile.md)

## What it does

The Findings page lists the findings of the time range, filtered by severity, state and check, and below them the mutes with who set them, why and until when. A finding's own page explains it: what fails in the check's own words, since when the condition holds and since when nats-lens has seen it, how many other entities share it, each server's copy of a stream or consumer, what to do, the related settings with their values, what happened around its start, every check of the same entity on a status timeline, and the finding's transitions. From the Overview's worst finding or a finding's page, an operator can mute a finding with a reason and an end; from a finding's page or the Findings page's mutes list, an operator can lift a mute early.

## Behavior and rules

- **What a finding is.** The page says it: "A finding is one failing check for one entity."
- **Order.** Unmuted findings first, then by severity, then the ones firing longest. 50 per page.
- **Filters.** Severity (all, critical, warning, info); state: Firing (the default), Flapping, Resolved, or All; "Include muted" (on by default); and a check code. With the default filters and nothing firing, the page says "All clear: no finding fires in this range."
- **A row.** Severity (or muted), check code and name, a FLAPPING, CHANGED or RESOLVED tag where it applies, the entity, the detail, and since when it fires (or when it resolved). On a muted row, the time column shows "until <date> <time>" and "in <duration>", and a line reads "Muted by <name>:" followed by the reason and "Expires <time> UTC."
- **Past ranges.** For a range that ends in the past, the findings are shown as they stood at its end, with the mutes in force then.
- **The finding page.** Its headline is the finding's detail as the check wrote it (the check name when there is none), under the severity (or muted), code, check name and state, with the entity. One line reads "Condition since <time> · first seen by nats-lens <time>" where the check knows the condition's start, else "First seen by nats-lens <time>"; it adds "already true when nats-lens started" when the condition began before nats-lens's history, or the finding was first seen within a minute after the history starts; and "N other entities have this finding", linking to the Findings page for the check, when other entities have it (muted ones included, resolved ones not). Then how long it fired, when it resolved, its transitions in the last hour, its value with the value's name, and "Cause in window", a link to the event the check names as its cause. Below it: the mute, if any; for a stream or consumer finding, "Server copies"; the remediation (with any text from the remediation file); the related settings, with where they were read from and the metric basis; "Around the start"; "Related" links; a status timeline of every check of the same entity; and the transitions, newest first, at most 100.
- **Server copies.** For a stream or consumer finding, a table of each server's copy: role (leader, follower, behind or offline), creation time, positions (messages and last sequence for a stream, delivered and ack floor for a consumer), lag, and whether the configuration is the same as most copies'. A copy created more than an hour from the time most copies share is marked with the difference (for example "+5d"), and so is a position that differs from the leader's or a configuration that differs. A value a server did not report shows as "–". Other findings, such as a client's, have no table.
- **Around the start.** The events worth reading, folded, in the 10 minutes either side of the condition's start, or of the first sighting without one, at most 20. When those minutes lie before nats-lens's history, the page says it has no history from then; when they begin before it, the page says where the history begins.
- **Related.** Links to the entity, its stream (for a consumer), its server, and the Events page filtered to it.
- **Mute a finding.** "Mute…" opens a dialog. It applies to this entity, or to every entity of the check. It needs a reason (at most 1000 characters) and an end: in 1 hour, 8 hours, 24 hours (the default), 7 days or 30 days, or at a time in UTC. The end must be between one minute and 90 days ahead.
- **Who muted it.** With `iap_audience` set, a mute or a lift needs a valid IAP assertion, nats-lens records the verified IAP user, and the dialog says "The mute is recorded as your IAP identity." Without it, nats-lens records the name the request gives; the dialog asks for "Your name" and the browser remembers it for next time.
- **Lift a mute.** "Lift mute…" on a finding, or "Lift…" in the mutes list, ends a mute early and records who lifted it.
- **The mutes list.** Mutes in force by default; "Include ended" adds lifted and expired ones. Each shows Muted, Lifted or Expired, the check, the entity or "every entity", when it ends or ended, and "Muted by … on …: reason", with who lifted it.
- **Mutes apply when findings are read.** A mute does not stop the check. When it ends, the finding counts again if the condition still holds.

## Scenarios

### Look at everything that flaps

- **Given** a finding that resolved and fired again five times in the last hour
- **When** I choose the Flapping state
- **Then** the finding is listed with the FLAPPING tag

### Mute every finding of one check

- **Surfaces:** `ui`
- **Given** `server-005` "Version differs from the majority" fires during a planned upgrade
- **When** I choose "Mute…", "Every entity of server-005", the reason "rolling upgrade this week", and "in 7 days"
- **Then** every `server-005` finding is shown as muted with my reason, and none of them counts towards the Overview's status

### A mute without a reason is refused

- **Surfaces:** `ui`
- **Given** the mute dialog is open
- **When** I leave the reason empty and press Mute
- **Then** the dialog says "Give a reason; it is shown with the muted finding." and nothing is muted

### Lift a mute early

- **Surfaces:** `ui`, `api`
- **Given** a mute in force on `stream-005` for `order-archive`
- **When** I choose "Lift…" in the mutes list, or call `UnmuteFinding` with the mute ID
- **Then** the mute ends now, records who lifted it, and moves to the ended mutes

### Mute through the API

- **Surfaces:** `api`
- **Given** `natslens run` without `iap_audience`, on a loopback address
- **When** I call `MuteFinding` with a check code, an entity, a reason, an end and my name
- **Then** nats-lens stores the mute with my name, and `ListMutes` returns it
- **And** behind IAP, the same call without a valid IAP assertion is refused as unauthenticated

### See which server's copy is odd

- **Surfaces:** `ui`
- **Given** `consumer-012` fires for `resize-worker` on `order-jobs`, whose copy on `nats-0` was created two months before the copies on `nats-1` and `nats-2`
- **When** I open the finding
- **Then** "Server copies" lists the three servers, marks the `nats-0` row with "−65d", and shows the delivered and ack floor positions of each

### A condition older than the history

- **Surfaces:** `ui`
- **Given** nats-lens started at 08:19 and a finding's condition began four days earlier
- **When** I open the finding
- **Then** it reads "Condition since <four days ago> · first seen by nats-lens <08:19>, already true when nats-lens started", and "Around the start" says nats-lens has no history from then

## Planned

- **Why this severity.** A sentence that says why the finding is info rather than a warning, and what would raise it.
- **What to do, ready to copy.** The `nats` CLI command that checks the finding by hand, with a copy button, and a link to the playbook that applies.
- **Placement link.** A link to the placement of the finding's stream ([placement.md](placement.md)).
- **Accept as known.** Accepting with a reason and a review date, for this entity, for the same finding on related entities, or for the check everywhere ([checks.md](checks.md)).

## Not supported / out of scope

- **No mute without an end.** Every mute ends within 90 days.
- **No editing a mute.** A mute's reason or end cannot be changed; lift it and mute again.
- **No mute of several checks at once.** A mute covers one check.
- **No assignment or acknowledgement.** Findings have no owner or "I'm on it" state yet ([mobile.md](mobile.md)).
- **No sign-in of its own.** Without IAP, the name in a mute is whatever the person types.
