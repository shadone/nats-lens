# Check reference pages

Base: `345f405c178307d7187c23bd5d85edd31760ef2d`.

Design proposed, not yet confirmed: expose the embedded definition catalog through Connect, supplement it with the existing per-check feature reference, and keep selected-system configuration separate. Default reference never includes private remediation extensions. The shared Topology classifier explanation is included from its canonical feature doc. No second hand-authored catalog is maintained.

- [x] Inspect sources and routes
- [x] Runtime RED for catalog and metadata correction
- [x] Definition API, all 88 references, configuration isolation
- [x] Catalog/detail routes and contextual links (including mobile finding card)
- [x] Focused RED/GREEN, browser and broad verification
- [x] Feature docs audit and focused commits

Parent owns fresh read-only review. No reviewer spawned and no self-approval. No external board calls.

## Runtime RED and restored GREEN

All commands below ran. The early sandbox setup failures are excluded from RED evidence.

- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./internal/api -run '^TestCheckCatalogHTTP$' -count=1`: initial runtime RED, HTTP 404; restored GREEN with the API implemented.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./internal/checks -run '^TestCreationReferenceDoesNotInferCauseOrPromiseRepair$' -count=1`: initial runtime RED for inferred rebuild/repair claims; GREEN with corrected header. Replacing the SQL file temporarily with base `345f405` reproduced runtime RED; restoring it reproduced GREEN. A later RED/GREEN cycle preserved the ahead-tolerance ratio's leader-position basis.
- `cd web && npm test -- src/pages/Checks.test.tsx -t 'opens the catalog route'`: temporarily removing only the catalog route reproduces runtime RED (reference link absent); restoring it passes. The final shell fixture supplies synthetic shell status.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./docs -run '^TestReferenceSelfContainedPlainText$' -count=1`: runtime RED for literal Markdown links and missing numeric slow-route classification; GREEN after canonical explanation reuse and plain-text conversion. Replacing the parser temporarily with its `0b69a0f` version reproduces RED; restored parser passes.
- `cd web && npm test -- src/pages/Checks.test.tsx -t 'concise authored meaning'`: runtime RED for repeating the verbose header; GREEN after using the existing concise Problem paragraph.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./internal/api/fake -run '^TestCreationFindingUsesShippedGuidance$' -count=1`: runtime RED for demo's stale step-down promise, GREEN after reusing shipped guidance. Temporarily restoring the old finding implementation reproduces RED; restored fix passes.

Temporary removal checks restore files in Python `finally` blocks. No production edit from those checks remains.

## Verification

- Final frontend: `cd web && npx tsc --noEmit && npm test` — 141 files passed; 1,401 tests passed, 24 existing skips.
- Final full API/provider packages: `GOCACHE=/tmp/nats-lens-checkref-go-cache go test -short ./docs ./internal/api ./internal/api/fake` — passed (API 21.742s, fake 32.526s).
- Focused backend: `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./docs ./internal/api ./internal/checks -run 'TestReferenceSelfContainedPlainText|TestCheckCatalog|TestCreationReferenceDoesNotInferCauseOrPromiseRepair' -count=1` — passed.
- Detection and wording: `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./internal/checks -run 'TestCreationReferenceDoesNotInferCauseOrPromiseRepair|TestConsumerAgreement|TestSlowConsumersAndStalls' -count=1` — passed.
- Coverage: `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./docs ./internal/api -run 'TestReferenceSelfContainedPlainText|TestCheckCatalog' -coverpkg=./docs,./internal/checkref -coverprofile=/tmp/nats-lens-checkref-coverage.out -count=1` — 98.1% combined statements (`CheckReferences` 100%, `Response` 96.2%). Frontend coverage instrumentation is not installed; no package was installed for it.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go vet ./...` — passed.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache make build` — passed. Existing Vite bundle-size warning; Go may print a denied module-stat-cache write warning but exits zero with the temporary build cache.
- `make proto SHELL=/bin/bash` with `GOCACHE` and a shell `npm` wrapper: the `npm ci` step verifies the copied existing `protoc-gen-es` dependency instead of installing packages; `buf lint`, `buf format --diff --exit-code`, and `buf generate` all execute successfully. Generated Go/TypeScript files are committed.
- `python3 /Users/denis/.codex/plugins/cache/ddenis/ddenis-core/0.4.0/skills/feature-docs/scripts/audit_feature_docs.py docs/features/ --map-refs require` — clean, 39 capability docs; both index sections updated.
- `git diff --check` — clean.
- Detection bodies after `*/` in `checks/consumer-009.sql` and `checks/server-009.sql` are byte-identical to base. No migration changes.

## Broad Go result and limitation

`GOCACHE=/tmp/nats-lens-checkref-go-cache go test -short ./...` ran with permission for local synthetic test listeners. Every package passed except `internal/checks`, whose only failing test was `TestBudgetRotates` (`engine_test.go:372`: both epochs give `stream-901` the budget). It fails alone as well. The identical standalone command fails identically in an untouched `git archive 345f405` export at `/tmp/nats-lens-checkref-base-345f405`. This pre-existing engine-test failure is left outside scope. The broad `internal/checks` run completed in 568.512s; `storeprov` passed in 475.770s. The in-process cluster tests omitted by `-short` were not run.

The initial unprivileged broad run hit sandbox HTTP-listener restrictions and was stopped before the permitted run. It is not reported as product-test evidence.

## Synthetic browser verification

Only task-owned fixed demo processes on `127.0.0.1:18455` and `:18456` were used. No live NATS, port 18900, private configuration/database, installation, deployment, restart, or push.

T3 preview first: `preview_status`, `preview_open`, navigation. Snapshot failed twice with identical raw error: `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` Used the installed agent-browser fallback after those failures.

- Catalog: all 88 entries, 360/761/1280 CSS px, document width equals viewport and no main-content element exceeds it.
- Long `consumer-014` reference: 360/761/1280 px, no horizontal overflow; concise meaning replaces repeated long introduction.
- Keyboard: focus search, native keyboard typing, Tab through entity filter/results, Enter opens reference; system/range/account preserved; new-route focus is `main`.
- Mobile finding: direct `consumer-009 · Check reference` link opens correct check with system/range retained.
- Unknown code: explicit heading and catalog return link.
- Slow routes: complete shared classifier, including 2 ms floor, present; no literal source Markdown link remains.
- No page errors reported by fallback browser.

Screenshots in `/tmp`: `checkref-final-catalog-360.png`, `checkref-final-creation-360.png`, `checkref-final-long-360.png`, `checkref-final-long-761.png`, `checkref-final-long-1280.png`.

All 88 checks have check-specific authored reference content sourced from the existing headers and feature docs; no authored check page is missing. API tests enforce count, built-ins, nonempty reference, defaults, overrides, disabled/unavailable/demo states, unknown-system rejection, no private remediation leakage, and system isolation. UI tests cover unknown code, links, keyboard controls, errors/retry, configuration distinctions and clearing another system's settings during a switch.

Task-owned synthetic demo processes and fallback browser are stopped after verification. Final working tree contains only this evidence update before its commit.
