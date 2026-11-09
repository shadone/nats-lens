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

## Final results

- Final full web: **184 files, 1,818 tests passed, 24 existing skips**, 73.70 s. Receipt: `/private/tmp/nats-chart-range-full-web-final.log`.
- Focused restored set: **42 tests passed**; types and production build pass. Existing bundle-size warning remains visible and unchanged in policy.
- Feature audit: **44 capability docs, clean**. No backend test suite was run for this UI-only delta.
- Fresh read-only review approved exact `59591438` against `c3c8b188`, with no concrete findings and no suite reruns.
- T3 Preview opened the owned demo but snapshot twice returned `Preview automation snapshot timed out after 15000ms.` Switched to the documented agent-browser fallback on an owned fixed demo at loopback port 18682.
- Native mouse drag and dialog bounds passed at **360/761/1280**, light/dark. All six layouts kept the URL unchanged and the dialog inside the viewport. Real-font screenshots: `/private/tmp/chart-range-{360,761,1280}-{light,dark}.png`.
- Native keyboard Home/Shift+arrows/Enter, Set time range, browser Back, picker and contributor navigation passed. `/private/tmp/nats-chart-range-browser.json` records exact selected and resulting URLs. Contributor finishing adds its existing endpoint pins after navigation.
- Overview storage reverse drag opens the dialog without following the stretched card link; Escape keeps the URL and native heading activation still opens Capacity. `/private/tmp/nats-chart-range-overview.json` records this. Raw plot clicks retain the pre-existing inspection-only behavior; the heading is the navigation target.
- The first browser action script used unsupported `:has-text` selector syntax after its six geometry assertions; that action attempt is excluded. Continued through semantic/native controls. A mistaken expectation that clicking the raw inspection plot should navigate is also excluded; the existing heading link was verified directly.
- Touch events/vertical-pan cancellation and the UTC-picker alternative are tested; physical touchscreen behavior is not certified. Refresh-frame, scope dismissal, account precedence and exact milliseconds are covered by component/URL tests, not a private-source browser claim.

Installed/private server 18900, NATS, databases, configuration and logs were not accessed. Temporary browser/server/dependency links are removed during integration cleanup. No push or deployment.
