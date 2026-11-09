# Run repository verification

Use these entry points to check a stable working tree before integration. Choose a focused check during editing and reserve the full Go suite for a coordinated validation boundary. The tier selection is **proposed, not yet confirmed** as an owner policy.

## Prepare the tools

Use the Go version required by `go.mod`, its native DuckDB build prerequisites, Node.js and npm from the [development prerequisites](../../README.md#prerequisites), Python 3.9 or later, and `buf`. Install locked web dependencies explicitly before verification. Go protobuf plugins come from the tool declarations in `go.mod`; the TypeScript plugin comes from the installed web dependencies.

Provide the installed feature-docs auditor. The runner discovers the highest numbered `ddenis-core` version in the local Codex plugin cache. On another machine, set `FEATURE_DOCS_AUDITOR` to its `skills/feature-docs/scripts/audit_feature_docs.py` path, or pass `--auditor PATH`. A missing auditor fails verification; the runner never downloads one.

Dependency preparation is separate from verification. The runner does not run `npm ci` or update lockfiles. Go commands can fetch missing modules or a toolchain according to Go's environment settings. For a prepared offline cache, set `GOPROXY=off` and `GOTOOLCHAIN=local` deliberately.

## Choose the scope

Run commands from the repository root:

| Entry point | Checks | Excluded work |
| --- | --- | --- |
| `make verify-fast` | Go format, gate/build helper tests, TypeScript, protobuf lint/format/drift, feature docs, scoped Go vet, curated pure Go tests, complete web tests | Database/NATS Go fixtures, scale tests, targeted race tests, production build |
| `make verify-full` | Shared static checks, all Go vet, complete web tests, uncached full Go suite, targeted API/Kubernetes race tests | Explicit environment-sized scale cases, extended fuzzing, release/container matrix, production build |
| `make verify-web` | TypeScript and complete web tests | Go, protobuf and documentation checks |
| `make verify-proto` | Protobuf lint, format and isolated generated-code comparison | Other tests |
| `make verify-race` | Selected API pagination/budget/cancellation and Kubernetes watch recovery races | Broader package or full-repository race coverage |

The fast profile avoids database and local-server Go fixtures; it still runs the complete frontend suite. It is broader than `make test-fast`, which runs only the [curated pure Go tier](go-test-tiers.md). Neither `-short` nor the fast profile establishes a runtime limit.

The full profile runs its expensive stages sequentially. Its Go command is `go test -p 1 -count=1 -timeout 40m ./...`: the timeout applies per package, with no aggregate wall-clock timeout. Do not run it alongside database-heavy tests or browser measurements. Persisted fuzz seeds run when their package's ordinary Go tests include them; the profile does not start an extended fuzz session. The subjects scale fixture still requires its explicit environment variables.

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

No CI provider or schedule is adopted by this change. A future CI workflow should call these same entry points after preparing pinned tools and dependencies. CI provider, schedule and artifact retention remain **proposed, not yet confirmed**. `govulncheck` remains the documented manual command; no gate runs it implicitly.

A passing full profile does not certify a release matrix, DuckDB upgrade/rollback compatibility, Docker architectures, live-deployment behavior, browser layout or extended fuzzing. Run those task-specific checks separately. See the [verification implementation record](../research/verification-gates-2026-11-08.md) for executed controls and timing limits.
