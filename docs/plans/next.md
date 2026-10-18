# Next steps after phase 1

Phase 1 is complete on `develop` (see `phase-1.md`). This note is the starting point for the next session.

## State

- Every phase 1 task, the backend and UI clean-ups and the closing round are merged and pushed. All Go tests and the web tests pass; the feature-docs audit has 0 errors.
- The owner confirmed the proposed decisions listed in `docs/design.md` under "Owner review of proposed decisions, 2026-10-17".

## Next work, most valuable first

1. **Short-lived connections as a signal.** Done: `client-005` fires for a name with 20 or more short-lived connections in 10 minutes; folding and the routine status of `client.churn` stay (see `docs/features/client-checks.md`).
2. **One conformance table for the store and the demo.** Done: `apitest.Conformance` runs against a store fixture and the demo; `example-global` gained disagreeing, missing and leaderless replicas, a paused consumer and one without a rate, and changed findings, and the demo's growth is the store's Theil-Sen slope (see `docs/features/demo-mode.md`). Left: a server without JetStream, and the stopped or starting collection states, are not in the demo.
3. **Fewer requests per page.** The Overview makes about 27 calls per load and refetches them every 30 s. Ideas: a light header-status RPC, a multi-metric `GetTimeSeries`, a cached `ListSystems` strip, `Connections` without the full server list.
4. **Phase 2 pages from the design canvas:** Placement, Replay, Topology, Accounts and auth, Capacity, Subjects (observable data only), Micro services, Applications. Each has a planned doc in `docs/features/`.
5. **Small leftovers:** an Events page with the account scope; tile links to their pages; cause-card evidence tables; drag to zoom; `client-005` and `server-011` keep their hysteresis only when the check ran in the previous epoch, so match the last epoch the check ran successfully instead; the Overview sends `GetOverview` twice per cycle (header and page).

## How the work was run

Each task: a fresh implementer in its own worktree, then a fresh reviewer, then fix rounds with their own reviewer, then a merge with the tests run before the merge commit. Decisions an agent makes are written as "proposed, not yet confirmed" until the owner confirms them.
