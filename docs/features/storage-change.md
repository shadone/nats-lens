# Storage-change investigations

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** shipped
- **Related:** [capacity.md](capacity.md), [agent-investigations.md](agent-investigations.md), [offline-datasets.md](offline-datasets.md), [data-history.md](data-history.md)

## What it does

A storage comparison shows which retained resource measurements increased or decreased between two selected moments. Capacity opens a dedicated evidence page; agents read the same comparison through the existing read-only investigation commands. The comparison separates logical bytes, observed copies, configured-replica estimates and server-reported usage.

## Behavior and rules

- **Collection windows.** Each moment resolves to the latest collection completed at or before it. The response retains requested times, actual collection windows, ages, collection IDs and coverage reasons. It never uses future replies. An unavailable pinned collection fails explicitly instead of selecting another one. Missing or mismatched detailed stream counts leave complete totals unknown even when JSZ answered. A captured source's existing one-microsecond exclusive-end adjustment stays disclosed; its observed source clock does not move.
- **Comparable evidence.** Change requires distinct, available collection windows no older than three collection intervals. Stale or identical bookends retain endpoint values but do not establish a change.
- **Pins and scope.** The evidence page freezes live presets against the loaded source clock and records available returned collection pins plus the resolved system in its URL. Explicit range changes discard old pins. Sharing, pagination and copied prompts preserve the system, account, requested interval and pins. Server usage always covers the whole system; an account-scoped comparison cannot reconcile that usage to its filtered resources.
- **Both endpoints.** The comparison includes resources observed at either endpoint, including ordinary streams, KV buckets and object stores. IDs include the reported creation identity and storage type. A replacement has its own row. Unknown creation identity retains measured endpoint values but cannot establish an incarnation's contribution.
- **Observed absence.** A matching exact-account deletion event after A can qualify a known zero at B. A later reported incarnation created between the collection windows can qualify retirement of the earlier incarnation. Otherwise an absent report remains unknown. A creation time after A can qualify zero at A; an impossible creation time beyond its collection cannot establish a contribution.
- **Separate quantities.** Logical bytes describe one copy. Observed-copy bytes require usable reports for every configured copy and do not describe physical disk allocation. Configured-replica bytes multiply logical bytes by the configured copy count and remain an estimate. File and memory storage have separate totals; moving a known incarnation between them produces separate signed contributions.
- **Server identities.** Named server rows retain the process IDs at both endpoints. A restart can change a gauge without being a counter reset. Simultaneous processes with the same name leave the named store unknown. A server reporting JetStream disabled contributes known zero usage; a missing reply does not.
- **Ranking and totals.** Resources rank by absolute logical change by default, with signed ascending/descending orders and type filters available before paging. Unknown contributions follow. Filters change displayed rows, while totals cover all types. Named server stores follow resources. Totals include all comparison rows. Known and off-page contributions carry unknown-row counts; incomplete totals and residuals stay unavailable. The residual is server usage change minus observed-copy change, not physical accounting or causal attribution.
- **Bounded evidence.** A page holds at most 100 rows and 512 KiB. The comparison rejects excessive ranking or metadata rather than returning partial success. Continuations bind scope, pins and evidence; changed or removed evidence requires restarting. The page never follows every continuation automatically. Events and configuration differences are supporting observations, not proven causes.
- **Local datasets.** Offline commands use the same handlers, arithmetic and bounded reads. Responses omit remote UI links because no UI serves the dataset. The same source and exact context retrieve local evidence again.
- **Policy.** Endpoint selection, identity qualifications, ranking and budgets are proposed, not yet confirmed as owner policy.

## Scenarios

### Attribute an increase without hiding a decrease

- **Surfaces:** `ui`, `api`, `cli`
- **Given** one stream grows, another has a recorded deletion, a replica count changes and a server fails to report
- **When** I compare the two moments
- **Then** the comparison retains positive and negative known contributions and distinguishes logical change from added copies
- **And** the missing server prevents a complete server total or residual instead of supplying zero

### Replay the same evidence

- **Surfaces:** `ui`, `api`, `cli`
- **Given** a comparison returns collection IDs and an explicit system
- **When** I share its evidence URL or reuse its context and continuation
- **Then** the read uses those retained collection windows
- **And** changed evidence or an unavailable pin fails explicitly without selecting a replacement

### Read a captured zero-duration collection

- **Surfaces:** `api`, `cli`
- **Given** a valid captured source's latest collection starts and finishes at the same instant
- **When** its existing derived exclusive query end is used for comparison
- **Then** the latest collection remains visible with the adjustment disclosed and its actual clock unchanged
- **And** no remote UI link is advertised for the local dataset

## Not supported / out of scope

- Physical disk accounting, message content, per-key or per-object measurements, causal diagnosis, or interpolation between replies.
- Resources observed only between the two endpoint collections, authoritative reconstruction across unknown incarnations, or lifecycle events whose account cannot be matched exactly.
- A universal cross-RPC snapshot, collection pins after retention removes their evidence, or permanent immutable results across database replacement.
- Chart-drag actions and automatic causal diagnosis. Capacity's measured-change table and interval charts are documented in [Capacity](capacity.md).
