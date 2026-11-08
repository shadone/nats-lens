# Storage-change investigation verification

Status: candidate; policies proposed, not yet confirmed. Base: `3797d3ae`. Board task: `UQlHZVeNTYO1wCMz0tIsO`.

## Implemented slice

One additive `GetStorageChange` RPC compares completed, retained endpoints through the existing live, captured and demo providers. The shared finisher handles exact optional int64 quantities, signed changes, deterministic ranking, bounded pages and evidence-bound continuation. The existing read-only agent client discovers the method and returns a replay context with resolved system and pins. Capacity opens a thin evidence page with endpoint windows, qualified totals, individual measurements and event/resource links.

The main Capacity change column, contributor charts and drag actions remain separate work. This does not provide physical accounting, causal diagnosis, per-key/object measurements, universal cross-RPC pins or immutable results across source replacement.

## Regression sequence

Small commits preserve runtime RED checkpoints before their implementations. Focused fixtures cover growth, deletion, changed replicas, a missing server, memory/file movement, restart, replacement, unknown creation, invalid creation, KV labels, JSZ/detail-count disagreement, no future completed sample, retention, scope, cancellation and read limits. High int64 IDs and byte values remain exact.

Additional regressions cover the public HTTP handler/read-only method admission, copied prompt scope and microsecond timestamps, returned CLI pin binding, a frozen captured clock and its existing exclusive-end adjustment, precise page budgets in both protobuf and JSON, and a demo comparison using one scenario clock. The page freezes live presets against loaded source metadata; explicit range changes discard old pins. Identical or stale bookends retain values but cannot establish change. Missing endpoints remain pageable without fabricated zero pins.

Setup-only failures were excluded: an early fixture omitted a required server ID; a parameterized multi-statement fixture was invalid. An attempted Capacity clock correction (`05d3ae7c`) failed because it omitted the required clock argument; `5ba37627` corrected it and passed. These are not counted as semantic RED/GREEN proof.

## Commands and receipts

All execution used synthetic fixtures or an owned fixed demo on loopback 18680. No existing database, private service, port 18900, deployment or cluster was accessed.

- Focused API: `GOCACHE=/private/tmp/nats-storage-go-cache go test ./internal/api -run '^TestStorageChange' -count=1` — passed. Disposable HTTP listeners required sandbox escalation; the denied attempt is excluded.
- Focused UI: `npm test -- --configLoader runner src/pages/StorageChange.test.tsx src/lib/timeRange.test.ts src/lib/investigationContext.test.ts src/pages/Capacity.test.tsx --maxWorkers=2` — 48 passed; TypeScript passed.
- Focused non-UTC race: `TZ=Pacific/Honolulu go test -race -p 1 ./internal/api ./internal/api/storeprov ./internal/api/fake ./internal/offline ./internal/investigation -run '^Test(StorageChange|OfflineStorageChange|EveryReadEffectiveContextRoundTrip|Capacity)' -count=1 -timeout=5m`.
- Final fast gate: `GOCACHE=/private/tmp/nats-storage-go-cache python3 -B scripts/verify.py fast --log-dir /private/tmp/nats-storage-final-fast`. Its result and per-stage logs are the authoritative final receipt, rather than an earlier 1,783-test run.
- Modified-package vet: `go vet ./internal/api ./internal/api/storeprov ./internal/api/fake ./internal/offline ./internal/investigation`.
- Proto generation: `make -o web/node_modules/.package-lock.json proto`; no field numbers were reused.

Earlier focused file coverage was 250/295 statements (84.7%) for the new store reader and 147/168 (87.5%) for arithmetic/finishing. These are file-level measurements, not whole-package coverage claims.

## Removal controls

Go overlays under `/private/tmp/nats-storage-removal` independently removed arithmetic, completed-window selection, unknown-presence protection, explicit system naming and read-only admission. Each produced intended runtime failures; unchanged source passed restored tests. UI controls under `/private/tmp/nats-storage-ui-removal` removed the view and portable pin filters, then restored files byte-for-byte.

Final controls under `/private/tmp/nats-storage-final-controls` remove freshness/distinctness plus partial cursor support: same-collection, stale-collection and missing-endpoint continuation regressions fail. Removing final UI corrections produces five failures (range resolution, detail-window rounding and partial/default-system pinning); the exact source is restored before final validation. No mutation is left in the candidate.

## Limits and deviations

The metadata scanner bounds application materialization to 100,000 rows/8 MiB shared across the comparison; ranking stops at 20,000 rows. It does not bound native SQL intermediate work. The common 30-second handler budget and cancellation are cooperative. Page caps are 100 rows and 512 KiB in both wire formats; overflow is explicit failure.

Resource-detail links use the last observed window with the next whole-second exclusive end because existing detail navigation rounds URLs. Exact source windows and pins remain on the comparison page. Events/configuration are supporting observations, never assigned causes.

`make proto` initially triggered an unintended `npm ci` through its prerequisite and replaced this worktree's borrowed dependency symlink with a task-owned directory. No dependency manifest/lockfile changed and the borrowed target was untouched. Subsequent generation bypassed the install prerequisite. This deviation is not hidden as an intentional install.

Browser and real CLI receipts are recorded below after final smoke checks. No full Go, heavy checks-engine suite or broad store-provider suite is claimed. Fresh read-only review and integration remain required.
