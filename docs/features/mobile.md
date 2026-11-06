# On call on the phone

- **Surfaces:** `api`
- **Status:** partial — the on-call card API is built; the phone layout and acknowledge dialog are planned
- **Related:** [triage-and-mutes.md](triage-and-mutes.md), [checks.md](checks.md), [replay.md](replay.md), [mobile plan](../plans/mobile.md)

## What it does

One read-only call returns a finding with the facts needed for an on-call card. It includes the current acknowledgement, affected assets, present observations, the episode's Replay story, and a configured playbook link. The phone layout is planned.

## Behavior and rules

- **Current episode.** The card returns the finding's current state, mute, acknowledgement, and up to 100 transitions of its episode. An acknowledgement from an earlier episode does not apply.
- **Affected assets.** The headline count covers firing, unmuted findings of the same check and entity type. Up to 10 assets appear, worst first and then by entity key, with the total before the cut. A resolved or muted finding without a group keeps its own asset. Replica counts, online counts, leaders, and server answering states appear only when known.
- **Present observations.** The card uses a collection within 3 collect intervals of the present. Otherwise its present values are unset. Rates require a leader counter report in that collection and at least 2 samples in its latest leader, creation, and counter run. Delivery sums include reporting consumers of affected streams, including siblings of a missing consumer. A sum covers only those reports; it does not establish delivery for every consumer. A missing observation stays unset; an observed zero stays zero.
- **Episode story.** The story begins 15 minutes before the condition started, or before first sighting when that start is unknown. It spans at most 24 hours and ends at resolve for a resolved finding, otherwise at the present. Up to 12 Replay steps appear in time order; the cut keeps the worst steps and those nearest the condition's start. The response includes the total before the cut. If the story starts before retention or stored history, the card returns an explanation alongside the finding and present observations.
- **Context.** The card chooses the newest Kubernetes window overlapping the span from 15 minutes before the condition started to the present. A window whose last event falls within the 15-minute quiet gap appears among the present observations. Its end remains the last observed event; the window does not prove that an upgrade is still active.
- **Playbook.** A remediation entry can provide an external HTTPS playbook link alongside its text. Invalid links stop startup. The card returns the remediation text and link separately; it does not fetch the playbook.
- **Read-only.** Reading a card changes neither NATS nor the finding, mute, or acknowledgement. It uses the API's 30-second query deadline.

## Scenarios

### Read the current episode

- **Given** a finding whose current episode has an acknowledgement
- **When** a client requests its on-call card
- **Then** the card includes that acknowledgement and the episode's transitions
- **And** an acknowledgement of an earlier episode stays out

### Read a resolved finding

- **Given** a finding has resolved
- **When** a client requests its on-call card
- **Then** the story ends at resolve while present observations describe the present

### Read a system without recent collections

- **Given** the collector stopped more than 3 collect intervals ago
- **When** a client requests an on-call card
- **Then** present numbers are unset rather than zero
- **And** the finding remains available

### Read retained samples after reports stop

- **Given** earlier collections contain delivery and stream counters, but the selected collection has no leader counter report for those entities
- **When** a client requests the card
- **Then** those entities contribute no present rate
- **And** a reporting sibling can still contribute its measured rate without implying complete reporting

### Read a story beyond stored history

- **Given** the episode's story starts before retention or stored history
- **When** a client requests the card
- **Then** its steps are empty with an explanation, and its finding and present observations remain available

## Not supported / out of scope

- The phone card layout, acknowledge dialog, and phone-specific navigation are planned.
- No prediction of what follows a finding is available.
- No native app, push notification, or action on NATS is part of the card.
