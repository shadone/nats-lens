# Offline investigation verification

This records the implementation candidate and its synthetic verification on 2026-11-08. Source syntax and mechanisms are proposed, not yet confirmed as owner policy. Fresh read-only review and integration belong to the parent task; this record does not claim their approval.

## Requirements and evidence

| Requirement | Implementation and evidence |
|---|---|
| One explicit source with consistent commands | `investigate` selects an endpoint, profile, bundle, or bare database. `TestSourceSelectionContract` rejects ambiguous combinations. Bare databases require system/cadence assertions. |
| Authoritative validation, calculations and finishing | `NewInvestigationHandler` shares the generated Connect service construction and read boundary with the live API. The in-process transport uses the same protobuf JSON client and handlers. There is no parallel semantic SQL or formula implementation. |
| Frozen reference and explicit bounds | `Now` remains the exact completed collection finish; `RangeReference` changes only derived bounds. Zero-duration collections add one microsecond only to the derived exclusive end. `TestRequestedAndResolvedBoundsStaySeparate` retains requested relative/context bounds separately from effective overrides. `TestOfflineReferenceResolutionPrecision` checks zero/one-microsecond/one-second finishes, exact fixed endpoints, series inclusion, freshness, relative CLI imports and replay without a second tick. |
| Separate capture/export/event clocks | Canonical bundle tests retain the manifest's independent timestamps. Bare sources do not invent export metadata. `TestOfflineRetainsSeparateEpisodesAndEventClock` retrieves events after the collection reference only when explicitly requested and preserves their available-history boundary. |
| Actual captured system and discovery | One offline system registry uses the manifest identity or an explicitly qualified operator assertion. Lens information, systems, metric and check catalogues use the same API. Check overrides/private remediation are unavailable. `nowFixed` identifies the frozen clock; unknown configured retention is absent rather than duration zero. |
| Strict source protection | Shared `Open`/`OpenDatabase` validate closed files without migration, checkpoint, recovery or conversion. The borrowed store view verifies engine read-only mode and does not own Close. Tests reject old/new/checksum-mismatched schemas and WAL files, preserve source bytes, refuse writes/external readers, and exercise unpublished bundle rejection through both consumers. |
| No offline runtime or network initialization | The full CLI/context-replay test replaces the default HTTP transport with a denial object and runs in the network-restricted sandbox. A remote constructor would fail that test. The composition creates no listener, profile authentication, NATS/Kubernetes client, runtime status or archiver callback. |
| Captured facts and unavailable state | Offline freshness does not claim collection is running, collection state is unspecified, and live archiver state is absent. Source limitations distinguish retained facts, missing configuration, partial collections, pending evaluation and unknown retention loss. |
| Precise storage, identities and episodes | The fixture has IDs and logical/server byte values above 2^53, differing replica reports, and a missing server. Storage reads retain logical leader, observed replica and server-store semantics. Two finding episodes remain separate, including a historical gap and historical firing states. Series keep the API's double representation. |
| Read-only methods and bounded failures | The existing allowlist excludes mutations and SQL. Input/response, pagination, ranking, byte and shared deadline limits remain authoritative. Errors omit arbitrary engine text and paths. Cancellation, schema mismatch and unrepresentable references have explicit tests. |
| Continuation across actual CLI processes | `TestOfflineCLITwoProcessesContinue` invokes the real CLI dispatcher in two separate OS processes. Immutable traversal reloads through the existing bounded loader and binds source fingerprint, scope and summary. Invalid offsets/hash, changed source/scope/summary, cancellation, expired/future issue times and row/byte limits are covered. Live random IDs/cache behavior remains covered by existing tests. |
| Local evidence access | Offline browser links are absent and explicitly unavailable. The returned exact context can be saved and reimported against the same local source, with evidence retrieved by typed finding/event ID. Copied origins remain validated inert namespaces, never destinations. |
| Raw SQL distinction | `sql -bundle` uses only the shared reader before the existing advanced SQL executor. Positional columns, typed values, publication and output budgets remain unchanged. JSON adds the validated manifest inside its byte budget. Bare SQL stays schema-independent; trusted SQL's possible external effects are explicit. |

The expected correctness benefit is shared API policy and response assembly instead of duplicated CLI rules. Frozen live/offline parity and the existing handler/finishing regressions verify that benefit. There is no public local listener merely to reuse the handlers.

## Frozen-fixture parity

The live fixture runs through a disposable HTTP API server. Offline reads use the generated Connect handler in memory, against the same closed synthetic database. Canonical bundle reads compare with the same retained bare-file contents.

| Surface | Methods |
|---|---|
| Discovery and summaries | `GetLensInfo`, `ListSystems`, `ListMetrics`, `ListCheckDefinitions`, `GetOverview` |
| Measurements and timeline | `GetTimeSeries`, `GetStatusTimeline` |
| Events and findings | `ListEvents`, `GetEvent`, `ListFindings`, `GetFinding` |
| Storage and investigation | `ListStreams`, `GetStream`, `GetCapacity`, `GetPlacement`, `GetDataHistory`, `GetSlowConsumerInvestigation` |

There are 17 distinct live/offline methods, extra historical finding intervals, an event continuation, and invalid scope/page/cursor/metric/range controls. Comparison preserves meaningful measurements, identities, ranges, precision and episode fields. Permitted differences are collector live/collecting fields, collection state, fixed-clock flag, unknown configured retention in lens information, and absent live archiver state. Storage retention policies are compared. Source/provenance envelopes and browser-link availability differ explicitly. A bundle reports its captured application version; a bare file does not invent one.

Offline stream/consumer tokens are unsigned pagination data, not authentication or signed expiry. Rehydration remains bounded. The five-minute check uses wall time, while relative investigation time uses the dataset. Native database input remains trusted; hashes establish integrity/coherence rather than authenticity or a hostile-input sandbox.

## Runtime regression checkpoints

Each named fix was removed independently, its focused test reached runtime RED, and the original implementation was restored and reached GREEN. The removal script retained production files byte-for-byte and rejected compile/setup failures as evidence.

| Removed fix | Runtime regression |
|---|---|
| Summary evaluation gate | `TestOfflineNoEvaluationDoesNotClaimHealthy` |
| Inert copied-page origin | `TestOfflineContextAcceptsInertUIOrigin` |
| Immutable continuation | `TestOfflineCLITwoProcessesContinue` |
| Derived exclusive-end tick | `TestOfflineZeroDurationCollectionBounds` |
| Unknown retention preservation | `TestOfflineClosedSource` |
| Frozen-clock flag | `TestOfflineClosedSource` |
| Timestamp representation validation | `TestOfflineRejectsUnrepresentableReference` |
| Borrowed engine read-only enforcement | `TestReadViewEnforcesBorrowedReadOnly` |
| SQL bundle provenance | `TestSQLCanonicalBundle` |
| Requested relative bounds | `TestRequestedAndResolvedBoundsStaySeparate` |

The source-selection, disclosure, read-view, transport, source, health/context, continuation, zero-duration, clock-metadata and SQL-provenance RED/GREEN stages have separate conventional commits on this branch. Shared acquisition checkpoints remain in their dependency commits.

## Focused verification

The focused checks passed on the combined candidate, including the canonical shared-reader hardening dependencies:

- Full `internal/investigation` and `internal/sqlcli` regression packages.
- Offline live/bare/bundle parity, sandboxed network denial, process continuation, schema/WAL/refusal, clock/storage/episode tests; offline package coverage 86.8%. New `NewCaptured` and `NewReadView` functions each have 100% focused statement coverage; this does not claim coverage of their entire pre-existing packages.
- `TZ=Pacific/Honolulu` clock, context, storage, episode, canonical bundle and network-disabled tests.
- Targeted race checks for offline source/context/continuation, immutable and live concurrent pagination, in-process transport, and borrowed read views.
- Modified-package vet: offline, investigation, API, store provider, dataset, store, SQL CLI and command composition.
- Existing API lens-clock, batch-range, normalization, account-decoration/byte-budget and investigation-boundary regressions.
- The three existing store-provider Fleet summary regressions and focused command/SQL precision/atomicity/diagnostic regressions.
- Required feature audit: clean, 41 capability docs with both index sections consistent.

Exact principal commands (scratch artifacts stay outside the repository):

```sh
GOCACHE=/private/tmp/natslens-offline-go-cache go test ./internal/offline -coverprofile=/private/tmp/natslens-offline-final.cover -count=1
GOCACHE=/private/tmp/natslens-offline-go-cache go test ./internal/investigation -count=1
GOCACHE=/private/tmp/natslens-offline-go-cache go test ./internal/sqlcli -count=1
GOCACHE=/private/tmp/natslens-offline-go-cache go test ./cmd/natslens -run '^Test(OfflineCLITwoProcessesContinue|SQLBundleSelectionRejectsAmbiguity|SQLCanonicalBundle)$' -count=1
TZ=Pacific/Honolulu GOCACHE=/private/tmp/natslens-offline-go-cache go test ./internal/offline ./internal/investigation -run '^Test(OfflineCanonicalBundleParity|OfflineRejectsUnpublishedBundle|OfflineReferenceResolutionPrecision|RequestedAndResolvedBoundsStaySeparate)$' -count=1
GOCACHE=/private/tmp/natslens-offline-go-cache go test -race ./internal/offline ./internal/investigation -run '^Test(OfflineCanonicalBundleParity|OfflineRejectsUnpublishedBundle|OfflineInvestigationNetworkDisabled|RequestedAndResolvedBoundsStaySeparate)$' -count=1
GOCACHE=/private/tmp/natslens-offline-go-cache go vet ./internal/offline ./internal/investigation ./internal/api ./internal/api/storeprov ./internal/dataset ./internal/store ./internal/sqlcli ./cmd/natslens
```

No full Go/check-engine suite, broad store-provider suite, web suite, benchmark, installation, deployment, restart, push, board mutation or private database/configuration access was performed. No module or migration files changed. Parent-owned review remains required.

## Shared dependency lineage

| Canonical sibling commit | Local equivalent |
|---|---|
| `2c3a5dd1` | `90f513d5` |
| `3ef6a96c` | `0bee8523` |
| `c19796ec` | `46c06e92` |
| `bb49f5cc` | `fbc971cc` |
| `b0832a85` | `bb0a501e` |
| `f2a4c7fb` | `0a23e98a` |
| `e3fb7a6e` | `7e5c7dc5` |
| `61c19460` documentation | `600ac8ae` |
| `a9699b54` documentation | `3b8941e1` |
| `101a849e` R1 regression checkpoint | `60c73d79` |
| `b7526689` canonical path/disclosure fix | `81b7ca13` |
| `31dbd757` exact canonical JSON keys | `f4a3bafe` |

The original shared adapters are `018e4656` (`inspect.go`) and `a0eb3208` (`fingerprint.go`); their sibling equivalents must not be reapplied. Administrative command commits, `babb3ad5`, and the producer's final `680d037d` documentation/comment followup are intentionally omitted from this implementation branch. Administrative changes need their own base; the parent reconciles the producer's final documentation with these semantic capability/index edits. The parent reconciles shared command composition and index edits during integration.
