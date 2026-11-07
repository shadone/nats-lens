# On call on the phone

- **Surfaces:** `ui`, `api`
- **Status:** partial — phone triage and acknowledgements are built; consequence predictions and dedicated layouts for other pages are deferred
- **Related:** [triage-and-mutes.md](triage-and-mutes.md), [checks.md](checks.md), [replay.md](replay.md), [mobile plan](../plans/mobile.md)

## What it does

One read-only call returns a finding with the facts needed for an on-call card. It includes the current acknowledgement, affected assets, present observations, the episode's Replay story, and a configured playbook link. At 760 px and below, the finding page presents these facts as a phone card with an acknowledgement action.

## Behavior and rules

- **Fixed Replay link.** The phone story offers "Replay this window" for its supplied episode story interval, with explicit A/B and a fixed range. It retains the system and drops account scope because Replay reads the whole system. The adjacent text identifies surrounding observations; All details offers the selected-range transition context described in [Replay](replay.md).

- **One finding URL.** A finding link opens a phone card at 760 px and below. Above that width, the desktop sections retain their layout without page-wide sideways scrolling. Related settings keep each label beside its value, wrapping within their card. “All details” opens those sections below the phone card, with tables scrolling within their boxes.
- **Phone card.** The card shows severity, system, start, headline, context, affected assets, “Right now”, and “What happened”. The story links to Replay with the same start and end. “What to do” keeps the check's wording, with the opening sentence emphasized and subsequent sentences listed as steps. An HTTPS playbook opens from its own link. Missing observations say “not reported”; zero remains zero. Rates name reporting consumers and streams. Kubernetes windows describe observed events, without asserting that an upgrade is active. History-start and missing-history explanations link to Data history.
- **Acknowledging.** “I'm on it” stays at the foot of the phone screen while the card scrolls. Desktop offers “Acknowledge…” beside Mute. The dialog accepts an optional note of up to 500 characters and quiet for 30 minutes, 2 hours by default, or until resolved. It uses the same name and verified IAP identity rule as mutes. A saved acknowledgement appears before refetch finishes only while cached state matches the originating episode and firing state, without superseding ownership or an authoritative removal, and focus moves to its section. Failed saves keep the dialog open with an announced error.
- **Updating and handing back.** An active acknowledgement offers “Update…” and “Hand back…”. Updating replaces the previous acknowledgement. Handing back allows alerts to resume. The page shows who replaced or handed back the acknowledgement during that visit to the same system, finding, and episode. Resolving or changing the episode clears visit receipts. Quiet expiry leaves ownership in place. Resolve ends ownership for that episode.
- **Team marks.** Overview cause cards and Findings rows show the author, creation time, note, and quiet end while quiet holds. The Overview status line counts acknowledged findings. Acknowledging leaves the finding visible and counted at its severity.
- **Alert labels.** `/findings/by-labels` reads `system`, `code`, and `entity_key` from query parameters. It decodes spaces, literal plus signs, slashes, and colons, then replaces the route with the existing finding's URL. A missing finding opens Findings for the check with a message that it may have resolved and been swept. Both destinations retain the global range, system, and account query, while label-routing and unknown parameters are removed. Other API failures remain errors.
- **Overview order on phones.** The status line and cause cards precede health tiles. Tiles use two columns; the timeline follows them.

- **Current episode.** The card returns the finding's current state, mute, acknowledgement, and up to 100 transitions of its episode. An acknowledgement from an earlier episode does not apply.
- **Affected assets.** The headline count covers firing, unmuted findings of the same check and entity type. Up to 10 assets appear, worst first and then by entity key, with the total before the cut. A resolved or muted finding without a group keeps its own asset. Replica counts, online counts, leaders, and server answering states appear only when known.
- **Present observations.** The card uses a collection within 3 collect intervals of the present. Otherwise its present values are unset. Rates require a leader counter report in that collection and at least 2 samples in its latest leader, creation, and counter run. Delivery sums include reporting consumers of affected streams, including siblings of a missing consumer. A sum covers only those reports; it does not establish delivery for every consumer. A missing observation stays unset; an observed zero stays zero.
- **Episode story.** The story begins 15 minutes before the condition started, or before first sighting when that start is unknown. It spans at most 24 hours and ends at resolve for a resolved finding, otherwise at the present. Up to 12 Replay steps appear in time order; the cut keeps the worst steps and those nearest the condition's start. The response includes the total before the cut. If the story starts before retention or stored history, the card returns an explanation alongside the finding and present observations.
- **Context.** The card chooses the newest Kubernetes window overlapping the span from 15 minutes before the condition started to the present. A window whose last event falls within the 15-minute quiet gap appears among the present observations. Its end remains the last observed event; the window does not prove that an upgrade is still active.
- **Playbook.** A remediation entry can provide an external HTTPS playbook link alongside its text. Invalid links stop startup. The card returns the remediation text and link separately; it does not fetch the playbook.
- **Read-only.** Reading a card changes neither NATS nor the finding, mute, or acknowledgement. It uses the API's 30-second query deadline.

## Scenarios

### Read across the phone breakpoint

- **Surfaces:** `ui`
- **Given** a finding with related settings opens at 760 or 761 px
- **When** the person reads the card or desktop details
- **Then** labels and values wrap within their card without sideways page scrolling
- **And** wide evidence tables scroll inside their own boxes

### Acknowledge from a phone

- **Surfaces:** `ui`
- **Given** a firing finding opens at phone width
- **When** the person selects “I'm on it” and saves an acknowledgement
- **Then** the page shows the saved author and note without a reload and focuses the acknowledgement
- **And** the Overview and Findings retain the finding with its acknowledgement mark

### Update or hand back

- **Surfaces:** `ui`
- **Given** the episode has an acknowledgement
- **When** the person updates it or hands it back
- **Then** the page shows who replaced or handed it back during the visit
- **And** an expired quiet end does not itself remove ownership
- **And** the receipt clears when the episode resolves or the visit changes system, finding, or episode

### Follow a link built from alert labels

- **Surfaces:** `ui`
- **Given** an alert supplies the system, check code, and entity key as query parameters
- **When** the person opens the label link
- **Then** the existing finding opens on its canonical URL
- **And** a missing finding opens its check's Findings list with an explanation
- **And** both destinations retain the global time range, system, and account

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

- The acknowledgement section keeps only a replacement or hand-back receipt from the current visit. The API has no acknowledgement-history list. Reloading reads the current episode's acknowledgement; it does not reconstruct those receipts. Stored acknowledgement events can be read in Events within retention; the seeded demo writes none.
- Dedicated phone layouts for Servers, Streams, and Replay are deferred. Their existing layouts remain.
- No acknowledged-only Findings filter or acknowledgement of several findings at once is available.
- No prediction of what follows a finding is available.
- No native app, push notification, or action on NATS is part of the card.
