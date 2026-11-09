# Investigation boolean audit reconciliation

Base: `7ae51f84`. The frozen full run at `87200340` reported `TestEveryReadBooleanHasAuditedPolicy` failures for `ListMetricContributors.descending`, `GetTimeSeries.includeObservations` and `GetStorageChange.resourcesOnly`. Those RPC fields had been added without adding them to the explicit test audit. Production request preparation already preserves their descriptor-defined values.

The audit now lists those three fields explicitly. Its enumeration and fail-closed assertion remain unchanged: any future unlisted read boolean still fails. Companion tests verify each protobuf default is false, explicit false and true survive preparation, and exact machine context serialization/reimport preserves the entire request. The options respectively control native ranking direction, optional pre-bucket observation metadata, and omission of server rows from storage comparisons. They do not create inherited URL defaults or establish UI-link equivalence.

Verification uses `GOCACHE=/private/tmp/nats-storage-go-cache GOPROXY=off GOTOOLCHAIN=local GOMAXPROCS=2`:

- `/private/tmp/nats-boolean-audit-red.log`: exact original three runtime audit failures; companion value/round-trip cases pass.
- `/private/tmp/nats-boolean-audit-green.log`: complete `go test ./internal/investigation -count=1 -timeout=3m` passes, 1.062s. Test-only loopback permission was approved.
- `/private/tmp/nats-boolean-audit-proof/removed-red.log`: overlaying the original audit reproduces all three runtime failures.
- `/private/tmp/nats-boolean-audit-proof/restored-green.log`: restored audit and companion contracts pass. Tracked production files were never mutated by overlays.

The initial companion fixture used a new metric UI path that the portable-context allowlist does not yet accept. That setup failure is excluded from runtime proof. These explicit-RPC policy tests use a supported server-page context; metric-route import/copy compatibility is a separate discovered product gap, not claimed fixed here.

Only tests and this evidence record change. No production defaults, algorithms, security allowlists, protocol definitions, dependencies, migrations or capability behavior change. No frontend or broader package rerun is needed for this delta. The original full run remains failed; this focused result is not relabeled as an uninterrupted full pass. No private/live sources were used.
