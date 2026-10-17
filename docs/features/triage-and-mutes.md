# Triage and mutes

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [checks.md](checks.md), [overview-page.md](overview-page.md), [events.md](events.md), [metrics-and-alerting.md](metrics-and-alerting.md), [mobile.md](mobile.md)

## What it does

The Findings page lists the findings of the time range, filtered by severity, state and check, and below them the mutes with who set them, why and until when. A finding's own page explains it: what fails, since when, what to do, the related settings with their values, every check of the same entity on a status timeline, and the finding's transitions. From the Overview's worst finding or a finding's page, an operator can mute a finding with a reason and an end; from a finding's page or the Findings page's mutes list, an operator can lift a mute early.

## Behavior and rules

- **What a finding is.** The page says it: "A finding is one failing check for one entity."
- **Order.** Unmuted findings first, then by severity, then the ones firing longest. 50 per page.
- **Filters.** Severity (all, critical, warning, info); state: Firing (the default), Flapping, Resolved, or All; "Include muted" (on by default); and a check code. With the default filters and nothing firing, the page says "All clear: no finding fires in this range."
- **A row.** Severity (or muted), check code and name, a FLAPPING, CHANGED or RESOLVED tag where it applies, the entity, the detail, and since when it fires (or when it resolved). On a muted row, the time column shows "until <date> <time>" and "in <duration>", and a line reads "Muted by <name>:" followed by the reason and "Expires <time> UTC."
- **Past ranges.** For a range that ends in the past, the findings are shown as they stood at its end, with the mutes in force then.
- **The finding page.** Its header shows the severity (or muted), code, state, name, entity and detail; "First seen by nats-lens" with how long; "Condition since" where the check knows it; when it resolved; its transitions in the last hour; its value with the value's name; and "Cause in window", a link to the event the check names as its cause. Below it: the mute, if any; the remediation (with any text from the remediation file); the related settings, with where they were read from and the metric basis; a status timeline of every check of the same entity; and the transitions, newest first, at most 100.
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

## Planned

- **A finding page that explains itself.** A headline in plain words (for example "order-processed was rebuilt on nats-1 five days after the others"), the condition start and the first sighting side by side with "(when it started)" when they differ, and how many other entities have the same finding.
- **Each server's copy.** For agreement findings, a table of each server's role, creation time, positions and configuration, with the odd value marked.
- **Why this severity.** A sentence that says why the finding is info rather than a warning, and what would raise it.
- **What to do, ready to copy.** The `nats` CLI command that checks the finding by hand, with a copy button, and a link to the playbook that applies.
- **Around the start.** What happened in the 10 minutes around the condition's start: restarts, leader changes and Kubernetes events. When nats-lens has no history from then, it says so.
- **Related.** Links to the entity, its stream, its server, its placement and its events.
- **Accept as known.** Accepting with a reason and a review date, for this entity, for the same finding on related entities, or for the check everywhere ([checks.md](checks.md)).

## Not supported / out of scope

- **No mute without an end.** Every mute ends within 90 days.
- **No editing a mute.** A mute's reason or end cannot be changed; lift it and mute again.
- **No mute of several checks at once.** A mute covers one check.
- **No assignment or acknowledgement.** Findings have no owner or "I'm on it" state yet ([mobile.md](mobile.md)).
- **No sign-in of its own.** Without IAP, the name in a mute is whatever the person types.
