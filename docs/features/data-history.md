# Data history

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [history-and-archive.md](history-and-archive.md), [monitoring-snapshots.md](monitoring-snapshots.md), [event-capture.md](event-capture.md), [triage-and-mutes.md](triage-and-mutes.md)

## What it does

Planned as a page; parts of it exist today. nats-lens will say plainly what history it holds: when it started collecting, where it has gaps, and how much data each forecast uses. Today the pieces are spread out: `collector.gap` and `collector.not_connected` events record the gaps, a finding first seen at the start of the history says "first seen when nats-lens history begins", and the growth figures are not shown with less than 6 hours of data. The scenarios below describe the intended page.

## Behavior and rules

- **History begins here.** When the chosen range starts before nats-lens's oldest collection, a banner says so, for example "31 min of history: you asked for the last 24 h; nats-lens started at …, so charts show … to now", with "Show the full 24 h anyway".
- **Gaps are drawn.** Charts hatch the time without data and label it, such as "No data: nats-lens was not running" or a short gap between two times.
- **Forecasts say how sure they are.** Each forecast says whether it is still collecting ("needs 6 h, 5 h 29 m to go"), or gives its result with the span it used and a margin.
- **Findings say what they know.** A finding whose start nats-lens cannot know says "already true when nats-lens started", never a fresh start time.
- **Tiles for the range.** Messages per second, connections, storage and growth, each with the span of history behind it.
- **Built on stored data.** The oldest collection, the gap events and the collection times are stored today; the API already reports where the history starts.

## Scenarios

### Open a 24-hour range right after the start

- **Surfaces:** `ui`
- **Given** nats-lens started collecting 31 minutes ago
- **When** I open the Overview for the last 24 hours
- **Then** a banner says that there are 31 minutes of history, and the charts show only those

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
