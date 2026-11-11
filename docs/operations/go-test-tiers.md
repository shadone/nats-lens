# Run the Go test tiers

Use these tiers to choose the dependencies and coverage needed for a change. Run commands from the repository root with the Go version in `go.mod`; the DuckDB driver needs the existing native build prerequisites. Tests use disposable stores and local listeners, not a deployed NATS system.

## Tier inventory and short policy

| Tier | Source inventory | Dependencies and short policy |
| --- | --- | --- |
| Fast policy and decoding | `scripts/test-go-fast.sh`: configuration and route health; selected collector lookup, decoding, and snapshot tests and command policy tests | No NATS or HTTP listeners and no DuckDB stores. Temporary configuration files are allowed. Selectors are explicit because packages mix pure and integration tests. |
| DuckDB integration | `internal/store`, SQL fixtures in `internal/checks`, `internal/api/storeprov`, `internal/sqlcli`, `internal/inventory`, and command store/archive tests | Temporary or in-memory DuckDB; some API fixtures also serve local HTTP. Ordinary row, migration, checksum, atomic-write, and contract tests remain in short mode. Short is not a unit tier. |
| Local HTTP and concurrency integration | `internal/api`, `internal/notify`, `internal/iap`, `internal/k8s`, plus command metrics and delivery tests | Fake providers, HTTP test servers, cancellation, deadlines, and concurrency. Retained in short mode unless the case also starts NATS or requests scale coverage. |
| Real local NATS and cluster integration | `internal/testcluster` callers in commands, collector, capture, runner, checks, store, and store provider; standalone `server.NewServer` fixtures | One or more real in-process NATS servers with local ports and disposable storage. The shared cluster fixture and standalone server-owning fixtures skip in short mode. Run without `-short`. |
| Scale and extended histories | `*_scale_test.go`; large/long cases in store `*_time_test.go`, `*_random_test.go`, matching tests, collector micro/subscription tests, and runner acceptance tests | Cases guarded by `testing.Short()` skip or reduce their bulk workload. `TestOldestUnreadScale` limits its check to 10 times one pass over the same history, timed in the same process, rather than to a fixed time, so a loaded runner does not fail it. `TestSubjectsAPIRateScale` additionally requires explicit size environment variables and skips in short mode even when they are set. |
| Fuzz seeds | `Fuzz*` functions, listed in `scripts/fuzz.sh` | No NATS, HTTP or DuckDB. Ordinary runs replay the seeds under `testdata/fuzz` only; fuzzing needs `-fuzz` ([fuzz parsers and boundaries](#fuzz-parsers-and-boundaries)). |
| Live deployment/manual | `scripts/dev-cluster.sh`, load and churn scripts; external Kubernetes checks | Outside the automated fast tier. The dev scripts create a local cluster; Kubernetes observations require a separately authorized environment. Automated test tiers require no private deployment. |

`make test-short` excludes real NATS fixtures and guarded scale work, but still runs database-heavy integration tests and local HTTP clients/listeners. It has no fast-runtime guarantee. Some tests exercise failed NATS dials without starting a server; those also belong to integration, not the curated pure tier. New pure tests belong in the fast selectors only after checking their transitive fixture dependencies. New server fixtures must honor `testing.Short()` at the helper that owns the server, before creating it.

## Run focused tiers

Run the curated tier uncached, optionally with the race detector:

```sh
make test-fast
./scripts/test-go-fast.sh -race
```

The script uses `-count=1` and anchored selectors. Imported packages and native libraries may still compile or link; avoiding a database fixture does not remove the DuckDB link dependency from a mixed test binary.

Run ordinary store and API/store integration separately from SQL checks:

```sh
go test -short -count=1 -timeout 40m ./internal/store ./internal/api/storeprov ./internal/sqlcli ./internal/inventory
go test -short -count=1 -timeout 40m ./internal/checks
```

Run local NATS micro and connection-transition coverage without `-short`:

```sh
go test -count=1 -timeout 3m ./cmd/natslens -run '^(TestMicroRefusalIsNoSubscriptionError|TestMicroBadAccountWithoutEvents|TestMicroTwoLabelsOneAccount|TestRunAsksMicroServices|TestEventConnRedials|TestRedialBacksOffWhenDialFails|TestKeepDialsAConnectionWhoseFirstDialFailed)$'
```

Run the cross-layer cluster stories explicitly:

```sh
go test -count=1 -timeout 5m ./internal/collect -run '^TestCollect'
go test -count=1 -timeout 5m ./internal/events -run '^TestCaptureFromCluster$'
go test -count=1 -timeout 40m ./internal/checks -run '^TestChecksAgainstCluster$'
go test -count=1 -timeout 10m ./internal/api/storeprov -run '^TestReferenceStoryOnTheCluster$'
```

Run selected scale cases without `-short`. Choose the environment-sized subjects case deliberately; its row count grows with all three inputs.

```sh
go test -count=1 -timeout 40m ./internal/checks -run '^(TestSourceHealthScale|TestOldestUnreadScale)$'
go test -count=1 -timeout 10m ./internal/api/storeprov -run '^TestCapacityManyStreams$'
SUBJECTS_SCALE_DAYS=1 SUBJECTS_SCALE_SERVERS=2 SUBJECTS_SCALE_ACCOUNTS=3 go test -count=1 -timeout 10m ./internal/api/storeprov -run '^TestSubjectsAPIRateScale$'
```

These commands document coverage boundaries; their presence does not record a passing run. Use the task's validation record for executed selectors and results.

## Fuzz parsers and boundaries

Go fuzz targets check the parsers and boundary rules that take untrusted or hand-written input. Each target runs in memory, without NATS, HTTP or DuckDB, and bounds its input size.

| Target | Package | What it checks |
| --- | --- | --- |
| `FuzzParseDuration` | `internal/config` | A duration in days is a whole, non-negative number of days; the written form reads back the same |
| `FuzzParseConfigFile` | `internal/config` | A YAML file is accepted or refused the same way each time; a refused file returns the base, and no file changes the base |
| `FuzzSnapshotApply` | `internal/collect` | A monitoring reply without server info is an error and leaves no server; unknown enum values are reported only for a reply that does not decode plainly |
| `FuzzDecodeEvent` | `internal/events` | A decoded event has a kind, its source and the receive time in UTC; its raw payload is a JSON object without a `jwt` key at any depth |
| `FuzzFromURL`, `FuzzPageFor` | `internal/eventselection` | An accepted selection link names a page of its own membership and reads the same when written again; paging by `Next` visits every ID once |
| `FuzzParseContext`, `FuzzContextFromURL` | `internal/investigation` | Accepted context validates, holds no C0 control character or DEL in its short texts, encodes and reads back to the same encoding, links to the trusted deployment, and prepares the same request each time |
| `FuzzDecodeCursor`, `FuzzEntityToken`, `FuzzEpisodeCursor` | `internal/api` | A page token is refused as invalid or expired, or holds bounded values and encodes again to a token that reads the same |
| `FuzzTimelineBuckets` | `internal/api` | An accepted timeline range is ordered, at most 400 days, and holds 1 to 2000 buckets that cover every instant |
| `FuzzEntityKey` | `internal/api` | An accepted compound entity key splits into non-empty parts that join to the key |

The targets for the configuration, monitoring replies, events, selection links, investigation context and timeline buckets also check that the same input gives the same result twice, as their code ranges over maps or decodes in several passes.

The seeds come from the recorded fixtures, the investigation tests' page URLs, protocol edge cases (timestamp and duration extremes, zone offsets, identifier separators, invalid UTF-8, control characters) and the inputs that found earlier bugs, kept under each package's `testdata/fuzz/<target>/`. An ordinary `go test` run replays every seed as a subtest, deterministically and in about a second per package; it never fuzzes.

Fuzz one target for a bounded time:

```sh
go test -run='^$' -fuzz='^FuzzParseContext$' -fuzztime=60s -fuzzminimizetime=5s -parallel=4 ./internal/investigation
```

Fuzz every target, one after another:

```sh
scripts/fuzz.sh 3m
```

`scripts/fuzz.sh` takes the time per target (default `3m`) and `FUZZ_PARALLEL` for the workers per target (default 4). `scripts/fuzz.sh check` fails when a `Fuzz` function is missing from its list, and every run starts with that check. The short `-fuzzminimizetime` keeps the run fuzzing: minimizing a new input holds up the other workers, and with the default 60 seconds per input a run sat idle for most of a minute at a time.

A failure writes the input to the package's `testdata/fuzz/<target>/`. Keep that file, give it a name that says what it holds, add a regression test, and fix the cause. The inputs the fuzzer finds interesting go to the Go build cache (`$(go env GOCACHE)/fuzz`), not the repository.

The [Fuzz workflow](../../.github/workflows/fuzz.yml) runs `scripts/fuzz.sh 5m` every Tuesday and on demand, with the build cache's fuzz corpus restored from the previous run; a failure fails the run and keeps the testdata directories as an artifact for 30 days. A scheduled workflow, rather than a `verify.py` profile, keeps about 70 minutes of fuzzing off the machines that run the verification profiles, and a fuzz run is not a pass or fail gate that a change can wait for. The workflow is **proposed, not yet confirmed**.

Known limits: `FuzzEpisodeCursor` reaches its accepted path only through seeds made at the start of a run, as a token expires five minutes after it was issued; `FuzzContextFromURL` rarely produces a percent-encoded control character, so its seeds carry them; `FuzzParseConfigFile` does not call `Validate`, which looks at the data directories on disk.

## Run combined validation

Use `make verify-fast` for shared static checks, this curated pure Go tier and complete web tests. Use `make verify-full` for shared checks, full Go/web suites and selected non-UTC races. See [entry-point scopes and prerequisites](verification.md). These aggregate checks do not replace focused edit-time tests.

Reserve the full suite for a coordinated validation boundary. `internal/checks` alone can take about 13 minutes, beyond Go's default 10-minute timeout. Avoid running it alongside frontend tests, browser timing, or another database-heavy suite.

```sh
go test -count=1 -timeout 40m ./...
go vet ./...
```

`make test` uses the same 40-minute per-package timeout. `make test-short` also allows 40 minutes because it retains DuckDB integration. Neither target disables the test result cache; use the explicit `-count=1` commands for measurements. The opt-in subjects scale case still needs its environment variables during a full run.

## Keep tests steady on a loaded machine

A CI runner or a busy workstation can pause any goroutine for hundreds of milliseconds. Write tests so that load makes them slower, never wrong:

- Wait for the condition itself: poll it with a deadline of seconds, or wait on a channel or a counter the fake keeps. A fixed `time.Sleep` before a check is either too short under load or lets the check pass without testing anything.
- A test that tells a fast path from a timeout keeps them seconds apart: a timeout of 1 s or more and a bound well below it, not 20 ms against 30 ms. Prefer a slow path that never ends (a held fake, a long caller deadline), so the bound only has to be generous.
- `Runner.Interval` also limits each epoch's write. A fixture that calls `Once` gives it 10 s; only a test of `Run` uses a short interval, as `Run` takes the next epoch after a write that runs out.
- A handler can keep reading in the background after the test returns. Log through `apitest.Log(t)`, which drops lines after the test ends: the test output panics on a late write and fails the whole package.
- Web tests: reach into a dialog opened by asynchronous work only after `findByRole("dialog", ...)`, as `Dialog` opens in an effect after the commit. Type into a debounced field with the debounce held (`web/src/test/heldDebounce.ts`), so that a pause between keys cannot settle a prefix, and keep one test of the component without the hold, on fake timers, that checks the delay (`SearchJump.debounce.test.tsx`). Vitest gives a test 15 s and `findBy`/`waitFor` 5 s; a test never relies on a find running out, but waits with `waitFor` for whichever of two outcomes appears.

Check a timing-sensitive test under load before trusting it, for example with `GOMAXPROCS=1 go test -race -count=20 -cpu 1 -run <name>` beside a few `yes > /dev/null` processes, or with `npx vitest run --maxWorkers=36` on a busy 12-core machine.

## Measure a change

Coordinate a bounded quiet window with other tasks before comparing runtimes. Record the revision, exact selector, Go version, race flag, test package elapsed time, and `/usr/bin/time -p` output. Use `-count=1`; build-cache reuse is separate from test-result caching. Record compilation overhead when comparing wall time.

Observe load before and after each stage with `uptime` and the visible process list. Coordination establishes a task-level quiet window, not an uncontended host: background work and workloads outside the visible process list remain limits. Keep baseline, changed, and race stages sequential. Stop and report if a focused run expands unexpectedly; do not substitute a full suite for a focused failure.

The micro refusal regression requires two persisted epochs, a SHOP permission refusal for INFO or STATS, and the corresponding held request in a later epoch. Only then does it interpret the absence of `$SRV` subscription errors. A zero-activity control must fail; `TestMicroRefusalEvidence/no_activity` checks that invariant without any servers or stores. The related run fixtures wait for named SQL conditions with bounded contexts and report the missing state and last count on timeout. The existing real connection backoff test remains separate from `TestRedialBackoffPolicy`, whose clock advances under `testing/synctest`.
