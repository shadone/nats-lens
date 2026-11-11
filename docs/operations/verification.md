# Run repository verification

Use these entry points to check a stable working tree before integration. Choose a focused check during editing and reserve the full Go suite for a coordinated validation boundary. The tier selection is **proposed, not yet confirmed** as an owner policy.

## Prepare the tools

Use the Go version required by `go.mod`, its native DuckDB build prerequisites, Node.js and npm from the [development prerequisites](../development.md#prerequisites), Python 3.9 or later, and `buf`. Install locked web dependencies explicitly before verification. Go protobuf plugins come from the tool declarations in `go.mod`; the TypeScript plugin comes from the installed web dependencies.

Provide the installed feature-docs auditor. The runner discovers the highest numbered `ddenis-core` version in the local Codex plugin cache. On another machine, set `FEATURE_DOCS_AUDITOR` to its `skills/feature-docs/scripts/audit_feature_docs.py` path, or pass `--auditor PATH`. A missing auditor fails verification; the runner never downloads one.

Dependency preparation is separate from verification. The runner does not run `npm ci` or update lockfiles. Go commands can fetch missing modules or a toolchain according to Go's environment settings. For a prepared offline cache, set `GOPROXY=off` and `GOTOOLCHAIN=local` deliberately.

## Choose the scope

Run commands from the repository root:

| Entry point | Checks | Excluded work |
| --- | --- | --- |
| `make verify-fast` | Go format, Go release agreement (the release go.mod builds with, from its `toolchain` line or else its `go` line, and the Dockerfile's `golang` image), the fuzz target list (`scripts/fuzz.sh check`), gate/build helper tests, TypeScript, protobuf lint/format/drift, feature docs, scoped Go vet, curated pure Go tests, complete web tests | Database/NATS Go fixtures, scale tests, targeted race tests, production build |
| `make verify-full` | Shared static checks (Go release agreement included, the fuzz target list too), all Go vet, complete web tests, uncached full Go suite, targeted API/Kubernetes race tests | Explicit environment-sized scale cases, extended fuzzing, release/container matrix, production build |
| `make verify-release` | The full profile, then the compatibility matrix (`compat-store`, `compat-cmd`) | As for the full profile; see the [compatibility check](upgrade-compatibility.md) |
| `make verify-compat` | Every schema version upgraded, interrupted and rebuilt from its archive, a newer schema refused, migration rules, with the migration, archive, crash and recovery tests they build on | Everything else |
| `make verify-web` | TypeScript and complete web tests | Go, protobuf and documentation checks |
| `make verify-proto` | Protobuf lint, format and isolated generated-code comparison | Other tests |
| `make verify-race` | Selected API pagination/budget/cancellation and Kubernetes watch recovery races | Broader package or full-repository race coverage |

The fast profile avoids database and local-server Go fixtures; it still runs the complete frontend suite. It is broader than `make test-fast`, which runs only the [curated pure Go tier](go-test-tiers.md). Neither `-short` nor the fast profile establishes a runtime limit.

The full profile runs its expensive stages sequentially. Its Go command is `go test -p 1 -count=1 -timeout 40m ./...`: the timeout applies per package, with no aggregate wall-clock timeout. Do not run it alongside database-heavy tests or browser measurements. Persisted fuzz seeds run when their package's ordinary Go tests include them; the profile does not start an extended fuzz session. The subjects scale fixture still requires its explicit environment variables.

The compatibility stages set `NATSLENS_COMPAT_MATRIX=1` and `TZ=Pacific/Honolulu` and, like the race stages, first check that every selected test exists. Without that variable the same tests run only their seed versions, which keeps them in the full profile and CI at a small cost.

Targeted races use `TZ=Pacific/Honolulu`, `-race`, `-short`, `-count=1` and a five-minute per-package timeout. The runner first checks that every named test exists, then executes the anchored selectors. The selected tests exercise entity-page eviction/cancellation, API batch budgets and watch authentication/recovery; this is not all concurrency coverage.

For a small edit, select a named stage:

```sh
python3 -B scripts/verify.py fast --only format,types
python3 -B scripts/verify.py full --dry-run
```

A subset prints **NOT a complete profile** and records `complete_profile: false`. A dry run prints commands without executing checks. Neither is a passing complete gate.

## Read failures and artifacts

The runner stops at the first failing stage and exits nonzero. It prints the artifact directory and each stage's elapsed time. Read the failing stage's log before running more checks. Interruption stops the active command's process group; it does not cancel unrelated work.

The default artifact directory is a private temporary directory named `natslens-verify-*`. Use `--log-dir PATH` to retain logs at an explicit location; choose permissions appropriate for local test output. Each stage has a log. `result.json` records the profile, selected and completed stages, exit codes, elapsed times, tools, Git revision/dirty state, module-file hashes and completion state. A missing final completion record does not establish success.

Normal Go/module, npm and Buf caches remain in place. The runner does not clean caches. `-count=1` disables Go test-result reuse for the Go test stages; compilation can still reuse build caches. Tool versions, host load, installed dependencies and cache state affect timing.

## Preserve local edits

Format checks call `gofmt -l`, never `gofmt -w`. The scan includes tracked and nonignored untracked Go files. Correct reported formatting deliberately after inspecting the diff.

Protobuf generation runs in a disposable directory with copied proto/module/configuration inputs and the existing TypeScript plugin. The runner compares generated paths and bytes against the working tree, then removes the disposable directory. Changed local generated files stay unchanged, and missing or extra generated files fail the check. Proto input changes during generation also fail. Output destinations must match the reviewed relative directories; inspect a generation-configuration change before extending that guard.

The feature-doc stage requires map references and rejects every auditor issue except a `bad-status` warning for a document with exactly one `Status: planned` header. Other warnings remain failures, even when the auditor itself exits zero.

Checks write logs, temporary fixtures and tool caches. They do not format the checkout, regenerate its source files, install web dependencies or build production assets. Existing tests retain their own temporary-file and local-listener behavior.

## Keep release and CI decisions explicit

GitHub Actions runs these entry points on every pull request and every push to `develop` ([`.github/workflows/ci.yml`](../../.github/workflows/ci.yml)). One job runs the fast profile, and one the `vet-full` and race stages of the full profile. The `go-full` stage runs as parallel jobs through [`scripts/ci-go-test.sh`](../../scripts/ci-go-test.sh). Its `SPLIT` list names the slowest packages (`internal/checks`, `internal/api/storeprov` and `internal/store`) and how many jobs each gets; each of those jobs runs the tests whose sorted position falls to it, and one more job runs every other package with `go test`'s default parallelism rather than `-p 1`, since a runner has its cores to itself. The workflow builds its job list from `scripts/ci-go-test.sh matrix`, so changing `SPLIT` changes the jobs. Each job writes its package times and slowest tests to the run summary, prints the output of failed tests and, when it fails, keeps the raw `go test -json` stream as an artifact for 7 days. Together the jobs cover the full profile except `docs`, whose auditor is not in the repository. Run the docs stage locally. A failed fast or vet and race job prints the last 200 lines of each stage log and keeps the logs as an artifact for 7 days. The workflow is **proposed, not yet confirmed**. A separate workflow, [`.github/workflows/vulncheck.yml`](../../.github/workflows/vulncheck.yml), runs `govulncheck` at a pinned version, built with the Go release in the Dockerfile, and `npm audit --omit=dev --audit-level=high` in `web/`, every Monday and on demand; a finding fails the run. The owner chose the weekly govulncheck run on 2026-11-09; the `npm audit` job is **proposed, not yet confirmed**. Neither runs on pull requests or in `verify.py`; `make vuln` runs `govulncheck` locally. A third, [`.github/workflows/fuzz.yml`](../../.github/workflows/fuzz.yml), runs every Go fuzz target for 5 minutes every Tuesday and on demand ([fuzz parsers and boundaries](go-test-tiers.md#fuzz-parsers-and-boundaries)); it is **proposed, not yet confirmed**.

A passing full profile does not certify a release matrix, the schema compatibility matrix (run `make verify-release` or `make verify-compat`; see [upgrade and recovery compatibility](upgrade-compatibility.md)), DuckDB engine upgrade or rollback, Docker architectures, live-deployment behavior, browser layout or extended fuzzing. Run those task-specific checks separately; `scripts/fuzz.sh` runs the fuzz targets. See the [verification implementation record](../research/verification-gates-2026-11-08.md) for executed controls and timing limits.
