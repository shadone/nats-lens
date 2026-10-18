# Next steps after phase 1

Phase 1 is complete on `develop` (see `phase-1.md`). This note is the starting point for the next session.

## State on 2026-10-18, evening

- Items 1 to 3 below are done and merged on `develop`. `develop` is **not pushed**; the owner pushes when ready. Go tests, web tests and the feature-docs audit pass on `develop`.
- **Placement page, open branch `feat/placement-page`** in the worktree `../nats-lens-placement` (head `3cb8213`). Built, then three review rounds; rounds 1 and 2 were fixed and reviewed, and the fixes of round 2 (`1579dc9`, `4459060`, `3cb8213`) still need a fresh review. The review of them was stopped when the session paused. Next: a fresh reviewer for `b9c0e4a..3cb8213`, fixes if any, then merge with the tests run before the merge commit. The branch also edits items 4 and 5 below.
- Phase 1 decisions: the owner confirmed those listed in `docs/design.md` under "Owner review of proposed decisions, 2026-10-17".
- The design canvas the phase 2 pages come from is the claude.ai artifact <https://claude.ai/artifact/Lz4Tsej5k1XHDdfV1kbLJD> (one board per page, such as `Placement.dc.html`).

## Next work, most valuable first

1. **Short-lived connections as a signal.** Done: `client-005` fires for a name with 20 or more short-lived connections in 10 minutes; folding and the routine status of `client.churn` stay (see `docs/features/client-checks.md`).
2. **One conformance table for the store and the demo.** Done: `apitest.Conformance` runs against a store fixture and the demo; `example-global` gained disagreeing, missing and leaderless replicas, a paused consumer and one without a rate, and changed findings, and the demo's growth is the store's Theil-Sen slope (see `docs/features/demo-mode.md`). Left: a server without JetStream, and the stopped or starting collection states, are not in the demo.
3. **Fewer requests per page** (done). The Overview made about 27 calls per load and refetched them every 30 s. Done: `GetTimeSeriesBatch` (the Overview from 25 calls on load to 10, the Servers page from 10 to 5); `Connections` without the full server list, as `ListConnections` names the range's servers, and the Consumers stream filter read once instead of polled (Connections from 6 calls on load and 4 per cycle to 5 and 3, Consumers from 6 and 4 to 6 and 3). Then `GetShellStatus`, one status call per system for the whole shell in place of `ListSystems` and a header `GetOverview`, with every system's badge kept for one collect interval in the server; measured on `natslens demo`, calls on load and per 30-second cycle went from 10 and 9 to 9 and 8 on the Overview, Servers from 5 and 4 to 4 and 3, Streams from 4 and 3 to 3 and 2, Consumers from 6 and 3 to 5 and 2, Connections from 5 and 3 to 4 and 2, Events from 4 and 3 to 3 and 2, Findings from 5 and 3 to 4 and 2, Fleet unchanged at 3 and 2.
4. **Phase 2 pages from the design canvas:** Placement, Replay, Topology, Accounts and auth, Capacity, Subjects (observable data only), Micro services, Applications. Each has a planned doc in `docs/features/`.
5. **Small leftovers:** a cached `ListSystems` strip for the Fleet page; an Events page with the account scope; tile links to their pages; cause-card evidence tables; drag to zoom; `client-005` and `server-011` keep their hysteresis only when the check ran in the previous epoch, so match the last epoch the check ran successfully instead; the demo uses several 30 s epoch grids (freshness at :26/:56, series and growth at :00/:30, story times at :05/:35), so share one anchor; for a past range the demo still reads the newest state in stream and consumer details and placement and in `GetServer` (endpoints, processes, routes), and lists servers for a range without an epoch, where the store lists none; a server down at a past epoch shows its uptime as of that epoch, not its last answer; a range before 2026-10-02 08:12 still lists servers with a negative uptime, and `ListAccounts` lists `APP` for a range before the history, where the store lists none; `ListConnections.servers` in the demo lists every server whatever the range; GetShellStatus: serve a kept error badge at once instead of waiting 500 ms on every call, and keep error badges briefly so a broken system is not read and logged on every poll; a thrown shell-status error is hidden behind an earlier in-band one (`useShellQuery` in queries.ts), so the tooltip can name the wrong code; multiple-systems.md omits that a failed badge read also waits; rename the `review_shell` tests after the behaviour they check.

## How the work was run

Each task: a fresh implementer in its own worktree, then a fresh reviewer, then fix rounds with their own reviewer, then a merge with the tests run before the merge commit. Decisions an agent makes are written as "proposed, not yet confirmed" until the owner confirms them.

## Proposed decisions waiting for the owner (2026-10-18)

Made in the session that built them, not confirmed. Each is also written in `docs/design.md` or the feature doc as "proposed, not yet confirmed".

- `client-005`: fires at 20 short-lived connections of one name in 10 minutes, warning severity; once firing it holds down to `resolve_ratio` 0.8 of the threshold.
- `GetTimeSeriesBatch`: at most 32 series per call, read four at a time; each series gets 30 s and the call 40 s, which fits under the web client's 45 s; the server's write timeout is 55 s. The Overview and Servers pages no longer share cached series.
- Connections names its servers from `ListConnections`: every server seen in the range, also servers without connections. The Consumers stream filter is not polled.
- `GetShellStatus`: the page system's part gets 10 s; a failure keeps the system list and returns `error_code` and `error`; badges are kept one collect interval and refreshed in the background; a first badge waits at most 500 ms and is otherwise `pending`. The Fleet badge can trail the Fleet cards by up to two intervals.
- The demo: `stock-poller` fires `client-005`; the demo choices listed in the Demo section of `docs/design.md`.
- Placement (on the open branch): the odd copy is the value most servers report, the leader's on a tie; only durable consumers; "servers disagree" also counts a differing creation time; open questions in the review: KV and object stores sort before plain streams, and `example-global`'s "All" view shows 24 columns.

## Real data seen on 2026-10-18

The owner runs a collector against two real clusters outside the repository. On one of them a Kubernetes controller opened and closed a connection about 12 times a minute all day, all of it folded into routine `client.churn` events; that is what `client-005` now flags. Short-lived CronJob connections that last minutes are not folded, as intended.
