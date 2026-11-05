# Data history

- **Surfaces:** `ui`, `api`
- **Status:** partial — forecast margins and the T4 documentation consolidation remain planned
- **Related:** [history-and-archive.md](history-and-archive.md), [monitoring-snapshots.md](monitoring-snapshots.md), [event-capture.md](event-capture.md), [triage-and-mutes.md](triage-and-mutes.md)

## What it does

Data history shows what nats-lens holds of a system. Range pages name a history shorter than the requested range, charts hatch times without collections, and missing growth figures explain the history they need. Forecast margins remain planned.

## Behavior and rules

- **History begins here.** When the chosen range starts more than one collection interval before the oldest collection, a note names the available span and links to Data history. Charts keep the full requested time axis, including the time before history.
- **Gaps are drawn.** Trend and series charts hatch times without collections and the time before history, with a tooltip and accessible name. Event-capture and Kubernetes-watch gaps leave the collection charts unhatched. Charts on the same page share the cached history request; a page without charts makes no history request for hatching.
- **Growth needs history.** Capacity, Streams, and the Overview storage tile share the six-hour requirement. A live range long enough for growth counts down the missing history. A shorter range asks for at least six hours; a fixed range names its available history. Forecast margins remain planned.
- **Findings say what they know.** A finding whose start nats-lens cannot know says "already true when nats-lens started", never a fresh start time.
- **Tiles for the range.** Overview message and connection trends, connections tiles, and storage name the shorter history behind the range. Missing-history words on Capacity, Accounts, Applications, Replay, and finding details link to Data history with the global query.
- **Built on stored data.** The oldest collection, the gap events and the collection times are stored today; the API already reports where the history starts.

## Scenarios

### Open a 24-hour range right after the start

- **Surfaces:** `ui`
- **Given** nats-lens started collecting 31 minutes ago
- **When** I open the Overview for the last 24 hours
- **Then** a banner says that there are 31 minutes of history, and the charts retain the full requested range with the time before history hatched

### See a gap in the data

- **Surfaces:** `ui`
- **Given** nats-lens was stopped for three minutes an hour ago
- **When** I look at a chart that covers that hour
- **Then** the three minutes are hatched and labelled as a gap

### Ask where the history starts

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `GetLensInfo`
- **Then** it receives the start of the oldest stored collection

## Not supported / out of scope

- **No backfill.** nats-lens cannot collect for a time when it was not running.
- **No history from the archive in the UI.** Hours older than `retention` are in the archive only ([history-and-archive.md](history-and-archive.md)).
- **No forecast margins.** Forecast confidence margins remain planned.
