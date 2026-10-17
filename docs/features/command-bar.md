# Command bar

- **Surfaces:** `ui`
- **Status:** partial — the header search jumps by name to servers, streams, consumers, KV stores, object stores and accounts; findings, events, grouped results and prefixes are planned
- **Related:** [navigation.md](navigation.md), [sql-access.md](sql-access.md), [events.md](events.md)

## What it does

The search box in the header, focused with Ctrl+K (Cmd+K on a Mac), finds servers, streams, consumers, KV stores, object stores and accounts of the current system by part of their name and opens the one you pick ([navigation.md](navigation.md)). Planned: a command bar that searches everything nats-lens knows, findings, events and applications too, with grouped results and prefixes.

## Behavior and rules

- **What it finds today.** Names that contain the typed text, those that start with it first, at most eight, each with its kind and its account, stream or cluster.
- **Planned: grouped results.** Results are grouped by kind (Consumers, Services, Findings, Events, and so on), each with a short note, such as "servers disagree" for a consumer or "firing since" for a finding.
- **Keyboard first.** The arrow keys move, Enter opens, and Escape closes.
- **Planned: prefixes narrow the search.** `stream:`, `server:` and `since:2h` limit the results to one kind or a time.
- **Read-only.** The command bar opens pages; it changes nothing.

## Scenarios

### Jump to a consumer

- **Given** the UI is open on any page
- **When** I press Ctrl+K, type `resize`, and press Enter on `resize-worker`
- **Then** the consumer's page opens

### Find recent events of a server (planned)

- **Given** the command bar is open
- **When** I type `server:nats-1 since:2h`
- **Then** the results show `nats-1` and its events of the last two hours

## Not supported / out of scope

- **Ask (a question box answered by an LLM): decision pending.** The design shows an "Ask" mode that answers a question in prose, links every sentence to its evidence, lists the read-only queries it ran on nats-lens's own data, and can neither change NATS nor mute anything. Whether nats-lens gets it is not decided.
- **No actions.** The command bar does not mute, restart or change anything.
