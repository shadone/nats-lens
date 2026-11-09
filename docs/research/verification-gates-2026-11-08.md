# Verification gate implementation record — 2026-11-08

This record covers the proposed local verification profiles added after `7030584f`. It does not record a full-repository Go test pass or adopt CI.

## Environment and scope

The observed host is Darwin arm64. The runner recorded Go 1.27.3, Node v22.21.1, npm 11.7.0, Buf 1.75.0 and Python 3.15.2. Existing installed web dependencies and build/module caches were reused, with `GOCACHE=/private/tmp/natslens-agent-go-cache` and `GOPROXY=off`. No packages or toolchains were installed. These are local execution observations, not uncontended-host performance claims.

The [verification runbook](../operations/verification.md) defines the profiles and artifact format. The fast profile reuses the approved pure Go tier. The full command plan was inspected with `python3 -B scripts/verify.py full --dry-run`; the expensive full Go and all-package vet stages were not run for this task.

## Executed controls

| Control | Observed behavior |
| --- | --- |
| Initial helper regression checkpoint `b7f42f9a` | Four runtime contract failures before implementation |
| Format-only normalization `fa23ea36` | Fourteen previously committed fixture files exactly match `gofmt` of their committed preimages; no semantic edits |
| Initial complete fast run | Stopped at a valid-YAML output guard regression in its own helper tests; later stages did not run |
| Corrected YAML guard | Seven helper tests and isolated protobuf generation pass |
| Next complete fast run | Static, protobuf/docs, scoped vet and pure Go stages passed; full web tests found one stale Capacity header assertion |
| Capacity assertion | Expected raw heading text omitted the help icon; corrected test checks the accessible heading and help control separately; focused 19 tests pass |
| Synthetic unformatted Go file | Format subset exits 1 even though `gofmt -l` itself exits 0 |
| Synthetic TypeScript type error | Type subset exits 2 |
| Synthetic failing configuration test | Pure Go subset exits 1; later curated packages do not start |
| Appended local generated TypeScript edit | Proto profile exits 1, reports drift and preserves the edit byte-for-byte; the control restores its own edit afterward |
| Actual auditor warning controls | A private feature-doc copy with `Status: planned` passes; an unexpected status warning fails |
| Four independent implementation removals | Format-output detection, first-failure stopping, drift detection and unexpected-warning rejection each fail a runtime assertion; restored seven helper tests pass |
| Targeted races | API stage passes in 15.75 seconds; Kubernetes stage passes in 23.14 seconds, including selected-test discovery, in Pacific/Honolulu |

Temporary probes use unique owned files and restore their edits in `finally` blocks. No private data, deployment, external service or existing database was used. Synthetic tests retain their local fixture behavior.

## Acceptance run

The final complete fast profile passes in 86.75 seconds, including 1,776 passing web tests and 24 existing skips across 180 files. The preceding run had one failure and 1,775 passing tests. The final receipt is `/private/tmp/nats-verify-fast-final/result.json`; the tracked [receipt summary](verification-gates-2026-11-08.json) preserves tool versions, stages and exit codes. The complete race profile passes in 39.14 seconds. These are single local runs with compilation-cache reuse, not performance budgets.

Full Go, all-package vet, release compatibility, container architectures and browser acceptance remain outside this task's executed coverage. The final acceptance run includes uncommitted documentation changes, identified by its recorded dirty state; verification implementation matches `c58d30b0`.

Raw local evidence is retained under `/private/tmp/nats-verify-*`, including `nats-verify-controls/results.json`, `nats-verify-removal/results.json`, `nats-verify-race-acceptance/result.json` and `nats-verify-format-only.json`. These paths are local receipts, not portable prerequisites.
