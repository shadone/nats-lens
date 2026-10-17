# Next steps after phase 1

Phase 1 is complete on `develop` (see `phase-1.md`). This note is the starting point for the next session.

## State

- Every phase 1 task, the backend and UI clean-ups and the closing round are merged and pushed. All Go tests and the web tests pass; the feature-docs audit has 0 errors.
- The owner confirmed the proposed decisions listed in `docs/design.md` under "Owner review of proposed decisions, 2026-10-17".

## Next work, most valuable first

1. **Short-lived connections as a signal.** The owner's note on churn collapse: very short-lived connections likely point to a problem. Proposal to discuss first: keep folding them for storage, but stop treating `client.churn` as routine when the rate is high or new for a client; add a check (or extend `client-004`) that fires when a client opens and closes connections within seconds repeatedly, with the client names and hosts in the detail.
2. **One conformance table for the store and the demo.** The demo cannot show some states the store produces: placement DISAGREES/UNSPECIFIED/MISSING on streams, an unset delivery rate, Theil-Sen growth. Drive both from one shared test table.
3. **Fewer requests per page.** The Overview makes about 27 calls per load and refetches them every 30 s. Ideas: a light header-status RPC, a multi-metric `GetTimeSeries`, a cached `ListSystems` strip, `Connections` without the full server list.
4. **Phase 2 pages from the design canvas:** Placement, Replay, Topology, Accounts and auth, Capacity, Subjects (observable data only), Micro services, Applications. Each has a planned doc in `docs/features/`.
5. **Small leftovers:** an Events page with the account scope; tile links to their pages; cause-card evidence tables; drag to zoom.

## How the work was run

Each task: a fresh implementer in its own worktree, then a fresh reviewer, then fix rounds with their own reviewer, then a merge with the tests run before the merge commit. Decisions an agent makes are written as "proposed, not yet confirmed" until the owner confirms them.
