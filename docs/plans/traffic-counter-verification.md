# Comparable traffic counter verification

Base: `7656555b`. Scope: eight stored server/cluster incoming/outgoing message/byte rate metrics, their observation metadata and server investigation displays. This is a partial step of task `bXMlS0kOLTauMBDTEL280`, not global ranking or complete traffic coverage.

## Contract

Proposed, not yet confirmed: exact increases admit only consecutive stored-server-ID report pairs wholly inside the selected half-open interval, with nonnegative known counters, no decrease, positive elapsed time and a gap at most three collection intervals (minimum one second). Integer arithmetic preserves totals beyond 64-bit and JavaScript safe-integer range. Comparison counts, distinct contributing IDs, skipped returned rows, range-start crossings and collection timestamp envelope explain the evidence. No continuous coverage, missing-report count, process-lifetime reconstruction or rate integration is claimed. Resource sequence rates remain unsupported. Demo curves have no backing counters and omit this metadata.

Existing ordinary server rates now reject backwards timestamps plus decreasing counters; previously a negative delta divided by negative elapsed time could fabricate a positive rate. Other native rate behavior and opt-out summary work remain unchanged. Curve and summary share the existing read transaction and two metric reads. SQL fragments come from the compile-time catalogue; range/entity values remain bound parameters. No dependency, migration or check-catalog changes.

## Runtime evidence

- Native runtime RED checkpoint `50f3ecbd`: exact totals/scopes/boundaries/reset/gap cases fail without metadata; backwards-time control reproduces fabricated positive rate. Native GREEN `ed64bcaf`.
- UI runtime RED `aabe8757`: exact total/coverage/unknown-vs-zero/page assertions fail before implementation. GREEN `469185bd`.
- Independent overlays remove counter-summary implementation, restore the original ordinary rate function, and remove the observation read transaction. Each reaches its intended runtime assertion: absent comparison data, fabricated rate 1, and concurrent counter total 10000 instead of 10. Restored native traffic tests pass. Logs: `/private/tmp/nats-traffic-removal/`.
- Removing the UI renderer reaches three runtime failures; restoring it passes all seven focused component/page tests. Native JSON decoding preserves exact decimal values and timestamp representation. Logs: `/private/tmp/nats-traffic-ui-removal.log`, `nats-traffic-ui-restored.log`, `nats-traffic-ui-final.log`.
- Sum across two IDs reaches `18446744073709551614`; negative/NULL/decreasing counters, 90/91-second gaps, reset/new ID, start-boundary crossing, known zero, opt-out and concurrent append are covered.
- Frozen live HTTP and offline handler parity includes server and cluster traffic with exact values above 2^53. Demo tests assert that synthetic curves do not fabricate cumulative-counter evidence.

## Executed checks

All Go commands use `GOCACHE=/private/tmp/nats-storage-go-cache GOPROXY=off`. Disposable HTTP fixtures require approved loopback access.

```sh
go test ./internal/api/storeprov ./internal/api/fake ./internal/api -run 'TestTraffic|TestSeriesObservation|TestDeliveredRate(SkipsGaps|RestartsOnLeaderMoveAndRecreate)$|TestDemoTraffic|TestBatch(EqualsSingleCallsInOrder|ExplicitRange|SharesOneRange)$' -count=1 -timeout=5m
TZ=Pacific/Honolulu go test -race ./internal/api/storeprov ./internal/api/fake ./internal/offline -run 'TestTraffic|TestSeriesObservation|TestDemoTraffic|TestOfflineLiveParity$' -count=1 -timeout=5m
go vet ./internal/api/storeprov ./internal/api/fake ./internal/offline
make -o web/node_modules/.package-lock.json proto
npm --prefix web test -- --configLoader runner --maxWorkers=2
npx --prefix web tsc --noEmit -p web/tsconfig.json
cd web && npx vite build --configLoader runner --outDir /private/tmp/nats-traffic-ui
```

Focused native packages pass; non-UTC race packages pass. Full web: **202 files, 1908 passed, 24 existing skips**. Final focused web after singular-label cleanup: seven pass. Types, production build, proto lint/format/generation, scoped vet and feature-doc audit (46 capabilities) pass. Existing >500kB production chunk warning remains. No full Go/check-engine/broad store-provider suite or performance benchmark ran.

Setup/control-harness failures are excluded from proof: an initial new epoch fixture omitted explicit columns; an initial demo test used the wrong constructor signature; one sandboxed HTTP test could not bind; one removal regex selected no tests; the UI control wrapper expected a particular error class despite reaching the intended runtime query failures. Those were corrected, and the stated assertions and restored checks ran successfully. No compilation failure is counted as runtime RED.

## Browser acceptance

A disposable native store with three cumulative-counter reports serves the production UI and real API on loopback. Desktop shows exact incoming `9,007,199,254,741,000`, outgoing measured zero, two comparable pairs and one stored ID. At actual 360px viewport the document stays 360px, counter notes/details stay within 328px and the wide comparison table scrolls inside its region. Native pointer activation opens comparison evidence; fixed event/coverage links retain source and endpoints. Fonts load. This is synthetic native API acceptance, not private deployment validation. Browser ran before a singular/plural label-only cleanup; final focused tests/types cover that cleanup.

T3 resize initially returned stale 1280px metadata; a corrected/repeated resize and direct innerWidth observation confirmed actual 360px before phone claims. An ambiguous selector was retried with a unique locator. No alternate browser was needed. No live 18900 or existing database was read.

## Remaining acceptance

Complete interval volumes cannot be guaranteed from incomplete reports. Resource sequence-volume semantics, global metric contributor ranking and richer historical attribution remain open. No client causation, contents, profiling or cross-RPC pin is added.
