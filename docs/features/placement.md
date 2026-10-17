# Placement

- **Surfaces:** `ui`, `api`
- **Status:** planned
- **Related:** [streams.md](streams.md), [consumers.md](consumers.md), [stream-checks.md](stream-checks.md), [consumer-checks.md](consumer-checks.md), [replay.md](replay.md)

## What it does

Planned; nothing here is built yet. The Placement page will show every stream and consumer as each server sees it, in one matrix: a row per asset, a column per server. Each cell says whether the server leads the asset, follows it in sync, disagrees with the others, is assigned but missing it, or does not hold it. It answers at a glance where each asset lives and where the servers disagree. All scenarios below describe intended behaviour.

## Behavior and rules

- **One row per asset, one column per server.** Streams with their consumers indented below them. Each server's column header shows how many assets it leads, its memory and uptime, and whether it is the meta leader.
- **Cell states.** Leader (L) or follower (F); in sync; disagrees with the others; assigned but missing; not placed here. The cell text says what differs, or else the replica's lag.
- **Filters with counts.** All, servers disagree, replica behind, missing somewhere, single replica.
- **Compare with another time.** A selector compares the matrix with an earlier moment, such as this time yesterday.
- **Select a cell.** A side panel shows that server's view of the asset: its creation time and the other servers' times, its positions against the others, its peers, how long the state has lasted, what it means, the `nats` CLI command to check it by hand, and a link to its events.
- **No write actions.** A "Step down leader" button copies the command and links the playbook; nats-lens does not run it.
- **Built on stored data.** Everything the matrix needs, each server's report of each stream and consumer, nats-lens already stores every collection.

## Scenarios

### Find every asset the servers disagree about

- **Surfaces:** `ui`
- **Given** four consumers of `order-jobs` have another creation time on `nats-1`
- **When** I open Placement and choose "Servers disagree"
- **Then** the four consumers are listed, with the `nats-1` cells marked as disagreeing

### Check one replica by hand

- **Surfaces:** `ui`
- **Given** the matrix shows `resize-worker` disagreeing on `nats-0`
- **When** I select that cell
- **Then** the side panel shows each server's creation time, whether the positions agree, and the `nats consumer info` command to compare them

### Read the matrix from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client asks for the placement at a moment
- **Then** it receives each asset's state on each server

## Not supported / out of scope

- **No moving of assets.** nats-lens does not change placement, remove peers or step leaders down.
- **No placement for core NATS subscriptions.** Only JetStream assets have a placement.
