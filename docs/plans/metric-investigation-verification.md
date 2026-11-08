# Server metric investigation verification

Base: 51ae76f8881ef5bdaa1e1c8276f84fe803d6acec. This is a server-family UI slice of bXMlS0kOLTauMBDTEL280, not whole-task acceptance. Policies remain proposed, not yet confirmed. No backend, dependency, proto or migration change.

## Runtime checkpoints

- 11b5aaf5: ten runtime failures for canonical destinations and native comparison reads; 1229a0cc restored nineteen focused tests. An initial wrong test import was corrected before the valid RED checkpoint.
- dba222d7: three page runtime failures; 1ba7ba23 restored fifteen focused page/comparison/Timeline tests and types. A setup script syntax error was corrected; its output is not regression evidence.
- 5957b411: three runtime failures for loading entity navigation, mixed fleet/entity scope and response identity.
- c4410769: one runtime failure for narrowed metric interval navigation.
- e578ca67: copied metric-route identity failed at runtime before the implementation.
- Five independent removal controls (page, MetricBox navigation, interval action, copied context, API response identity) each failed at runtime; production files were restored byte-for-byte in finally blocks. Restored six-suite selection passed 32 tests. Logs: /private/tmp/nats-metric-removal/{page,navigation,interval,context,identity}.log and results.json; restored /private/tmp/nats-metric-restored.log. A temporary parse error in navigation implementation was corrected before GREEN and is not counted as RED.

## Validation

Commands from web/: `npm test -- --config vite.metric.config.ts --configLoader runner --maxWorkers=2`; `npm run typecheck`; `npx vite build --config vite.metric.config.ts --configLoader runner`. The temporary config only redirects Vite cache outside borrowed dependencies.

- Full web: 190 files, 1,841 passed, 24 existing skips, 76.26 seconds. Log /private/tmp/nats-metric-pages-full-web.log.
- Typecheck and production build pass. Existing 500 kB warning remains visible. Build log /private/tmp/nats-metric-build.log.
- Feature audit clean: 46 capabilities, both index sections consistent.
- Query test proves at most 26 histories (two fleet plus twelve two-direction servers), same fixed bounds for metadata and histories, selected-name filtering and effective page clamping; native summaries preserve unknown latest values. Identity guard rejects another server's returned history. The legacy full server inventory is not covered by this history bound.
- No broad Go suite or new Go correctness claim. The fixed-demo binary was built using the existing private cache. Initial sandbox listener binding was denied; approved loopback retry served only synthetic fixtures.

## Native browser

T3 collaborative browser worked for this pass; no fallback browser was needed. Owned fixed demo: 127.0.0.1:18686, no private service access.

- Clicked Overview CPU title to /metrics/cpu and read three native server rows with raw peaks, actual timestamps and known counts, without API errors.
- At 360px the document remained 360px wide. The comparison table scrolled locally (328px region, 729px content), and an expanded server chart remained 270px wide. Findings opened a 328x768 fixed-interval dialog, body scroll locked, no horizontal document overflow; Escape dismissed it. Native dialog inspection uses dialog[open], not a role attribute selector.
- Fixed selected-server message page retained APP/system/time/server in Events links, showed both directions and separate native summaries. The findings selection kept exact displayed endpoints.
- Native Shift+ArrowRight and Enter selected 13:45:00–13:45:57; Open metric details used those exact fixed endpoints, system/account, with no live range parameter. Browser history.back restored the previous 13:00–14:30 range. An earlier datetime input typing attempt did not change its value and is excluded from narrowed-range proof.
- Memory at1280, connections at761 and byte traffic at360 all returned three native rows, no alerts and no document overflow. Phone/table containment and computed geometry were inspected in the actual production-component app. This is geometry/interaction acceptance, not a screenshot-based visual certification or physical-touch test.

## Remaining whole-task work

Resource-specific stream/consumer detail families; exact traffic volumes where supported; global contributor ranking; full primary-metric destination inventory; richer process-identity attribution. Current pages disclose all-account server metrics, per-page sorting, sample-weighted means, incomplete capture, grouped server names and absence of a universal pin. Storage/detection specialists are reused; server-origin storage navigation explicitly remains system-wide.
