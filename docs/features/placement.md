# Placement

- **Surfaces:** `ui`, `api`
- **Status:** partial — how long a state other than a leader's has lasted, and a link to a playbook, are not built
- **Related:** [streams.md](streams.md), [consumers.md](consumers.md), [kv-and-object-stores.md](kv-and-object-stores.md), [stream-checks.md](stream-checks.md), [consumer-checks.md](consumer-checks.md), [replay.md](replay.md), [design.md](../design.md)

## What it does

The Placement page, under JetStream in the sidebar, shows every stream and durable consumer as each server sees it, in one matrix: a row per asset, a column per server. Each cell says whether the server leads the asset, follows it in sync or behind, disagrees with the others, is assigned but reports nothing, is offline, or holds no replica. It answers at a glance where each asset lives and where the servers disagree. Select a cell for that server's view against the others' and the `nats` command to check it by hand. `GetPlacement` serves the same matrix to any API client.

## Behavior and rules

- **Rows and columns.** Each stream, KV bucket and object store is a row, by account and name, with its durable consumers indented below it. Below the name the row says "durable" for a consumer, "KV bucket" or "object store", the configured replicas (`R3`), a stream's message count, and how many configured replicas no server names ("1 unplaced"). The name opens the asset's page. Each column is a server, with how many streams and consumers it leads, "meta leader", its memory and uptime, and "not answering" for a server that missed the collection. The server's name opens its page.
- **Only the servers that matter.** The matrix shows the servers that hold a replica of a listed asset (or held one at the moment compared with) and says how many hold none, such as "The 3 servers that hold these replicas; 21 hold none." "Show all 24 servers" shows every column; the choice is in the address as `?servers=all`.
- **One collection.** The matrix reads the newest collection in the page's range, as every list does, and the line above it names its time, such as "in the collection of 14:29:56 UTC". A range without a collection says "No collection in this range."
- **Cell states.** A cell's state is the replica's placement square on the Overview and in the Streams API ([streams.md](streams.md)): leader, in sync, behind, disagrees, missing (the leader lists the server, which reports nothing), offline (the leader lists it as offline) or state unknown (no server leads). A consumer's replicas follow the same rule, from the consumer's own replica list. A server that holds no replica shows a faint dot ("not placed here"). Each cell draws the state's square, `L` for the leader or `F` for a follower, and a short text.
- **What differs.** A cell also marks the settings on which its server's report differs from what most servers report, the leader's value on a tie: the creation time and the configuration. Such a cell is drawn as disagreeing whatever its state, and its row is marked at its left edge. The cell's text says what differs, such as "created 10-13 08:03" or "config differs"; otherwise it gives the replica's lag ("lag 0"), the leader's last sequence ("last seq 7,378") or a consumer leader's delivered sequence, or the state ("reports nothing", "offline", "no leader").
- **Filters with counts.** All, Servers disagree, Replica behind, Missing somewhere and Single replica, each with the number of assets, streams and consumers alike, that it lists. Servers disagree lists the assets whose servers disagree on the configuration or creation time, as the lists' "servers disagree" says, and those with a disagreeing replica; Replica behind those with a replica behind; Missing somewhere those with a missing replica or one no server names; Single replica those with one configured replica. A stream listed only for its consumers is shown muted. The filter is in the address as `?filter=disagree|behind|missing|single`.
- **Compare with another time.** "Compare with" offers "this time yesterday" and "a time of my choice" (a UTC date and time). nats-lens reads the matrix of the newest collection in the hour before that moment, says "Compared with the collection of 16 Oct 14:30 UTC: 2 cells differ.", counting the cells of every asset whatever the filter, and marks each cell whose state differs then with a dot, including a server that held a replica then and holds none now. A screen reader hears the earlier state in the cell's name. While a newly chosen moment loads, the line says "Loading the matrix of …" and no cell is marked from the moment before; the same holds after a change of system or of the page's range, until both the new moment's matrix and the range's own matrix have loaded. On a live range "this time yesterday" moves with the clock and keeps its marks while the next minute's matrix loads. When the matrix of that moment or the page's unfiltered matrix fails to load, the line says "Could not load the matrix to compare with:" and the error, and no cell is marked, even while the matrix from before the failure stays on screen. The choice is in the address as `?cmp=1d` or `?cmp=2026-10-16T09:00:00Z`.
- **Select a cell.** A cell is a button; selecting it opens the side panel, beside the matrix on a wide screen and below it on a phone, and puts the cell in the address (`?asset=…&server=…`), so a link opens the same cell. The panel shows:
  - the state, and what differs from the other servers;
  - this server's creation time and each other server's, the leader marked;
  - this server's position (a stream's last and first sequence, a consumer's delivered and acknowledgement floor sequence) and whether every server that reports one reports the same, else each server's;
  - the lag the leader reports, and the configuration hash;
  - the replicas this server names, itself included, and whether every server names the same ones;
  - since when: the leader's tenure for the leader cell, how old a copy with another creation time is, and the state at the moment compared with;
  - what the state means, in a sentence or two;
  - the `nats stream info` or `nats consumer info` command to check it by hand (with `| jq .created` when the creation time differs), with a Copy button;
  - links to the asset's events and its page.
- **Commands are safe to paste.** Every command puts the stream and consumer names after `--`, quoted when needed, such as `nats consumer info --json -- order-jobs resize-worker`, so a name that starts with `-` is never read as an option. A name with a control character, such as a line break, gets no command, only a note. The commands name neither the account nor the system, so a line below each says which to use a nats context for, such as "Run with a nats context for account APP on example-global."; without a system in the address it names the default system.
- **No write actions.** For an asset with a leader, "Step down leader" shows the `nats stream cluster step-down` or `nats consumer cluster step-down` command and copies it. nats-lens runs nothing and changes nothing in NATS.
- **Keyboard and screen readers.** Tab reaches every cell; each cell's name says the asset (a consumer with its stream, "resize-worker of order-jobs"), the server, the state, what differs, its text and how it changed. The filters are toggle buttons. Close, or removing the selection from the address, closes the panel.
- **Phone width.** The asset column stays in place while the server columns scroll sideways inside the matrix's box; the page itself never scrolls sideways. A selected cell scrolls into view, and the panel follows the matrix.
- **The API.** `GetPlacement` takes a range, a system and a filter. It returns the servers as `ListServers` lists them for the range, the streams that pass the filter or have consumers that do, each with its durable consumers that pass, and the counts of every filter. Each asset carries its configured replicas, its leader and when the leader took the lead, its cells, how many configured replicas no server names, whether its servers disagree, whether it passes the filter, and a stream's messages. Each cell carries the server, the state, the creation time and configuration hash it reports, the lag its leader reports, its positions, the settings on which it differs and the other replicas it names. A cell's state matches the asset's placement square in `ListStreams`, and its report matches the server's view in `GetStream` or `GetConsumer`.

## Scenarios

### Find every asset the servers disagree about

- **Surfaces:** `ui`
- **Given** the copy of `resize-worker` on `order-jobs` on `nats-0` has another creation time than on `nats-1` and `nats-2` (the demo's `example-prod`)
- **When** I open Placement and choose "Servers disagree"
- **Then** `order-jobs` is listed, muted, with `resize-worker` below it, and the `nats-0` cell of `resize-worker` reads "created" with its date and is drawn as disagreeing

### Check one replica by hand

- **Surfaces:** `ui`
- **Given** the matrix shows `resize-worker` disagreeing on `nats-0`
- **When** I select that cell
- **Then** the side panel shows each server's creation time, that the delivered and acknowledgement floor positions are equal on all three servers, and `nats consumer info --json -- order-jobs resize-worker | jq .created` with a Copy button

### See which replicas changed since yesterday

- **Surfaces:** `ui`
- **Given** `nats-us-4` of `example-global` stopped answering this morning and holds a replica of `shipping-updates`
- **When** I choose "this time yesterday" in "Compare with"
- **Then** the `nats-us-4` cells of `shipping-updates` and its consumers show offline with a dot, and selecting one says it was in sync at that time yesterday

### Keep the comparison while a live range moves on

- **Surfaces:** `ui`
- **Given** Placement on a live range compares with "this time yesterday" and says "1 cell differs"
- **When** the clock moves to the next minute and that minute's matrix of yesterday is still loading
- **Then** the line still says how many cells differ and the dots stay

### Drop the comparison on a change of system or range

- **Surfaces:** `ui`
- **Given** Placement of `example-prod` compares with "this time yesterday" and says "1 cell differs"
- **When** I switch to `example-global` and choose "this time yesterday" again, or change the page's range to a fixed hour, and the new moment's matrix is still loading
- **Then** the line says "Loading the matrix of …" with the new moment, and no cell is marked from the earlier comparison

### Learn that the comparison could not load

- **Surfaces:** `ui`
- **Given** a filter is chosen, so the page reads the unfiltered matrix as well, and that call fails
- **When** I choose "this time yesterday"
- **Then** the line says "Could not load the matrix to compare with:" and the error, and no cell is marked

### Know which system a command is for

- **Surfaces:** `ui`
- **Given** the address names no system, and the default system is `example-prod`
- **When** I select a cell
- **Then** the line below each command says "Run with a nats context for account APP on example-prod."

### Find a replica that is missing

- **Surfaces:** `ui`
- **Given** `nats-us-3` of `example-global` came back from an upgrade without its replica of `return-requests`
- **When** I choose "Missing somewhere"
- **Then** `return-requests` and `refund-issuer` are listed with the columns of `nats-us-3`, `nats-us-6` and `nats-us-8` only, and the `nats-us-3` cells read "reports nothing"

### Read the matrix from the API

- **Surfaces:** `api`
- **Given** `natslens run` serves its API
- **When** a client calls `GetPlacement` with a range and the filter `PLACEMENT_FILTER_REPLICA_BEHIND`
- **Then** it receives the servers, the streams with a replica behind (and those whose consumers have one), each asset's cells on each server, and the count of every filter

## Not supported / out of scope

- **No moving of assets.** nats-lens does not change placement, remove peers or step leaders down; it shows the commands only.
- **No placement for core NATS subscriptions.** Only JetStream assets have a placement.
- **Ephemeral and ordered consumers are not in the matrix**, nor consumers the inventory expects that no server reports; the Consumers list shows them.
- **How long a state has lasted** is known only for the leader (its tenure) and for a copy with another creation time; for any other state, compare with an earlier time. nats-lens does not search back for when a state began.
- **No playbook link.** "Step down leader" copies the command; nats-lens has no playbook to link. Site-specific advice belongs in a remediation file ([checks.md](checks.md)).
- **An offline replica that sends no report is missing from the asset's page.** The matrix shows such a replica offline, from the leader's list; the server list of the stream's or consumer's page (and `GetStream`, `GetConsumer`) lists only the servers that report the asset, so the replica is not there.
- **Two servers can show as leader on the asset's page.** When two servers each claim to lead an asset, the matrix names one leader and draws the other as disagreeing; the server list of the stream's or consumer's page shows each server's own claim, so both read as leader.
- **One server's own view by hand.** The `nats` command shows the asset as its leader reports it, not as the selected server does.
