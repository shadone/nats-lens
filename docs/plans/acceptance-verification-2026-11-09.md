# Combined verification, 9 November 2026

Exact frozen revision: `87200340ffdc96091b0ecf14c0814fffb5cf5e67`. Result: **failed**. The full profile ran sequentially once, with no interruption or restart. Head and module hashes stayed unchanged; the isolated checkout was clean before and after.

## Executed scope

Matched production UI build passed before `python3 -B scripts/verify.py full`. Format, verifier/build-helper tests, TypeScript, isolated protobuf lint/format/drift, feature audit (46 capabilities), and `go vet ./...` passed. Full web: 204 files, **1,914 passed, 24 existing skips**. The existing 500 kB chunk warning remains.

`go test -p 1 -count=1 -timeout 40m ./...` completed with exit 1: **20 packages passed, 2 failed, 11 had no tests**. The timeout was per package, with no aggregate timeout. Full profile elapsed 2,678.675 seconds (44m38.675s). The runner stopped after go-full, so its two targeted race stages did not run.

The failed packages were:

- `internal/api/storeprov`: two positive/zero-rate conformance rows and `TestReviewOnCallStaleDelivery`. Focused isolation proved missing creation metadata and a thirty-second provider cadence inconsistent with five-minute historical reports. The separately reviewed fixture reconciliation preserves all original assertions and adds unknown-metadata/wrong-cadence controls.
- `internal/investigation`: `TestEveryReadBooleanHasAuditedPolicy` lacked explicit entries for three added RPC booleans. The separately reviewed test reconciliation keeps the fail-closed enumeration and verifies defaults, both explicit values and exact machine-context replay.

See [rate reconciliation](resource-sequence-verification.md#acceptance-fixture-reconciliation) and [boolean audit evidence](investigation-boolean-audit-verification.md). Their focused passes do not turn the original full run into a pass or prove a new uninterrupted full run on their later revisions.

## Environment and artifacts

Go `1.27.3 darwin/arm64`; `github.com/duckdb/duckdb-go/v2 v2.20000.0`. No separate runtime `SELECT version()` probe was made for this run. Existing caches were reused with `GOPROXY=off` and `GOTOOLCHAIN=local`. Locked web dependencies were copied without installation. Synthetic loopback tests were authorized; no private/live service, database, configuration or logs were accessed.

Small GOMAXPROCS=2 occurrence/fixture checks ran in the separate implementation checkout while this frozen run continued. No other full frontend/check-engine suite or benchmark ran concurrently. This is not an uncontended-host performance measurement.

Raw artifacts: `/private/tmp/natslens-full-87200340-20261109/{before.json,after.json,summary.json,production-build.log,full-gate.log,gate/result.json,gate/go-full.log}`. Source changes are retained in Git; the owned temporary checkout and copied dependencies can be removed after these logs are preserved. All-package evidence remains tied to the exact frozen revision. A new full acceptance boundary remains open after the focused reconciliations and subsequent feature integration.
