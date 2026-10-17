# Replay

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [history-and-archive.md](history-and-archive.md), [events.md](events.md), [kubernetes-context.md](kubernetes-context.md), [placement.md](placement.md), [overview-page.md](overview-page.md)

## What it does

Planned; nothing here is built yet. Replay will answer "what changed between these two moments, and in what order?" An operator picks two moments, A and B, and nats-lens tells the story in between from its collections, NATS advisories and Kubernetes events: which servers restarted, which leaders moved, which assets lost quorum or disappeared, which node pools were upgraded. Every line links to its evidence. All scenarios below describe intended behaviour.

## Behavior and rules

- **Two moments.** A and B are picked on a strip of the range that marks the drain and upgrade windows.
- **What happened, in order.** A list of steps with their times, each a sentence with a short explanation, for example "nats-1 restarts" followed by what it recovered.
- **Between A and B, in numbers.** Servers restarted, assets gone without a delete event, leaders moved, node pools upgraded, client reconnects.
- **Gone between A and B.** Assets that servers held at A and no server holds at B, with how many servers held them at each moment.
- **Steps outside the range.** A step after B that closes the story, such as the finding resolving, is shown and marked as outside the range.
- **Evidence.** Every step links to the collection, event or finding it was built from.
- **Export.** "Export as incident log" writes the story as text, and "Copy link to A–B" shares it.

## Scenarios

### Explain a night's node upgrade

- **Surfaces:** `ui`
- **Given** a node upgrade drained the NATS nodes between 02:00 and 03:30
- **When** I set A to 02:00 and B to 03:30
- **Then** I see the upgrade start, each server restart, the leader moves and any quorum loss in order, with times, and the counts of what changed

### Find assets that vanished

- **Surfaces:** `ui`
- **Given** two consumers that all three servers held at A and none holds at B
- **When** I open the Replay of A to B
- **Then** "Gone between A and B" lists both, with "3 servers" at A and "none" at B

### Fetch the story from the API

- **Surfaces:** `api`
- **Given** two moments within `retention`
- **When** a client asks for the replay between them
- **Then** it receives the ordered steps with links to their evidence

## Not supported / out of scope

- **No moments older than `retention`.** Replay reads the database, not the archive.
- **No guesses.** A step is shown only when a collection, event or finding backs it; a period without collections is shown as a gap.
