# Chart interval action verification

Task: `SPpdDvTy7GOtU4ECxR9HS`. Base: `c3c8b188`.

## Scope and proposed policy

Shared storage-byte plots expose horizontal drag selection and a keyboard/UTC-picker alternative. Selection opens one explicit dialog outside chart cells; it does not mutate the page or navigate. Set time range pushes exact fixed bounds, clearing obsolete endpoint pins. Investigate change opens the existing system/account-scoped storage contributors page. This is a time selection, not evidence of causation or a resource-specific comparison filter.

Proposed, not yet confirmed: automatic eligibility is an explicit allowlist of storage-byte metric identities, a four-pixel horizontal drag threshold, and a 500 ms suppression window for the generated post-drag click. New pointer intent clears that suppression, including modified link activation. Vertical touch gestures remain scroll gestures. Explicit opt-in can expose generic selection without a storage action.

The global dialog survives chart replacement during refresh. Both pointer and keyboard selections retain their original frame. Route/system/filter changes invalidate it. Shared Dialog owns focus, Escape and scroll locking. Existing inspection/copy controls remain separate. Exact milliseconds now survive shared range URL serialization; whole-second URLs keep their prior form.

## Runtime checkpoints and controls

- `c5467409`: initial runtime RED, 9 failures / 15 passes. Missing interaction and precision loss reach assertions.
- `36c27794`: core GREEN, 33 focused passes and typecheck.
- `841d38ac`: boundary runtime RED, 2 failures / 11 passes: modified click swallowed after cancellation; a keyboard interval moved with a refreshed chart frame.
- `f4461b76`: boundary GREEN, 38 focused passes and typecheck.
- Independent action removal leaves the API/types intact but disables opening: the expected dialog assertion fails at runtime.
- Independent precision removal restores second flooring: the millisecond URL assertion fails at runtime.
- Independent modified-click removal restores stale suppression: native click-cancellation assertion fails at runtime.
- Restored focused interaction, inspection, helper and range suites pass. Private receipts: `/private/tmp/nats-chart-range-remove-{actions,precision,modified-click,restored}.log`.

Pointer fixture `isPrimary` and unsupported Testing Library cancel helper were corrected during development; those setup failures are not regression proof. An early removal runner rejected a valid missing-element runtime failure because it searched only for the word AssertionError; the corrected runner records actual test failures. A full-suite process accidentally overlapped that early mutation window and was stopped; `/private/tmp/nats-chart-range-full-web.log` is excluded. The final full-suite run starts only after byte-exact restoration.

## Validation commands

From `web/`, using an untracked task-owned Vite config whose cache is under `/private/tmp`:

```sh
npm test -- --config vite.chart-range.config.ts --configLoader runner --maxWorkers=2
npm run typecheck
npx vite build --config vite.chart-range.config.ts --configLoader runner
```

Feature documentation: capability, both index sections and adjacent Overview/Capacity/navigation/help docs. Audit command:

```sh
python3 /Users/denis/.codex/plugins/cache/ddenis/ddenis-core/0.4.1/skills/feature-docs/scripts/audit_feature_docs.py docs/features/ --map-refs require
```

No Go/API/proto/dependency or collector changes. No full Go suite is warranted for this UI-only change. Browser acceptance and fresh read-only review are recorded below after execution. The installed server and private sources remain untouched.
