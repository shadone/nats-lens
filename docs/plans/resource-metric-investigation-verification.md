# Resource metric investigation verification

Base: f7f202e89eb33f976805cabce329a0eaacc52d96. Board task bXMlS0kOLTauMBDTEL280 stays in progress. Proposed, not yet confirmed: twelve-resource pages using existing stream/consumer contexts and at most twelve histories per batch. No Go, proto, dependency or migration changes.

## Runtime checkpoints

- f92b5792: thirteen executed RED tests for typed resource destinations and bounded native reads; 5934b7da GREEN.
- 4d1f8d5a: five page/navigation failures; 27591335 GREEN (18 focused tests); one test fixture omitted required stepMs and was corrected before types passed, not counted as runtime proof.
- a103578c: four runtime failures for cross-resource placeholders, requested loading identity, copied context and resource-account evidence links; 0fb2d511 GREEN.
- 9c782b18: two runtime identity failures, seven passing adjacent controls; b3166e01 GREEN.
- First full web run: one failure, 1,867 passed, 24 skipped. The storage-change action incorrectly chose the newly recognized resource metric destination. f4b69ae4 checkpoint adds the distinct metric-action control; b307c587 fixes dispatch.
- f8802107: chart-title range test fails with NaN instead of1000; ba46fe17 GREEN. A seven-day chart must not inherit a one-hour page range.
- Nine independent removal controls fail at runtime: page, registry, comparison, history-scope, copied context, evidence account, typed identity, interval action and chart range. Each file is restored byte-for-byte in finally. Restored seven-suite selection: 46 tests pass. Logs /private/tmp/nats-resource-removal/{page,registry,comparison,history-scope,context,account,typed-identity,interval,chart-range}.log, results.json; /private/tmp/nats-resource-restored.log.

## Validation

From web/: `npm test -- --config vite.metric.config.ts --configLoader runner --maxWorkers=2`, `npm run typecheck`, `npx vite build --config vite.metric.config.ts --configLoader runner`. The temporary config only redirects cache outside borrowed dependencies.

Pre-review full web: 195 files, 1,870 tests passed,24 existing skips,75.39s (/private/tmp/nats-resource-final-full-web.log). Types and production build pass; existing500kB warning retained. Feature audit clean46 capabilities. No coverage percentage is claimed: no coverage provider was installed. No broad Go/check-engine suite rerun.

API tests verify one bounded summary page, selected-key-only histories, list-bound range continuation despite advancing clock, exact >2^53 collection IDs, no inventory dependency for explicit historical selections, account/stream and ephemeral scope, invalid key/typed-field mismatches, per-query errors, missing clock and oversized-page rejection before enrichment. Current metadata and history reads have separate consistency boundaries; no universal pin or global metric rank is claimed.

## Actual fixed-demo browser

T3 collaborative browser, task-owned generic fixed demos only (initial18688, final18690), real fonts; no private18900 access.

- Stream list resource diagnostics -> order-jobs detail -> Messages title -> selected metric page, preserving fixed bounds/system/resource. Seven-day Bytes title on final binary uses Oct10–17 while containing page is one hour. An initial guessed APP/orders fixture returned404; actual demo identities were then discovered from the list. That setup miss is excluded from success.
- Consumer detail Pending/Ack pending/Redelivered titles retain account/stream/name. Pending title opens resize-worker native history. Keyboard Shift+Right then Enter selects14:00:00–14:00:38; metric action preserves those exact endpoints, identity/system/account. Events link retains the consumer predicate; findings shows retained consumer-009 evidence in a328x768 fixed dialog with body scroll locked. Escape dismisses; history.back restores prior bounds.
- At360px the document remains360px, wide table scroll is local, selected history width270px. Ingestion at761, replica bytes at1280, delivery at360, redelivery at761 and ack-pending at1280 each return12 rows without alerts or document overflow.
- Consumer pagination advances Page1→2 with12 rows/55 matches and unchanged fixed bounds/list collection, while the first resource changes. Account/stream native filters reset traversal.
- Final storage interval menu has separate Open metric details and Investigate change actions. The latter opens endpoint-pinned Capacity contributors with preserved system/account/range. Direct-click automation initially timed out because hover-only controls were hidden (pointer-events none); hovering the chart revealed them and native click passed. This is not reported as a fixed product hit-target bug. One generic input selector was ambiguous; narrowed label-specific selectors were used.
- Browser evidence is actual-app geometry, semantic content, pointer/keyboard and URL behavior. No screenshot-based visual certification, physical touch or native clipboard certification is claimed.

## Remaining board acceptance

Full chart-family inventory including aggregate server/asset/account/meta destinations; traffic volumes where supported; global contributor ranking and richer historical identity attribution. Resource local cursor state is not shared in URLs. API observation sample counts do not establish capture completeness. Hashes/cross-query pins/new collection are unchanged.

## Fresh R1 corrections

Review596e47e9 found missing automatic live refresh and a consumer stream filter lost across metric tabs. Checkpoint7dec1d2a executes three runtime failures (live selected resource, live first list page, consumer stream link) with five controls passing. d1b29cd7 adds30-second live refresh only for selected resources/first pages; continuation contexts and fixed bounds remain unchanged. Consumer metric links retain the typed stream filter. UI explicitly explains pinned continuation and Refresh list.

Focused real-query timer tests advance the source clock and verify new response bounds; first-page lists refresh; continuation and fixed queries do not.17 focused tests, types and production build pass. Removing each correction independently reproduces its runtime failures, restored17pass. Logs /private/tmp/nats-resource-r1-{red,green,restored,types,build}.log and /private/tmp/nats-resource-r1-removal/{refresh,filter}.log. No second full-web or native browser timing loop was run for this small review delta; the1,870 full-web and earlier browser receipts apply to the pre-review feature, not an invented new full-suite count. The corrected production build is verified, and all prior source files remain intact.
