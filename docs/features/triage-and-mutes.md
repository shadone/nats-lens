# Triage and mutes

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [data-history.md](data-history.md), [checks.md](checks.md), [overview-page.md](overview-page.md), [events.md](events.md), [metrics-and-alerting.md](metrics-and-alerting.md), [mobile.md](mobile.md)

## What it does

The Findings page lists the findings of the time range, filtered by severity, state, check and the account scope, and below them the mutes with who set them, why and until when. A finding's own page explains it: what fails in the check's own words, since when the condition holds and since when nats-lens has seen it, how many other entities share it, each server's copy of a stream or consumer, what to do, the related settings with their values, what happened around its start, every check of the same entity on a status timeline, and the finding's transitions. From a cause card on the Overview or a finding's page, an operator can mute a finding with a reason and an end; from a finding's page or the Findings page's mutes list, an operator can lift a mute early. A finding of an acceptable check can also be accepted as by design, with a reason and a review date: from its own page, its row on the Findings page, its cause card on the Overview, or, for `stream-005`, the Capacity page ([capacity.md](capacity.md), [overview-page.md](overview-page.md)); the same action can accept the check on related entities. An accepted finding reads as accepted wherever it shows, and "Accepted findings" lists the acceptances with the soonest review first. The finding page also gives the `nats` command that checks the finding by hand, and links a stream or consumer to Placement.

## Behavior and rules

- **Account scope.** The Findings page offers the header's account scope ([navigation.md](navigation.md)). Scoped, it lists the findings of that account, its streams and its consumers, and those of servers, clients, connections and the system, which concern every account; a line under the list counts the rest ("3 findings of other accounts are not shown; findings of servers, clients, connections and the system are, as they concern every account.") with "Show all accounts". A finding's own page leaves the scope behind. Proposed, not yet confirmed.

- **Scoped refresh after an operator write.** Mute, unmute and acknowledgement saves refresh operator-dependent views for the system captured when the action opened, plus the shared fleet summaries. Known raw series, unrelated systems and immutable captured-event selections do not refetch for a mute. Acknowledgement creation additionally refreshes event feeds and event-history coverage because it writes a captured event. Stopped-collector server status and Replay status strips remain dependencies. Unknown same-system view families refresh conservatively. Mute/unmute dialogs retain their existing wait for affected active refreshes; acceptance saves keep their explicit nonwaiting refresh behavior, and all writes remain pending until the write itself completes. Frozen list traversals retain their existing snapshot contract; Refresh list starts a new capture.

- **Finding prompt.** Desktop finding details and the phone on-call card offer a labeled **Copy as prompt** action for firing, resolved, and muted findings. The prompt includes the loaded finding and check IDs, its typed entity, selected UTC window, and a bounded observed summary. It leaves acknowledgement and mute state untouched. Context, untrusted evidence, and clipboard fallback follow [agent investigations](agent-investigations.md).

- **Replay finding context.** Desktop details offer "Replay this window" around the latest recorded transition, first sighting or resolution in the selected range, with explicit fixed times. This is surrounding observation context, not a claimed episode duration. Missing timing has an explanation instead of a link; the newest 100 transitions can omit older episodes. [Replay](replay.md) gives the context, historical-range, retention and missing-evidence rules.

- **Acknowledgements.** The [phone card and acknowledgement dialog](mobile.md) record who works on a finding. Acknowledging keeps the finding visible and counted; a mute hides it.

- **History limits on findings.** Unresolved Findings rows and Overview cause cards say “already true when nats-lens started” under the finding page’s history-start rule. Finding details link the history-start wording and missing-history explanations around the start to Data history with the global query. This wording does not establish when the condition actually began.

- **What a finding is.** The page says it: "A finding is one failing check for one entity."
- **Order.** Unmuted findings first, then by severity; among warnings, expiry warnings (`account-003`, `account-004`, `server-016`) after the others, so a credential that expires soon never heads an outage, while a critical expiry ranks as any critical finding; then the ones firing longest. 50 per page.
- **Filters.** Severity (All, Critical, Warning, Info); state: Firing (the default), Flapping, Resolved, or All; "Include muted" (on by default); and a check code. With the default filters and nothing firing, the page says "All clear: no finding fires in this range."
- **Text filter in the API.** With `text`, `ListFindings` keeps the findings whose check code, check name or entity (key, name, account, stream, consumer or server) contains it, ignoring case. The Findings page has no text box; the command bar uses it ([command-bar.md](command-bar.md)).
- **A row.** Severity (or muted), check code and name, a Flapping, Changed or Resolved tag where it applies, the entity, the detail, and since when it fires (or when it resolved). On a muted row, the time column shows "until <date> <time>" and "in <duration>", and a line reads "Muted by <name>:" followed by the reason and "Expires <time> UTC." An accepted row is marked Accepted instead of Muted, its time column shows "review on <date>" and "in <duration>", and its line reads "Accepted by <name>: <reason>. Review on <date>."
- **Past ranges.** For a range that ends in the past, the findings are shown as they stood at its end, with the mutes in force then.
- **Entities by name.** A row, the finding page, the mute dialog and the mutes list name an account by its readable name ("SUPPLIERS"), or by its identifier when it has none; the identifier stays the finding's key. Where two accounts share a name, each adds the start of its identifier ("SUPPLIERS (ADSU…)"), as other pages label accounts: on the Findings page over the findings and the mutes lists together, so an account reads the same in both, and on the Overview over its cause cards and muted strip.
- **The finding page.** Its headline is the check name and the entity, under the severity (or muted), code and state; the finding's detail follows, as the check wrote it. One line reads "Condition since <time> · first seen by nats-lens <time>" where the check knows the condition's start, else "First seen by nats-lens <time>"; it adds "already true when nats-lens started" when the condition began before nats-lens's history, or the finding was first seen within a minute after the history starts; and "N other entities have this finding", linking to the Findings page for the check, when other entities have it (muted ones included, resolved ones not). Then how long it has fired since nats-lens first saw it ("Firing for (since first seen)"), when it resolved, its transitions in the last hour, its value with the value's name, and "Cause in window", a link to the event the check names as its cause. Below it: the mute, if any; for a stream or consumer finding, "Server copies"; the remediation (with any text from the remediation file, using the [shared Markdown formatting](checks.md) and contained tables); the related settings, with where they were read from and the metric basis; "Around the start"; "Related" links; a status timeline of every check of the same entity; and the transitions, newest first, at most 100.
- **Server copies.** For a stream or consumer finding, a table of each server's copy: role (leader, follower, behind or offline), creation time, positions (messages and last sequence for a stream, delivered and ack floor for a consumer), lag, and whether the configuration is the same as most copies'. Each value is compared with the one most copies match, which must be more than half of them: a creation time more than an hour from it is marked with the difference (for example "+5d"), a position more than max(100, 1%) from it is marked "differs from most copies", and so is a different configuration. An offline copy's positions are never the reference and are not marked. "Refresh" reloads the table, and it reloads by itself when the finding changes state. When no value has more than half the copies, nothing is marked and the section says "copies disagree". A value a server did not report shows as "–". Other findings, such as a client's, have no table.
- **Persistent timeline inspection.** The finding page opens selected check cells, events and windows in a full-screen investigation. The selected context survives live refreshes, including when the refreshed timeline has no check rows. Close or Escape dismisses it; changing systems closes it.

- **Copy event text.** Types and summaries in the finding timeline are selectable without toggling the event. See [Events](events.md).
- **Around the start.** The events worth reading, folded, in the 10 minutes either side of the condition's start, or of the first sighting without one, at most 20, for the whole system by default; "This entity", offered when the Events page can filter by the entity, narrows them to it. "Showing N of M" counts the events when fewer are shown, and "Open in Events" opens the same window on the Events page. When those minutes lie before nats-lens's history, the page says it has no history from then; when they begin before it, the page says where the history begins.
- **Related.** Links to the entity, its stream (for a consumer), its server, and the Events page filtered to it.
- **Mute a finding.** "Mute…" opens a dialog. It applies to this entity, or to every entity of the check. For a system finding, this entity is the system, which is the check's only entity. It needs a reason (at most 1000 characters) and an end: in 1 hour, 8 hours, 24 hours (the default), 7 days or 30 days, or at a time in UTC. The end must be between one minute and 90 days ahead.
- **Who muted it.** With `iap_audience` set, a mute or a lift needs a valid IAP assertion, nats-lens records the verified IAP user, and the dialog says "The mute is recorded as your IAP identity." Without it, nats-lens records the name the request gives; the dialog asks for "Your name" and the browser remembers it for next time.
- **Lift a mute.** "Lift mute…" on a finding, or "Lift…" in the mutes list, ends a mute early and records who lifted it.
- **Accepted findings.** An acceptance is a mute of one entity's finding with a review date as its end, between a day and 365 days ahead; the Capacity page makes one for `stream-005` ([capacity.md](capacity.md)), and the Overview's cause cards, the finding page and the Findings page for a finding of an acceptable check ([overview-page.md](overview-page.md)). Only a check that describes a configuration choice is acceptable ([checks.md](checks.md)); `MuteFinding` refuses to accept any other, while muting it stays possible. An acceptance stored before its check lost the right keeps applying until its review date. Everything that reads mutes reads it as muted: the Overview's status and severity counts, the Findings badge, the Fleet badges and `/metrics`; the Overview's tally and its strips count it apart from plain mutes ([overview-page.md](overview-page.md)). On the finding page the section is headed "Accepted as by design" and reads "Accepted by <name>: <reason>. Review on <date>."; for an acceptance of a check with the growth review (`stream-005`, `stream-019`, `stream-020`) it adds the growth recorded at acceptance ("Growth at acceptance: +96 MiB/day per copy (the Capacity page's /day counts every replica), over the last 7 days; …"; the end reason measures one copy too), or "No growth baseline is recorded yet: growth is recorded once the stream has 8 days of history.", and its button reads "End acceptance…", which ends it as "Lift mute…" ends a mute. nats-lens ends such an acceptance by itself when the entity's growth over a week changes by more than half ([checks.md](checks.md)); an acceptance of another check ends only at its review date. That review needs 8 days and an hour of history: with a shorter `retention` the finding page says that no baseline is recorded and the acceptance ends only at its review date, and nats-lens logs a warning at start.
- **Accept from the finding.** On the finding page, "Accept as by design…" stands beside "Mute…" for an unmuted finding of an acceptable check that names an entity and has not resolved. On the Findings page, each such firing row has "Accept…" under its time, named for the check and the entity ("Accept stream-020 on audit as by design"). Both open the dialog the Overview and Capacity use: "Accepts <code> <check name> for <entity>", a reason, a review date ("in 3 months" by default) and, without IAP, "Your name".
- **Accept more than one entity.** For a consumer or a stream, the dialog lists under "Also accept, firing now" the same check's other firing, unmuted findings under the same parent, read over the last 24 hours: the other consumers of the stream ("All 3 other consumers of stream order-jobs with this finding"), or the other streams of the account ("All 2 other streams of account APP with this finding"). Each has a box, and the first box ticks or clears them all; they start clear. With one or more ticked the button reads "Accept 3 as by design", and each entity gets an acceptance of its own with the same reason and review date: "Each gets an acceptance of its own with this reason and review date." One action accepts at most 100 entities, the target included; past that the dialog says how many more are not listed. The acceptances are stored together or not at all (`AcceptFindings`), and each is reviewed and ended on its own. While that read loads the section says "Loading related findings…", and a failed read is shown in it. On a cause card of one acceptable check under one stream or account, "Accept all N as by design…" opens the dialog with exactly the card's other findings ticked, listed even when the read does not return them; the button waits until the read has loaded, so the card's findings are never left out unseen. Proposed, not yet confirmed.
- **What an acceptance refuses.** `MuteFinding` with `accept` and `AcceptFindings` refuse an entity of another type than the check judges (a consumer for `stream-005`) as `invalid_argument`; an entity the check has no finding for, in any state, as `not_found` (a misspelt stream, or another account's stream of the same name); and an entity with an acceptance of the check already in force as `already_exists`. A batch is refused whole, naming the entity. A plain mute keeps its own rules. Decided in the session that built it, not confirmed.
- **Review accepted findings.** "Review accepted findings", at the top right of the Findings page and on the Overview's accepted strip, opens "Accepted findings" (`/findings/accepted`). It lists the acceptances in force, the soonest review date first: Accepted, the check code (a link to the finding), the check name, the entity, "review on <date>", "in <duration>" and "End acceptance…", then "Accepted by <name> on <time>: <reason>. Review on <date>." and, for a check with the growth review, "Growth at acceptance: +96 MiB/day per copy." or "No growth recorded yet: it is recorded once the entity has 8 days of history." "End acceptance…" ends it as "Lift mute…" ends a mute. "Include ended" adds the ended ones after those in force, the latest end first, marked Ended (by a person or by nats-lens, with its reason) or Expired. The list follows the header's account scope as the Findings page does, and counts the rest ("2 acceptances of other accounts are not shown.") with "Show all accounts". The page has no time range: it lists the acceptances as they stand now, and the header offers no range. "Include ended" stays in the address (`?ended=1`). `ListAcceptances` serves it. Proposed, not yet confirmed.
- **What to do, ready to copy.** Under the remediation, "Check it with the nats CLI" gives the command that shows what the finding reports, such as `nats stream info --json -- order-archive` for a stream or `nats consumer info --json -- order-jobs resize-worker` for a consumer, with "Copy nats CLI check" and the nats context to run it with ("Run with a nats context for account APP on example-prod."). The command follows the cause cards' rule ([overview-page.md](overview-page.md)).
- **Placement link.** Under "Server copies", "Open this stream in Placement" (or consumer) opens the Placement page with the leader's copy selected, or the first server's when none leads ([placement.md](placement.md)).
- **The mutes list.** Mutes in force by default; "Include ended" adds lifted and expired ones. Each shows Muted, Accepted, Lifted, Ended or Expired, the check, the entity or "every entity", when it ends (an acceptance: "review in <duration>") or ended, and "Muted by … on …: reason" (or "Accepted by …", with the review date), with who lifted it. An acceptance in force has "End…" in place of "Lift…"; each button's accessible name names the check and the entity ("End acceptance of stream-005 for order-archive…"). An acceptance that ended early is marked Ended, never Lifted: one a person ended reads "Ended <time> by <name>.", and one nats-lens ended reads "Ended by nats-lens: growth changed from +2.0 KiB/day to +516 KiB/day over the last 7 days (+1.6 MiB in the last day)."
- **Mutes apply when findings are read.** A mute does not stop the check. When it ends, the finding counts again if the condition still holds.

- **Single-page findings.** The Findings list keeps its result count without a page number or Previous/Next controls when all results fit on the first page. Multi-page lists keep navigation on their final page.
- **Finding summary layout.** Each duration, transition count, and check metric has a label above its value with space between them. The severity, code, and state badges have separate gaps. Acknowledge and Mute actions are spaced apart and wrap inside the card when the window narrows.
- **Acknowledgements beside mutes.** An acknowledgement records who is investigating one finding's current firing episode. It leaves the finding visible, counted at its severity, and eligible for the Findings badge. A mute hides the finding from those counts. “Acknowledge…” on desktop and “I'm on it” on the [phone card](mobile.md) accept an optional note of at most 500 characters, with 30 minutes, 2 hours, or until-resolved quiet. Quiet expiry resumes alert eligibility but keeps the acknowledgement. Resolve or a later firing episode stops it applying. “Update…” replaces it; “Hand back…” releases it. The verified IAP user or request name identifies the author, as for mutes. The API also accepts quiet durations from 30 minutes through 24 hours.
- **Acknowledgement visibility.** Overview cards, Findings rows, and finding details show the current acknowledgement. The Overview counts acknowledged firing findings, including muted ones. The finding page keeps replacement and hand-back receipts only during the visit to the same system, finding, and episode; resolving the finding clears those receipts. The API has no acknowledgement-history list. Reading a past range selects the acknowledgement in force at its end.

### Retained finding occurrences

- Timeline finding intervals use a separate occurrence read rather than the compatibility Findings list, which can show the newest occurrence for an ID. Repeated occurrences remain separate and include recorded resolutions. Each link fixes the system, UTC interval, first retained transition identity and transition high-water mark. Occurrence reads, transition pages, destination links and copied evidence preserve the original fractional timestamp bounds.
- The occurrence page shows the recorded state before the selected interval end, observed resolution when retained, and the separately labeled latest retained state. Original onset, historical configuration, historical operator state and historical flapping are not reconstructed. Missing resolution records remain unknown; an old fragment does not extend past a separately recorded new firing. Current check guidance is labeled as current.
- Raw history is explicit: at most 100 transitions per request, newest collection first, with byte-bounded responses and continuation. Transition identity strings retain 64-bit precision. Demo identities belong to a synthetic reference frame, not native collection IDs. A missing exact occurrence explains unavailable evidence rather than opening a newer recurrence.
- A labeled Copy as prompt action preserves the exact bounded read in descriptor-validated context data. The latest-finding link is an explicitly different destination; historical pages offer no mute or acknowledgement action against an old occurrence. The [limits and API contract](../reference/finding-occurrences.md) are proposed, not yet confirmed.

### Keep a finding timeline selection through refresh

- **Surfaces:** `ui`
- **Given** I open a check cell in a finding page’s timeline
- **When** a live refresh removes that check row
- **Then** the full-screen investigation retains the selected context until I close it

### Reopen an older occurrence

- **Surfaces:** `ui`, `api`
- **Given** a finding resolved and fired again
- **When** I open its older Timeline occurrence
- **Then** its retained state and transitions remain separate from the later recurrence
- **And** missing history is disclosed instead of replaced by current configuration or another occurrence


## Scenarios

### List one account's findings

- **Surfaces:** `ui`, `api`
- **Given** example-global, where SUPPLIERS' account JWT expires soon and APP's streams have findings
- **When** I choose "Account: SUPPLIERS" on the Findings page
- **Then** the list shows `account-003` for SUPPLIERS and the server and client findings, and no APP stream finding
- **And** a line counts APP's findings as not shown, with "Show all accounts"

### Look at everything that flaps

- **Given** a finding that resolved and fired again five times in the last hour
- **When** I choose the Flapping state
- **Then** the finding is listed with the Flapping tag

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

### Read an accepted finding

- **Surfaces:** `ui`
- **Given** `example-prod`'s `stream-005` on `KV_customer-config` is accepted by `ops@example.com` with a review in six months
- **When** I open the Findings page and the finding
- **Then** the row is marked Accepted and reads "Accepted by ops@example.com: Customer records are kept for the account's life. Review on <date>."
- **And** the finding page's section is headed "Accepted as by design", names the growth recorded at acceptance, and offers "End acceptance…"

### See an acceptance nats-lens ended

- **Surfaces:** `ui`
- **Given** in `example-global` nats-lens ended the acceptance of `stream-005` on `KV_feature-flags` when the bucket's growth changed
- **When** I tick "Include ended" in the mutes list
- **Then** the acceptance is marked Ended and reads "Ended by nats-lens: growth changed from +2.0 KiB/day to +516 KiB/day over the last 7 days (+1.6 MiB in the last day)."

### Accept every consumer of one stream at once

- **Surfaces:** `ui`, `api`
- **Given** `consumer-016` fires for the consumers `a`, `b` and `c` of `order-jobs`, which run with one replica on purpose
- **When** I open the finding for `a`, choose "Accept as by design…", tick "All 2 other consumers of stream order-jobs with this finding", give a reason and press "Accept 3 as by design"
- **Then** each of the three consumers has an acceptance of its own with that reason and review date, made in one `AcceptFindings` call
- **And** a call that names one entity twice, more than 100 entities or a check that is not acceptable stores none of them

### Review what was accepted

- **Surfaces:** `ui`, `api`
- **Given** `example-prod`'s `stream-005` on `KV_customer-config` is accepted with a review on 2027-04-03
- **When** I choose "Review accepted findings" on the Findings page
- **Then** "Accepted findings" lists it with "review on 2027-04-03", who accepted it, why and the growth at acceptance, the soonest review first, and offers "End acceptance…"
- **And** with "Include ended" it also lists the acceptances that ended, after those in force

### See which server's copy is odd

- **Surfaces:** `ui`
- **Given** `consumer-009` fires for `resize-worker` on `order-jobs`, whose copy on `nats-0` was created two months before the copies on `nats-1` and `nats-2`
- **When** I open the finding
- **Then** "Server copies" lists the three servers, marks the `nats-0` creation time with "−65d", and shows the delivered and ack floor positions of each

### A condition older than the history

- **Surfaces:** `ui`
- **Given** nats-lens started at 08:19 and a finding's condition began four days earlier
- **When** I open the finding
- **Then** it reads "Condition since <four days ago> · first seen by nats-lens <08:19>, already true when nats-lens started", and "Around the start" says nats-lens has no history from then

## Planned

- **Why this severity.** A sentence that says why the finding is info rather than a warning, and what would raise it.
- **The playbook on the finding page.** A link to the playbook that applies, which the phone card already has ([mobile.md](mobile.md)).

### Acknowledge without hiding trouble

- **Surfaces:** `ui`, `api`
- **Given** an unmuted firing finding
- **When** an operator acknowledges it with a note and 2 hours of quiet
- **Then** it remains visible and counted at its severity, with the author and note
- **And** quiet expiry permits alerts again while the acknowledgement remains until release, replacement, or the episode ends

### Reject an acknowledgement outside a firing episode

- **Surfaces:** `api`
- **Given** a resolved finding
- **When** a client calls `AcknowledgeFinding`
- **Then** the API refuses it with failed_precondition
- **And** an unknown finding is refused with not_found

### Refresh only relevant operator evidence

- **Surfaces:** `ui`
- **Given** current or historical findings for one system and unrelated series or another system remain cached
- **When** I mute, unmute or acknowledge a finding
- **Then** operator-dependent reads for the captured system and shared fleet summaries refresh, while raw series and the other system remain cached
- **And** acknowledgement creation refreshes captured-event feeds, while an acceptance save does not wait for its background refresh

## Not supported / out of scope

- **No mute without an end.** Every mute ends within 90 days.
- **No editing a mute.** A mute's reason or end cannot be changed; lift it and mute again.
- **No mute of several checks at once.** A mute covers one check.
- **No acceptance of a check everywhere.** An acceptance names one entity; there is no acceptance of a check for every entity, present and future. A configuration choice is made per stream or consumer, the growth review measures one entity, and a check accepted everywhere would also hide the next stream created without limits, which is what the check is for. Accept the entities there are, up to 100 at once; mute the check for every entity for a while, or turn it off with `checks.disabled` for good. Proposed, not yet confirmed.
- **No editing an acceptance.** Its reason and review date cannot be changed; end it and accept again.
- **No acknowledgement-history list.** The finding page reads the current episode's acknowledgement. Replacement and hand-back receipts last only for the current visit; stored acknowledgement events remain readable within event retention.
- **No sign-in of its own.** Without IAP, the name in a mute is whatever the person types.
