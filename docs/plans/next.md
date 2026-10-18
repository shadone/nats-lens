# Next steps after phase 1

Phase 1 is complete on `develop` (see `phase-1.md`). This note is the starting point for the next session.

## State

- Every phase 1 task, the backend and UI clean-ups and the closing round are merged and pushed. All Go tests and the web tests pass; the feature-docs audit has 0 errors.
- The owner confirmed the proposed decisions listed in `docs/design.md` under "Owner review of proposed decisions, 2026-10-17".

## Next work, most valuable first

1. **Short-lived connections as a signal.** Done: `client-005` fires for a name with 20 or more short-lived connections in 10 minutes; folding and the routine status of `client.churn` stay (see `docs/features/client-checks.md`).
2. **One conformance table for the store and the demo.** The demo cannot show some states the store produces: placement DISAGREES/UNSPECIFIED/MISSING on streams, an unset delivery rate, Theil-Sen growth. Drive both from one shared test table.
3. **Fewer requests per page.** The Overview made about 27 calls per load and refetched them every 30 s. Done: `GetTimeSeriesBatch` (the Overview from 25 calls on load to 10, the Servers page from 10 to 5); `Connections` without the full server list, as `ListConnections` names the range's servers, and the Consumers stream filter read once instead of polled (Connections from 6 calls on load and 4 per cycle to 5 and 3, Consumers from 6 and 4 to 6 and 3). Left: a light header-status RPC for the shell status, and a cached `ListSystems` strip.
4. **Phase 2 pages from the design canvas:** Placement, Replay, Topology, Accounts and auth, Capacity, Subjects (observable data only), Micro services, Applications. Each has a planned doc in `docs/features/`.
5. **Small leftovers:** an Events page with the account scope; tile links to their pages; cause-card evidence tables; drag to zoom; `client-005` and `server-011` keep their hysteresis only when the check ran in the previous epoch, so match the last epoch the check ran successfully instead; the Overview sends `GetOverview` twice per cycle (header and page).

## How the work was run

Each task: a fresh implementer in its own worktree, then a fresh reviewer, then fix rounds with their own reviewer, then a merge with the tests run before the merge commit. Decisions an agent makes are written as "proposed, not yet confirmed" until the owner confirms them.
