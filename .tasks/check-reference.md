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

## R2 implementation on the current parent

Integrated candidate `93e6aea1d06820de94a861275639326e57b29761` (original base `345f405c178307d7187c23bd5d85edd31760ef2d`) by `git cherry-pick 345f405..93e6aea1d06820de94a861275639326e57b29761` onto `e99c72f4b71f74e8e52b25cb8b2792bc195c4deb`. All eleven commits applied without conflict, ending at `eda763c`. The approved parent remains an ancestor. Storage and Events changes are preserved; the complete frontend run below includes their tests.

Proposed, not yet confirmed: keep every labelled authored reference paragraph, including numeric lists, rather than risk dropping holding or resolution behavior. The reference extractor now does this. `client-007` retains young reconnected clients, partial-collection explanations, resolution on the tenth consecutive partial collection without an unnamed client, and immediate resolution after a complete collection without one. The numeric default remains separately visible.

`stream-008` describes differing metadata and boundary/count comparisons without inferring rebuild history, message identity or guaranteed repair. Its description, remediation and authored feature reference qualify the evidence. The scenario uses reported timestamps and matching counters rather than an assumed rebuild. The related creation-metadata audit covered `consumer-009`, its authored reference, and demo findings, timeline, events and creation-source helpers. No `stream-008` demo finding exists. The consumer demo already reuses shipped guidance; its existing regression passes. The separate `stream-007` configuration demo is outside these creation-metadata fixes and was left unchanged.

### R2 runtime RED and removed-fix verification

- Before production edits, ran `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./docs ./internal/checks -run 'TestReferenceRetainsEveryAuthoredParagraph|TestReferenceClient007RetentionAndResolution|TestReferenceStream008DoesNotInferHistory|TestStreamCreationReferenceDoesNotInferCauseOrPromiseRepair' -count=1`. Runtime RED for every omitted Thresholds paragraph, the missing client holding/resolution behavior, and unsupported stream claims. Log: `/tmp/checkref-r2-red.log`. Committed the executed regressions as `2d95bd5`.
- Ran `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./docs ./internal/checks -run 'TestReference|TestStreamCreationReference|TestCreationReference' -count=1`: GREEN. Fix committed as `6c6021c`.
- A Python `try/finally` restored only `docs/check_reference.go`, `checks/stream-008.sql`, and `docs/features/stream-checks.md` to integration commit `eda763c`, ran the exact first command again, observed exit 1 for both findings, restored the fixed files in `finally`, then reran it with exit 0. Logs: `/tmp/checkref-r2-removed-red.log`, `/tmp/checkref-r2-restored-green.log`.
- Broad extraction coverage compares every labelled paragraph in all embedded check references against the output. Rendered frontend regressions read shipped authored source and assert full `client-007` rules alongside concise meaning and numeric defaults, plus qualified `stream-008` meaning. HTTP regression checks that client behavior reaches the real catalog response.

### R2 final checks

- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./docs ./internal/api ./internal/checks ./internal/api/fake -run 'TestReference|TestStreamCreationReference|TestCreationReference|TestCheckCatalog|TestCreationFinding' -count=1`: all four packages pass, including the final authored-doc adjustment.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./docs ./internal/api -run 'TestReference|TestCheckCatalog' -coverpkg=./docs,./internal/checkref -count=1`: docs reports 100%; API exercises combined reference/catalog coverage at 98.2%.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go test ./internal/checks -run 'TestStreamAgreement|TestConsumerAgreement|TestUnnamed' -count=1`: pass, 16.515s; includes client partial holding and its bounded end.
- `cd web && npx tsc --noEmit && npm test -- src/pages/Checks.test.tsx`: pass, 12 tests.
- `cd web && npm test`: first full run had one timeout waiting for the existing catalog-route link (1 failed, 1,422 passed, 24 skipped). The focused file passed before and after. `cd web && npm test -- --maxWorkers=2` then passed all 142 files: 1,423 tests passed, 24 existing skips, 65.03s. No timeout or production change was made to conceal the first run.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache go vet ./docs ./internal/checkref ./internal/api ./internal/api/fake ./internal/checks`: pass.
- `GOCACHE=/tmp/nats-lens-checkref-go-cache make build`: pass; existing Vite size warning and harmless denied module-stat-cache write warning. After the final doc-only adjustment, `GOCACHE=/tmp/nats-lens-checkref-go-cache go build -o bin/natslens ./cmd/natslens` also passed.
- `python3 /Users/denis/.codex/plugins/cache/ddenis/ddenis-core/0.4.0/skills/feature-docs/scripts/audit_feature_docs.py docs/features/ --map-refs require`: clean, 39 capability docs, both index sections updated. `git diff --check`: clean.
- All 85 detection SQL bodies after `*/` are byte-identical to the candidate. `stream-008` threshold metadata is byte-identical as well. The `stream-008`, `consumer-009` and `server-009` bodies also match original base `345f405`. Stream body SHA256: `9e8f8cb54e43b79e7bfc022393464daef63a18ce0b620a9b930a3ae7c7764331`.
- `git diff 93e6aea1d06820de94a861275639326e57b29761 -- proto internal/gen web/src/gen internal/store/migrations` is empty. Proto/generated code was not changed or regenerated; no migration was edited.

### R2 synthetic browser and limits

Built fixed demo only: `bin/natslens demo -fixed -listen 127.0.0.1:18457`. No live NATS, port 18900, private configuration/database, external board, installation, push or deployment. Existing installed node_modules were copied from the main checkout; no package installation ran.

T3 preview attempted first (`preview_status`, `preview_open`, `preview_navigate`). `preview_snapshot` failed twice at the same step with raw error: `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` Used the installed agent-browser skill and `checkref-r2` session after those failures.

`python3 /tmp/checkref-r2-browser.py` passed nine page/width combinations: `client-007`, `stream-008`, `consumer-009`, each at 360/761/1280 CSS px. Document width equals viewport; no main-content element overflows. Client holding and resolution rules render fully. Stream message-identity limitation and qualified repair guidance render. No unsupported rebuild, identical-message or guaranteed-resynchronisation claim appears. Demo configuration is labelled; finding links keep system and range. Catalog count remains 88. Browser errors are empty. An initial script assertion used an unknown account filter, which the shell removed; corrected the verification to test the requested system/range without that unrelated invalid filter.

Screenshots: `/tmp/checkref-r2-{client-007,stream-008,consumer-009}-{360,761,1280}.png`. Visually inspected the client 360 px and stream 761 px full-page captures. Task-owned fallback browser and T3 tab are closed; fixed demo stopped.

No heavy Go suite or `TestBudgetRotates` rerun: its independently established identical base/candidate failure remains outside scope. Fresh R2 read-only review belongs to the parent; no reviewer was spawned and this implementation does not self-approve.
