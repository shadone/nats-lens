# Resource sequence comparison verification

Base: `6d239ae3e8d2b4929cdc5ebe96fe96f4e8fd1749`. Proposed, not yet confirmed: unknown or conflicting creation metadata stops sequence comparisons rather than treating identity as unchanged.

## Change and benefit

Histories and consumer summaries previously could rejoin an earlier leader/creation run after an observed interruption. Timestamp filtering could also remove the actual predecessor. Shared resource sequence SQL now preserves epoch adjacency and compares only known coherent creation evidence; consumer delivery additionally requires one unchanged leader. The summary selects the newest comparable run, never an older substitute. This improves correctness rather than promising lower SQL-engine cost.

Stream sequence advancement is not stored-message volume: pinned NATS v2.15.0 `server/memstore.go` SkipMsg/SkipMsgs advance sequences without stored messages. Consumer delivery sequences can advance on redelivery. API descriptions, resource page guidance, Overview notes and protobuf comments now state these limits. Field numbers, metric IDs, dependencies, migrations and the 91-check catalogue are unchanged. Positive synthetic fixtures now explicitly supply creation metadata; missing-metadata regressions remain separate.

## Executed receipts

Logs are private local verification artifacts, not repository inputs.

- `/private/tmp/nats-resource-sequence-red.log`: new runtime tests fail against old histories and summary.
- `/private/tmp/nats-resource-label-api-red.log` and `nats-resource-label-ui-red.log`: definition/UX regressions fail before copy corrections.
- `/private/tmp/nats-resource-sequence-proof/series-red.log`: old series SQL overlaid; both timestamp-adjacency cases fail at runtime, reporting a fabricated rate 6.6667.
- `/private/tmp/nats-resource-sequence-proof/summary-red.log`: old summary overlaid; leader/incarnation/unknown/conflicting runs falsely rejoin. Runtime assertions fail.
- `/private/tmp/nats-resource-sequence-proof/restored.log`: restored implementation passes all resource-sequence regressions (5.689s). Overlays never mutate tracked production files.
- `/private/tmp/nats-resource-sequence-final-race.log`: `TZ=Pacific/Honolulu go test -race ./internal/api ./internal/api/storeprov -run '^Test(ResourceRateDefinitions|ResourceSequence|DeliveredRate|BusiestConsumers|HistoricalRateRanking|MetricContributorsEvery)' -count=1 -timeout=5m` passes (API 1.746s; store provider 13.956s). Includes positive native contributor summaries, ranking read limits and adjacent existing rate contracts.
- `/private/tmp/nats-resource-label-ui-green.log`: resource guidance, resource page and Overview tests pass, 27 tests across three matched files. The extra BusiestConsumers filename filter matched no standalone test file and is not counted as a fourth suite.
- `/private/tmp/nats-resource-label-types.log`: TypeScript passes.
- `/private/tmp/nats-resource-sequence-proto.log`: buf lint/format/generate passes; only consumer field documentation changes in generated outputs.
- `/private/tmp/nats-resource-sequence-vet.log`: scoped API/store-provider vet passes.
- `/private/tmp/nats-resource-sequence-docs.log`: feature audit clean, 46 capabilities; both index sections updated.

All Go runs use `GOCACHE=/private/tmp/nats-storage-go-cache GOPROXY=off`; synthetic stores only. No private deployment, live 18900, database, configuration or logs were accessed. No visual layout/styles changed, so no new browser matrix is claimed. Full combined-tree verification is separate and must identify its exact revision.

## Limits

Entirely absent resource rows may bridge a short allowed elapsed gap. Sampled creation metadata is not a physical incarnation certificate. Stream fallback sequence selection remains a reported-sequence projection. Floating rates/statistics are not exact integer traffic totals. These limits are explicit; no new resource-volume total is fabricated.

## Acceptance fixture reconciliation

The frozen full gate at `87200340` passed static/proto/docs/all-package vet and the complete web suite (204 files, 1,914 passed, 24 existing skips). Its Go run reported three store-provider failures: the two positive/zero-rate conformance rows and `TestReviewOnCallStaleDelivery`. The uninterrupted run is retained under `/private/tmp/natslens-full-87200340-20261109`; it is not a passing full-suite receipt.

Focused isolation proved that all eight `fast`/`planner` reports in the positive fixture had NULL creation metadata. They now carry explicit creation timestamps supplied at the scenario call sites. The deliberately contradictory `settler` creation reports remain contradictory. The stale-delivery test also configured thirty seconds for historical reports five minutes apart. It now uses the fixture's five-minute cadence; newer empty collections still leave the historical reports inside the fifteen-minute window, so the current-presence assertions remain necessary. No assertions, production algorithms, gap limits or timeouts were weakened.

`TestConformanceRateFixtureHasExplicitIdentityEvidence` reached runtime RED with 0/8 known creation values (`/private/tmp/nats-rate-fixture-red.log`). After metadata correction, only the cadence mismatch remained RED (`nats-rate-fixture-cadence-red.log`). Both focused contracts then passed (`nats-rate-fixture-green.log`). Coverage distinguishes positive one-per-second, measured zero, out-of-policy cadence and unknown creation.

Independent overlays of the original fixture and original stale-delivery cadence each reproduce their runtime failures. Restored contracts pass (`/private/tmp/nats-rate-fixture-proof/{creation-red,cadence-red,restored}.log`). Production source is byte-identical to `87200340`; only test fixtures/regressions and this evidence record change. No frontend/capability behavior changes, so this test-only reconciliation does not add a feature-index entry. Focused checks ran with GOMAXPROCS=2 while the frozen full run continued; no uncontended-host performance claim is made. The original failed run must remain reported as failed even if corrected focused checks pass.

The exact two original HTTP conformance cases also pass under `TZ=Pacific/Honolulu`: `go test ./internal/api/storeprov -run '^TestStoreConformance/(a_consumer_delivering,_its_rate_set|a_consumer_that_delivered_nothing,_its_rate_zero)$' -count=1 -timeout=3m` (21.900s; `/private/tmp/nats-rate-fixture-http.log`). Disposable loopback permission was approved. This is a selected-case result, not a broad store-provider pass.

### Fixture provider ownership

A subsequent scoped vet identified that the new short-cadence control copied `Provider`, including its internal mutex-bearing pager. The control now constructs a separate provider with the same store/checks/clock and its explicit thirty-second interval. Its assertions are unchanged. Scoped store-provider vet passes (`/private/tmp/nats-rate-fixture-ownership-vet.log`), and the fixture contract passes (2.539s, `nats-rate-fixture-ownership-green.log`). Overlaying the original test restores the exact copylock diagnostic (`/private/tmp/nats-rate-fixture-ownership-proof/removed-vet.log`). This is static vet RED/GREEN, not a runtime behavioral-failure claim. Production remains unchanged.
