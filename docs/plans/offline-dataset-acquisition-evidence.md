# Offline dataset acquisition implementation evidence

Implementation candidate based on `bac133e5`, in the isolated branch `t3/offline-dataset-snapshot-export`. Mechanism, artifact format, and administrative surface remain proposed, not yet confirmed as owner policy. Fresh read-only review is root-owned and pending this handoff.

All databases, WAL activity, events, findings, and annotations used here are synthetic fixtures. No existing private database, configuration, archive, log, NATS service, or deployment was acquired. No collector was restarted, installed, exposed, or deployed. No nested reviewer was spawned.

## Contract and ownership

`internal/dataset` owns the versioned manifest, closed read-only opener, integrity/provenance validation, canonical boundary inspection/fingerprint, and acquisition writer. `internal/store/dataset.go` supplies a guarded read-only opener and the in-process consistent logical copy. `internal/acquisition` supplies the opt-in private Unix administrative channel. `cmd/natslens/acquire.go` and the `run` dispatch expose that explicit channel.

The sibling owns semantic offline queries, raw-SQL JSON integration, context/profile behavior, and captured continuation. This branch does not implement those business rules or parse their contexts. The sibling's independent adapter commits `018e4656` and `a0eb3208` were incorporated once as `1171a061` and `7c790e4b`; integration must avoid duplicating their equivalents.

Shared API:

```go
dataset.Acquire(ctx, store, destination, system, applicationVersion, options) (dataset.Manifest, error)
dataset.Open(ctx, directory) (*dataset.Dataset, error)
dataset.OpenDatabase(ctx, closedPath) (*sql.DB, error)
dataset.InspectDatabase(ctx, validatedReadOnlyDB) (dataset.Boundaries, error)
dataset.FingerprintDatabase(ctx, validatedClosedPath) (string, error)
```

`Dataset` owns `Manifest`, `DB`, and `DBPath`; `Close` closes its owned DB. Bare opener callers own `Close`. Inspection borrows the handle. Fingerprinting requires holding the validated read-only handle through hashing and queries. Hashes establish integrity/coherence, not authenticity or safe loading of arbitrary databases.

## Runtime RED and restored GREEN

The initial runtime RED checkpoint is `2c3a5dd1`; `TestDatasetCopyActiveBoundary` failed with `consistent dataset export not implemented`. Minimal GREEN is `3ef6a96c`. Acquisition's second runtime RED checkpoint is `c19796ec`, followed by GREEN `bb49f5cc`. The administrative channel's runtime RED checkpoint is `380a4f90`, followed by GREEN `2f724835`.

The transaction was then deliberately removed around the source copy on a disposable source-code working edit. The focused command was:

```sh
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/store -run '^(TestDatasetCopyActiveBoundary|TestDatasetConcurrentPublishedStagesAndCancellation)$' -count=1 -timeout 90s
```

Without the consistency boundary, the command failed with `capture 1 export 2`; epochs, events, check runs, findings, mutes, and acknowledgements all crossed the captured boundary. Restoring the source transaction made that exact command pass. The transient removed-fix edit was not committed.

Additional regressions failed before their fixes and passed after restoration:

| Regression | RED observation | Fix |
| --- | --- | --- |
| `TestOpenDatabasePreservesCancellation` | Cancellation reclassified as incompatible version | `b0832a85` |
| `TestInvalidLatestCollectionHasNoReferenceClock` | Completion before start supplied a reference clock | `b0832a85` |
| `TestManifestNewlineCountsAgainstArtifactBudget` | Oversize manifest accepted because its newline was omitted | `f2a4c7fb` |
| `TestPreparedButUnpublishedDatasetCannotOpen` | Public reader accepted prepared, unpublished staging | `e3fb7a6e` |

The staging test includes a published control, rejection before rename, and successful fresh-reader opens after simulated atomic publication. The zero-duration collection test preserves `finished_at == started_at` exactly; query-end adjustments belong to the sibling's query layer.

## Focused verification

The dataset package passes with 81.2% statement coverage before the final staging/clock assertions. The acquisition package passes with 83.8% statement coverage. Coverage reflects focused package tests, not broad repository coverage.

Focused commands executed:

```sh
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset -count=1 -timeout 120s -coverprofile=/private/tmp/natslens-dataset-coverage.out
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/store -run '^TestDataset' -count=1 -timeout 90s
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/acquisition -count=1 -timeout 120s -coverprofile=/private/tmp/natslens-acquisition-coverage.out
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./cmd/natslens -run '^TestAcquire' -count=1 -timeout 120s
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/acquisition -count=1 -timeout 120s -race
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset ./internal/store -run '^(TestDataset|TestAcquire|TestOpenDatabase|TestOpenRejects|TestInspect|TestFingerprint|TestInvalidLatest|TestAtomicPublication|TestManifest|TestPrepared|TestZeroDuration)' -count=1 -timeout 120s -race
TZ=America/Los_Angeles GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset ./internal/store -run '^(TestDataset|TestAcquire|TestOpenDatabase|TestOpenRejects|TestInspect|TestFingerprint|TestInvalidLatest|TestAtomicPublication|TestManifest|TestPrepared|TestZeroDuration)' -count=1 -timeout 120s
GOCACHE=/private/tmp/natslens-dataset-go-cache go vet ./internal/dataset ./internal/acquisition ./cmd/natslens
python3 /Users/denis/.codex/plugins/cache/ddenis/ddenis-core/0.4.0/skills/feature-docs/scripts/audit_feature_docs.py docs/features/ --map-refs require
```

Dataset/store checks ran in the network-restricted sandbox. The reader disables engine external access and extension installation/loading; tests prove external-file access is refused and read-only writes fail. Unix-socket tests required the sandbox exception to bind only their private temporary sockets. No public TCP listener or NATS connection is used.

No full Go suite, checks package suite, broad store-provider suite, web suite, dependency installation, or benchmark was run. No performance measurement is claimed.

## Acceptance matrix

| Requirement | Evidence | Limit or qualification |
| --- | --- | --- |
| Engine-supported active acquisition | Pinned DuckDB logical copy; source metadata and data share one read transaction; actual concurrent collection/check/event/operator publication tests | In-process channel required; external opening of active writer is unsupported |
| Capture boundary | Deterministic old/new transaction comparison and exact source row multiset comparison | Pipeline completion and in-memory buffers remain unknown; separately published checks can be pending |
| Complete schema and data | Every table's exact canonical row values, SQL definitions, keys/check constraints/defaults/indexes, table/column/view comments, migration checksums and sequence options compared | Sequence continuation and constraint failures exercised only on synthetic descendants; no generic migration tool |
| Versioned manifest | Exact decimal-string int64 values, explicit nullable metadata, system/config facts, versions, UTC capture/export/history/event/check boundaries and checksum | Retention is configured policy, not a continuity claim; checks configuration/runtime callbacks absent |
| Offline open/query | Same shared read-only validator, checksum/provenance/schema/engine checks, stored-column scans, sandbox-restricted query tests | Trusted input only; checksum does not authenticate origin; semantic commands owned by sibling |
| No source repair/mutation | Export only reads source tables, checkpoints destination, and preserves checksum records; cancellation leaves collection usable; source restart passes | Source can accumulate MVCC/WAL history while pinned |
| Work budgets | One process-wide slot, admission headroom, cooperative duration/size/free-space monitors, oversize/space/cancellation tests | Native overshoot and cleanup latency remain possible; hard destination isolation requires external quota; no source growth/CPU latency guarantee |
| Atomic publication | Private staging, validated/synced destination, no-replace rename, existing-empty-directory refusal, restart and pre/post-rename controls | Post-rename directory-sync failure reports publication with durability error; process death can leave reserved partial directories |
| Access and privacy | Opt-in 0600 local socket and same-user private directories, strict system-only request, sanitized errors, no arbitrary server path, source ingest redaction unchanged | Same-user filesystem authorization is administrative; full retained system includes private annotations/raw stored events/durable notification facts |
| Transfer and retention | CLI/operations state server-local artifact path only, explicit user transfer, private modes and independent artifact retention | No upload, auto-transfer, or automatic artifact deletion |
| Documentation | Capability, both index sections, operation/reference docs and adjacent cross-links; audit clean | Mechanism/format owner policy remains proposed; root integration/review pending |

## Integration sequence

This branch's chronological commits are `2c3a5dd1`, `3ef6a96c`, `c19796ec`, `1171a061`, `bb49f5cc`, `380a4f90`, `7c790e4b`, `b0832a85`, `61c19460`, `2f724835`, `f2a4c7fb`, and `e3fb7a6e`, followed by final focused tests/documentation handoff commits. The two adapter equivalents must each appear only once in the root's integrated candidate.

For a sibling already holding the initial reader and adapters, writer dependencies are `c19796ec` then `bb49f5cc`, followed by `b0832a85`, `f2a4c7fb`, and `e3fb7a6e`. Dataset docs are `61c19460` plus final staging wording. The admin RED/GREEN pair owns `cmd/natslens/main.go`; the sibling must not independently duplicate that acquisition dispatch. Root resolves both branches' feature-index contributions and owns fresh read-only review of the complete integrated candidate.

## R1 review corrections

Fresh R1 on `a9699b54` requested changes for two P2 findings: a trailing `/.` bypassed reserved staging-name rejection, and valid database hashes did not require the application version or capture disclosures. All other R1 acquisition checks passed. R2 remains root-owned and pending this corrected candidate.

The reviewer probes `TestReviewUnpublishedCanonicalAlias` and `TestReviewMissingCaptureDisclosures` are retained in `review_r1_test.go`. Runtime RED checkpoint `101a849e` reproduces both defects. Shared fix `b7526689` normalizes paths before the reserved-name check and validates the complete version 1 object/disclosure contract. It also tests relative paths, trailing slash/dot segments, canonical and symlink parent aliases, final-symlink rejection, normal published controls, omission/null/empty/unsupported/missing/extra/duplicate disclosure tokens, every required field, duplicate JSON keys, reordered valid token sets, exact integers above 2^53, and unknown optional retention.

Followup `31dbd757` closes analogous case-folded field aliases accepted by Go's typed JSON decoder. All four top/system/boundary/data alias regressions failed before this fix and passed after it. Exact canonical object keys are enforced centrally; the writer and reader share disclosure constructors. No consumer implements a second parser or gains a public staging bypass.

Each correction was independently removed after its GREEN commit and restored:

```sh
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset -run '^(TestReviewUnpublishedCanonicalAlias|TestReviewStageAliasesAndPublishedControls)$' -count=1 -timeout 90s
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset -run '^(TestReviewMissingCaptureDisclosures|TestReviewDisclosureSemantics)$' -count=1 -timeout 90s
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset -run '^TestReviewRejectsCaseFoldedFieldAliases$' -count=1 -timeout 60s
```

Restoring the raw-path basename check reopened unpublished aliases and failed the first command; restoring normalization passed. Removing only the v1 contract call accepted missing/invalid disclosures and failed the second command; restoring it passed. Removing only exact object-key admission accepted all four case-folded aliases and failed the third command; restoring it passed. These transient removed-fix edits were not committed.

Post-fix focused verification passed:

```sh
TZ=America/Los_Angeles GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset ./internal/store -run '^(TestReview|TestDataset|TestAcquire|TestOpenDatabase|TestOpenRejects|TestInspect|TestFingerprint|TestInvalidLatest|TestAtomicPublication|TestManifest|TestPrepared|TestZeroDuration)' -count=1 -timeout 120s -race
GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/acquisition ./cmd/natslens -run '^TestAcqui' -count=1 -timeout 120s -race
TZ=America/Los_Angeles GOCACHE=/private/tmp/natslens-dataset-go-cache go test ./internal/dataset -run '^TestReview' -count=1 -timeout 90s -race
GOCACHE=/private/tmp/natslens-dataset-go-cache go vet ./internal/dataset ./internal/acquisition ./cmd/natslens
python3 /Users/denis/.codex/plugins/cache/ddenis/ddenis-core/0.4.0/skills/feature-docs/scripts/audit_feature_docs.py docs/features/ --map-refs require
git diff --check
```

The combined non-UTC/race run passed for dataset/store in 27.397/8.586 seconds; private admin/CLI race checks passed in 6.731/2.539 seconds. The case-folded-key followup received a separate final focused non-UTC/race pass. Vet and the 41-document feature audit are clean. No full suite, web tests, benchmark, live acquisition, nested review, deployment, upload, or push was performed.

R1 shared dependencies after `a9699b54` are `101a849e` then `b7526689` then `31dbd757`. The subsequent documentation commit records canonical aliases, required disclosures, both feature-index entries, and this evidence. The manifest field shape, exact integers, clock semantics, optional unknown retention, checksums-not-authenticity boundary, native cooperative limits, and administrative access model are unchanged.
