# Command bar

- **Surfaces:** `ui`, `api`
- **Status:** partial — names, findings and events in grouped results with prefixes, and Ask your agent, are shipped; applications, recently resolved findings and per-result notes such as "servers disagree" are planned
- **Related:** [navigation.md](navigation.md), [agent-investigations.md](agent-investigations.md), [triage-and-mutes.md](triage-and-mutes.md), [events.md](events.md), [sql-access.md](sql-access.md)

## What it does

The search box in the header, focused with Cmd+K on a Mac or Ctrl+K elsewhere, finds what nats-lens knows about the current system: servers, streams, consumers, KV stores, object stores and accounts by name, the findings of the selected range by check or entity, and its events by type or name. Results come grouped by kind, and a prefix such as `stream:` or `since:2h` narrows them. Picking a result opens its page ([navigation.md](navigation.md)). The last row, "Ask your agent", copies the page's agent prompt with the text as a question, for the user's own agent; nats-lens itself answers nothing and sends the question nowhere.

## Behavior and rules

- **Names.** A server, stream, consumer, KV store, object store or account shows when its name contains the text, ignoring case; names that start with the text come first. The names load the first time the box gets focus and are searched in the browser, from the first character.
- **Findings.** A finding of the selected range shows when its check code, check name or entity (key, name, account, stream, consumer or server) contains the text, ignoring case. Muted and accepted findings show too; resolved ones do not. Each row shows the check code, the entity, and a note with the severity and "firing since" or "flapping since" a time, with "muted" or "accepted" when it applies. Picking one opens the finding's page.
- **Events.** An event of the selected range shows when its kind, reason (such as `Unhealthy`), server, account (its key or, in operator mode, its readable name), stream, consumer, client name, Kubernetes object (a Kubernetes Event's `namespace/name`, such as `shop/resize-worker-6c9d4-x2k4l`, a pod's `namespace/pod`, or a node's name), finding ID (for a finding event) or the ID of a finding it caused contains the text, ignoring case, so a check code also finds the event behind its finding. Routine events, which the Events page hides by default, are left out, and repeats are folded as the Events page folds them: a run of the same event becomes one row with its count, such as "×5 · 18:58" at the newest time. Each row shows the consumer, stream, client or server the event concerns with its type, or, when it names none, such as a Kubernetes event, its type and summary; and its time, newest first. Picking one opens the Events page on that event; a folded row opens its newest event, and the others of its run are not marked there.
- **Bounded requests.** Findings and events are filtered on the server, by the `text` field of `ListFindings` and `ListEvents`. The command bar asks for them 200 ms after the last keystroke, and only once the text has two characters or more. It cancels a request whose text has since changed and never shows its result. Each request reads one page of at most 5 rows, or 10 when the box is narrowed to that kind.
- **Grouped results.** The groups come in a fixed order: Servers, Streams, Consumers, KV stores, Object stores, Accounts, Findings, Events. A group with no match is left out. Each name group shows at most 4 rows, findings and events at most 5. While the text settles and the next findings or events load, the rows of the previous text stay, dimmed, and the group says "Searching…". They cannot be picked, by the keyboard or a click, until the new rows arrive, and they stay only for the same system, range, `since:` window and narrowing, so another system's rows never show. With nothing to keep, the group says "Searching…" alone. When the request fails it says "Could not search findings" or "Could not search events". The name results show meanwhile.
- **Show all.** A group with more matches (for events, another page on the server) ends with "Show all N", which narrows the box to that kind (`stream: order`): up to 20 names, or 10 findings or event rows, then show. A narrowed group with still more ends with "Open the streams page" (or the kind's own page), which opens that page on the same range. The list pages do not take the text yet, so the page shows its whole list.
- **Prefixes.** `server:`, `stream:`, `consumer:`, `kv:`, `object:`, `account:`, `finding:` and `event:` narrow the results to one kind. The text may follow the colon directly or after a space, and the last kind prefix wins; a prefix may follow a kind prefix directly, as in `event:since:2h`. A kind prefix with no text lists that kind: names by name, findings worst first, as the Findings page orders them, events newest first. `since:` with minutes, hours or days (`since:30m`, `since:2h`, `since:1d`, at most 30 days) narrows findings and events to that last part of the selected range; it does not widen the range. Any other word with a colon, such as `subject:orders` or `since:soon`, is searched as text.
- **Applied prefixes.** Above the results each applied prefix shows as a chip, such as "Events" or "Since 2h", with a button that removes it from the text; only the word that held the prefix changes.
- **Hint.** With the box empty and focused, a line lists the prefixes and `ask:`.
- **Keyboard first.** The arrow keys move through every row of every group, "Show all" rows included, and wrap around; the list scrolls to keep the active row in view. Enter opens; the first Escape closes the list and the second clears the text. The cursor stays in the box.
- **Phone width.** The results take the width of the screen less its margins, and a finding's severity and time go under its code and entity.
- **Ask your agent.** The text can be a question for the user's own terminal-capable agent ([agent-investigations.md](agent-investigations.md)). An "Ask your agent" group with one row, "Copy prompt with question", ends the list for any text without a kind prefix, after the results, so the arrow keys reach it last (or first with the Up arrow). Text that starts with `ask:` is a question only: the rest of the text, as typed, other prefixes included, is the question, no name, finding or event is searched, the row is the only one, and a chip "Ask your agent" removes the prefix. A note under the list says that the row copies the page's agent prompt with the question, up to 2,000 characters, and that nats-lens sends it nowhere.
- **The question.** It is trimmed. An empty one ("ask:" alone) and one of more than 2,000 characters cannot be picked: the row says "Type a question after ask:" or, for example, "2,001 characters; a question takes at most 2,000".
- **The prompt.** Picking the row builds the prompt that the header's "Copy as prompt" builds for the current page: the system, the range, the page's context and the bounded read-only workflow, with all NATS data in the context JSON framed as untrusted evidence. The question comes first, between `BEGIN USER QUESTION` and `END USER QUESTION` lines, marked as the user's own text that does not widen the prompt's rules. It is written as one JSON string on one line, so a question that holds a marker, such as `END NATS LENS CONTEXT JSON`, cannot end the context or the question early. When the page adds no selection, resource or filter, as the Overview does, the prompt says that the context holds the system and range only. The question is not part of the context JSON, so `natslens investigate` reads the context as before.
- **Copy.** The prompt goes to the clipboard as "Copy as prompt" copies, with the same "Prompt copied" status; if the clipboard refuses it, the same dialog shows the whole prompt in a selectable field with "Retry copy". The text clears and the cursor stays in the box, and closing the dialog returns focus there. Until the system and its lens information have loaded the row is not offered, and after `ask:` the list says "Ask your agent is available once this system has loaded."
- **Read-only.** The command bar opens pages and copies prompts; it changes nothing. Picking Ask makes no request, and after `ask:` the text is not searched either.

## Scenarios

### Jump to a consumer

- **Surfaces:** `ui`
- **Given** the UI is open on any page
- **When** I press Cmd+K (Ctrl+K off a Mac), type `resize`, and press Enter on `resize-worker`
- **Then** the consumer's page opens

### Find a finding by its check code

- **Surfaces:** `ui`
- **Given** `consumer-010` fires for the consumer `analytics-aggregator`
- **When** I type `consumer-010` in the command bar
- **Then** the Findings group shows `consumer-010 analytics-aggregator` with "Critical · firing since" a time
- **And** the Events group shows the `CONSUMER.DELETED` event that caused it
- **When** I press Enter on the finding
- **Then** the finding's page opens

### Find everything about one name

- **Surfaces:** `ui`
- **Given** the consumer `analytics-aggregator` and its client `analytics-worker` exist
- **When** I type `analytics`
- **Then** the results show a Consumers group, a Findings group and an Events group, in that order
- **And** the arrow keys move from the consumer to the finding to the events

### Recent events only

- **Surfaces:** `ui`
- **Given** the selected range is the last 24 hours
- **When** I type `event: since:2h`
- **Then** only the Events group shows, with the newest events of the last two hours
- **And** chips show "Events" and "Since 2h"

### Find a Kubernetes event by its reason or pod

- **Surfaces:** `ui`
- **Given** the pod `shop/resize-worker-6c9d4-x2k4l` failed its readiness probe three times
- **When** I type `Unhealthy`, or `shop/resize-worker`
- **Then** the Events group shows those events

### One row for a run of the same event

- **Surfaces:** `ui`
- **Given** the client `analytics-worker` connected five times in a few minutes
- **When** I type `analytics`
- **Then** the Events group shows one `analytics-worker CLIENT.CONNECT` row with "×5" before the newest time

### Narrow a group that has more

- **Surfaces:** `ui`
- **Given** six streams contain `order`
- **When** I type `order`
- **Then** the Streams group shows four of them and ends with "Show all 6"
- **When** I pick "Show all 6"
- **Then** the box reads `stream: order` and all six streams show

### Ask little while typing

- **Surfaces:** `ui`
- **Given** the command bar is open
- **When** I type `a`
- **Then** nats-lens asks for no findings and no events
- **When** I type nine more characters, each within 200 ms of the last
- **Then** nats-lens asks once for findings and once for events, for the whole text

### Ask your agent about a server

- **Surfaces:** `ui`
- **Given** the page of the server `nats-1` is open
- **When** I press Cmd+K (Ctrl+K off a Mac), type `ask: why did nats-1 restart?`, and press Enter
- **Then** "Prompt copied" shows, and the clipboard holds the agent prompt of the server's page with `"why did nats-1 restart?"` between `BEGIN USER QUESTION` and `END USER QUESTION`
- **And** nats-lens asks the server for no findings or events, and no request carries the question

### Ask from the Overview

- **Surfaces:** `ui`
- **Given** the Overview is open and the clipboard is unavailable
- **When** I type `how healthy is the system`, press the Up arrow to "Copy prompt with question", and press Enter
- **Then** the "Copy as prompt" dialog shows the prompt, which says that the context holds the system and range only
- **When** I press Escape
- **Then** the dialog closes and the cursor is back in the command bar

### Refuse a question that is too long

- **Surfaces:** `ui`
- **Given** the command bar is open
- **When** I paste `ask:` followed by 2,001 characters
- **Then** the row says "2,001 characters; a question takes at most 2,000" and Enter copies nothing

### Filter findings and events by text

- **Surfaces:** `api`
- **Given** an event names the client `Analytics-Worker`, and a finding of `consumer-010` fires
- **When** a client calls `ListEvents` with `text: "analytics"` and `ListFindings` with `text: "CONSUMER-010"`
- **Then** the event and the finding are returned, and events and findings that do not contain the text are not
- **And** a `text` longer than 256 characters is rejected as an invalid argument

## Planned

- **Applications in the results.** Applications by name, as the Applications page groups them ([applications.md](applications.md)).
- **Recently resolved findings.** Findings that resolved within the range, after the firing ones.
- **Notes on name results.** A short note per result, such as "servers disagree" for a consumer.

## Not supported / out of scope

- **No built-in model.** nats-lens does not answer a question itself or call a language model; Ask hands the question to the user's own agent. A built-in model would need an API key and outbound network access from where nats-lens runs, which is often a cluster with restricted egress and least-privilege NATS credentials. It would send account names, subjects, client names and addresses to a third party. Its answers would rest on NATS data that anyone who can publish to NATS can shape, so text in that data could steer it. And its answers would change with the model and could not be tested. Decided by the session at the owner's request ([Owner delegation, 2026-11-11](../design.md#owner-delegation-2026-11-11)).
- **No search across systems.** The command bar reads the current system only.
- **No filtered list page.** "Open the streams page" and the like open the whole list: the list pages do not take a text filter from the address yet.
- **No actions.** The command bar does not mute, restart or change anything.
