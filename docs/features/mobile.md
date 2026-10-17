# On call on the phone

- **Surfaces:** `ui`
- **Status:** planned
- **Related:** [triage-and-mutes.md](triage-and-mutes.md), [notifications.md](notifications.md), [navigation.md](navigation.md), [replay.md](replay.md)

## What it does

Planned; nothing here is built yet. The person on call usually opens nats-lens from an alert on a phone. A phone view will lead with the one finding that paged them, say in a sentence what is wrong right now, tell the story of what led to it, and let them acknowledge it with a note for the team. Today the UI works at phone width (tables scroll inside their own box), but its pages are laid out for a desktop. All scenarios below describe intended behaviour.

## Behavior and rules

- **The finding first.** A card with the severity, the system and time, a headline such as "4 consumers have no quorum", the context (for example "During a node upgrade"), and the affected assets with their replicas online.
- **Right now.** One sentence on the present state, such as which node pool is upgrading, how many servers are back, and whether messages are delivered.
- **What happened.** The steps that led here, in order, with times, and what may happen next if nothing is done.
- **The playbook.** A link to the playbook that applies, with its first warning, such as not restarting the owning service first.
- **Acknowledge.** "I'm on it" opens "Acknowledge": a note for the team and how long to quiet the alert (30 minutes, 2 hours, or until resolved). The acknowledgement is recorded as the IAP identity. The finding stays on the Overview, marked as acknowledged by that person.
- **Acknowledging is not muting.** An acknowledgement says who is working on a finding; a mute hides it ([triage-and-mutes.md](triage-and-mutes.md)).

## Scenarios

### Open a page from the alert

- **Given** a critical finding paged me
- **When** I open its link on my phone
- **Then** I see the finding card, the affected assets and one sentence on what is happening right now

### Tell the team I am on it

- **Given** the finding card is open
- **When** I choose "I'm on it", write "Upgrade in progress; watching nats-2" and quiet the alert for 2 hours
- **Then** the finding shows as acknowledged by me with my note, on the phone and on the Overview

## Not supported / out of scope

- **No native app.** The phone view is the web UI.
- **No push notifications from the page.** Alerts come from the alerting system or from [notifications.md](notifications.md).
- **No actions on NATS.** The phone view, like the rest of nats-lens, changes nothing in NATS.
