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
