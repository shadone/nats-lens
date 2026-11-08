# Local production-build reuse measurements

These single-sample observations measure the existing build against verified asset reuse. They are not a general latency guarantee. The [runbook](../operations/build-reuse.md) records artifact guarantees and the remaining container gate.

## Setup

Baseline `0d52576f`; local build implementation `0a0b69ea`; container recipe `4daf3551`. Host: darwin/arm64, Go 1.27.3, Node 22.21.1, npm 11.7.0. Builds ran sequentially in the same disposable checkout with privately copied existing dependencies and a reused Go cache. No unrelated caches were cleared. Other host workloads were not excluded.

Every invocation used `make -o web/node_modules/.package-lock.json build`, `GOPROXY=off`, `GOFLAGS=-buildvcs=false` and the same reused GOCACHE. The explicit old-file prerequisite suppresses installation for this measurement only. It is not the recommended production command.

## Observed wall times

| Scenario | Before, seconds | After, seconds | Frontend rebuilt after? |
| --- | ---: | ---: | --- |
| clean | 17.201 | 12.619 | yes |
| noop | 5.723 | 0.413 | no |
| go-only | 10.501 | 4.402 | no |
| frontend-only | 9.913 | 9.699 | yes |
| dependency-input | 5.630 | 6.013 | yes |
| generated-proto | 5.627 | 5.942 | yes |

Clean removes output assets and the binary; it does not purge compilation caches. The no-op follows that successful build. Go-only appends a Go comment; frontend-only adds a CSS custom property; dependency-input adds package.json whitespace; generated-proto appends a generated TypeScript comment. The latter cases measure rebuild selection, not installation or generation cost. All invocations exited zero. Clean-build compilation cache history differs between the before/after series, so its change should not be attributed solely to this optimization.

## Artifact and invalidation evidence

- The final binary served all 24 production UI files byte-for-byte from the generated tree.
- A separate build-go with existing assets served those same 24 files. Removing assets in the private checkout produced the exact embedded placeholder on another Go-only build.
- Focused filesystem contracts cover changed/deleted inputs, configuration/lock/generated/proto changes, missing/tampered outputs, corrupt state, environment changes, force rebuilds, failed generation, mid-build edits, dependency markers, source symlinks/cycles and lock ownership through embedding.
- The cache manifest is 165 bytes; production assets are about 1.5 MiB. A rebuild temporarily holds another asset generation of that size. The test dependency copy is about 168 MiB. The reused host Go cache is about 5.3 GiB total; this work did not isolate its marginal growth. Docker cache storage is unmeasured.

## Limits

- Sequential synthetic private checkout; not a host-uncontended benchmark.
- Existing compiler cache reused; clean means absent output assets/binary, not a purged compiler cache.
- Dependency-input case changes package.json whitespace; no install/download timing.
- Generated-proto case changes generated TypeScript; no proto regeneration timing.
- Docker daemon unavailable; target builds/execution and BuildKit disk use remain unverified.

The [individual observations](build-reuse-2026-11-08.json) preserve the measurements. No full Go or web test suite was required for these build orchestration changes; actual frontend typecheck/generation ran in the measured builds.
