# Bounded entity list verification

Status: proposed, not yet confirmed. The task branch implements the contract below; fresh review and integration belong to the parent task. These measurements do not establish production latency.

## Source and contract

The implementation starts from `62c567cb`. The measured source is `b8b8531b53c6e2b85eaef9c0693b59f0e5ce7826`, on `t3/bounded-stream-list-pagination`. The originating historical audit was not restored.

The [list contract](../reference/entity-list-pagination.md) documents compatibility and reset behavior. Both providers implement additive stream and consumer cursor fields. Zero limit uses 100 rows, larger limits use 100, and negative limits return `InvalidArgument`. Both encodings have a 512 KiB response budget, checked again after account-name decoration. Oversized individual or decorated responses return `ResourceExhausted` rather than dropping fields. Counts and stream totals cover all filtered matches, including later pages. Hidden KV/object streams and their consumers are excluded from default stream totals.

Each provider retains at most 8 contexts, shared between stream and consumer lists. Each context has at most 2 MiB of encoded summary data and 20,000 ranking rows. Application scans stop after the observed-row limit; SQL aggregation work itself is not bounded by that row count. Contexts expire after 5 minutes, can be evicted earlier, and disappear on process restart. Expiry, eviction, and retention removal of the pinned collection return `FailedPrecondition`. Invalid cursors and changed normalized filters, RPC, system selector, or explicit range return `InvalidArgument`.

The context freezes the captured order, base summaries, consumer rate ranking, and totals. It is not a transactional database snapshot. First capture reads facts, findings, and inventory separately. Selected-page stream history and readable account names remain reads from retained data. No persistent schema, migration, write path, existing metric, slow-consumer RPC, or message cap changes.

## RED and GREEN evidence

- `ea0a0f9b` records the executed runtime RED: `TestStreamListZeroLimitIsBounded` returned 1,000 rows rather than 100. A setup failure from the protected default Go cache occurred first and was not counted as RED; the executed reproducer used a private cache.
- `0f305395` records the focused GREEN implementation. Scale, cancellation, HTTP contracts, targeted UI, typecheck, and the feature-doc audit passed before that commit.
- Removing the selected-key arguments from both page-history queries made `TestEntityListScaleAndEnrichmentScope` fail at runtime: `enriched 0 entities for page of 10`. This means the queries received no selected-key restriction, not that they read zero entities. Restoring the arguments made the same test pass.
- `50297c6b` adds the executed new-collection traversal and summary-only conformance cases and corrects stale placeholder expectations. A new collection adds a stream between pages; retained traversal stays on its collection and order, while a fresh traversal sees 1,201 matches.
- `b8b8531b` uses existing button classes and tokens for navigation. Its targeted component tests and production build pass.

## Work and response bounds

`TestEntityListScaleAndEnrichmentScope` uses 1,200 streams and 28 observed seven-day sparkline buckets. Its separate correctness run observed:

- Legacy unbounded response: 2,231,313 JSON bytes, above the unchanged 1 MiB send cap.
- Ten returned streams: 18,234 JSON bytes and 7,449 protobuf bytes.
- First page: 9 counted provider fact queries, including two history queries restricted to the same 10 selected keys. Store-owned finding/mute reads are outside this query-reader counter.
- Continuation: 3 counted fact queries, one retained-collection check and two selected-key history batches. It reuses captured ranking and totals.
- Summary-only discovery: 100 of 1,200 matches with zero history queries. Search and stream-name choices use this mode and traverse pages explicitly.
- Full traversal: no duplicate or missing keys, stable totals, and byte sums equal the all-match total. New collections cannot shift a retained traversal.

Additional tests exercise byte-constrained shorter pages with a continuation, an oversized single entity, account-name decoration, 20,001 observed ranking rows, malformed cursors, filter mismatch, expiry, eviction, canceled requests before reads, and cancellation during the first history query. Consumer tests traverse all 1,200 matches and verify that continuation does not rebuild ranking.

## Coordinated local benchmark

The parent granted one window at 2026-11-08 01:50:18 UTC. Other task owners acknowledged idle, and the parent started no verification work during the batch. This is coordination among the known tasks, not proof that the host had no other activity. Earlier correctness runs overlapped other ordinary checks and are not latency measurements.

The exact batch ran once, with no automatic rerun or expanded benchmark selection:

```sh
set -o pipefail
/opt/homebrew/bin/gtimeout --signal=TERM --kill-after=2s 118s env GOMAXPROCS=2 GOCACHE=/tmp/natslens-pagination-gocache go test ./internal/api/storeprov -run '^$' -bench '^BenchmarkStreamListPageEnrichment$' -benchtime=3x -count=3 -benchmem -timeout 2m | tee /tmp/natslens-pagination-benchmark.txt
```

The outer guard bounds execution to 118 seconds plus a 2-second kill grace. It did not fire. The batch passed; Go reported 6.961 seconds for the package, including benchmark fixture and harness work. Compilation is outside that package duration and per-operation timing. The private build cache was warm from correctness checks; a cold build was not measured.

The host was Darwin arm64 on an M2 Pro, with `GOMAXPROCS=2`. Each benchmark invocation opens a fresh in-memory DuckDB with 1,200 streams: 29 six-hour historical collections, followed by eight hourly collections, over a seven-day selected range. Fixture construction and initial collection lookup happen before `ResetTimer`, outside per-operation timing and allocations.

The legacy case calls `streamRows` for every stream, applies status, and limits to 10. The page case calls the provider's `ListStreams` with limit 10 and no token, creating a fresh context on each iteration. Thus its cost includes ranking/base totals, context creation, collection validation, and page enrichment. It is not a continuation benchmark. Neither case includes HTTP, account-name decoration, client rendering, or response serialization. This is a synthetic comparison of provider-side list/enrichment work, not end-to-end RPC or production latency.

Raw observations:

```text
legacy1200: 3 iterations, 84280903 ns/op, 44256936 B/op, 1260195 allocs/op
legacy1200: 3 iterations, 84239097 ns/op, 44257128 B/op, 1260196 allocs/op
legacy1200: 3 iterations, 89000167 ns/op, 44256493 B/op, 1260193 allocs/op
page10:     3 iterations, 40142750 ns/op,  2413026 B/op,   63469 allocs/op
page10:     3 iterations, 40968695 ns/op,  2412794 B/op,   63470 allocs/op
page10:     3 iterations, 39947611 ns/op,  2413194 B/op,   63471 allocs/op
```

The custom `enriched_entities/op` metric did not print because the benchmark reports it before `ResetTimer`, which clears it. No timing rerun was performed to repair that output. The executed query-argument tests above independently prove two 10-key history reads; the benchmark output is not used as evidence for entity counts.

## Caller audit and remaining gaps

- Streams and Consumers pages request one bounded page, show the full match count, and reset on system, account, filter, or range changes. Continuations stop live polling. Refresh list starts again. Different-range placeholder rows are discarded.
- Overview already requests 10 worst streams and 6 busiest consumers. Its stream count and totals remain all-match values; its finite callers do not enumerate pages.
- Search and stream-filter choices enumerate bounded pages sequentially, reuse absolute bounds, and carry cancellation. Stream discovery omits history. Consumer search retains delivery-rate fields. Search keeps its pre-existing partial-success behavior across entity kinds when one kind fails.
- Shared conformance list helpers enumerate pages. Focused real/demo HTTP contract tests cover traversal, defaults, summary-only behavior, typed invalid requests, and eviction.
- KV/object pages and their search calls use dedicated bucket RPCs. Those calls still compute bounded growth samples for all listed buckets and have no pagination. They are a remaining scalability gap, not covered by the two-RPC response guarantee.
- `GetStream` still embeds consumers; the shared observed-row read limit applies, but its response has no pagination contract. `GetConsumer` remains a single-entity read.
- Capacity and Subjects have their own fact-building APIs. Capacity still calls the separate `streamPoints` aggregate path. Servers, accounts, placement, and other lists retain their existing contracts and bounds or gaps. No global API response-bound claim is made.

## Verification and cleanup

Focused Go tests passed for pagination policies, scale, serialization, read limits, cancellation, real/demo HTTP contracts, stream/consumer ranking, and existing snapshot behavior. The focused race run passed for context concurrency and selected stream/consumer traversal and cancellation. `TZ=America/Los_Angeles` checks passed for selected-page enrichment, the six-hour growth boundary, and Unix-aligned series buckets. Full Go, checks, and the broad store-provider suite were not run; the parent owns the combined run.

The full two-worker UI suite passed: 158 files, 1,632 tests, and 24 skipped tests. Its first run found two old range-placeholder expectations; the corrected placeholder file then passed with 143 tests and 24 skips before the full rerun passed. The subsequent button-class change passed its two targeted tests and the production build/typecheck. The build retains the existing large-chunk warning; sibling route splitting was not merged. The feature-doc audit is clean across 39 capability docs and both index sections.

`make proto` ran with the stale npm prerequisite override, after comparing identical dependency locks. No dependency installation ran. A dependency symlink was replaced by a private clone when Vite attempted a cache write into the shared tree. The Go build cache was private after the initial setup failure; existing module dependencies were reused.

Browser QA used a fixed demo on isolated port 18922. T3 preview opened successfully, but the same snapshot step twice returned `Preview automation snapshot timed out after 15000ms.` The documented agent-browser fallback confirmed 100 of 109 consumers, then the remaining 9 with Next page disabled; removing ephemerals reset to page 1 with 22 matches and retained the system selector. The Streams page showed 8 matching rows and its filtered totals, with local fonts loaded. Visual inspection prompted the existing button-token correction. The T3 preview tab and fallback browser session were closed, the task demo stopped, and port 18922 had no listener afterward. No live deployment or private data source was accessed.

## R1 follow-up verification

Status: proposed, not yet confirmed; the parent owns fresh R2 and integration. The follow-up starts from approved `62c567cb` and integrates the candidate sequence through `0a1da2f6` without either sibling branch.

The unchanged reviewer probes were copied byte-for-byte. `5d26313f` records their executed RED: one current consumer and 20,001 historical rate identities let ListConsumers succeed, and refreshing a continuation made only two requests. `af871738` records their GREEN. The expanded real-page tests then exposed a third request using the old continuation: a child effect ran before React Query updated its observer. `4d44b95d` records that RED and `be959093` its GREEN. Reset refetch waits until observer options update, uses the current scope, and disables navigation and repeat refresh while fetching.

The production rate-ranking query has a bound LIMIT of 20,001. Its scanner rejects the overflow row before scanning or adding it to the map, so the production rate map holds at most 20,000 identities. The unchanged probe's direct inspection helper permits one additional sentinel identity and is itself bounded; no production caller uses that inspection budget. The strict path is shared by ListConsumers, GetConsumer, and GetStream's embedded consumers. A 20,050-historical-identity fixture captures the production query and arguments, independently executes it to prove exactly 20,001 returned rows, checks a small scanner budget, verifies account/stream/consumer scope and cancellation, and observes one batched ranking query. SQL aggregation work is still outside the application materialization bound. Stream and current-consumer base scans already have the observed-row rejection; list stream history remains selected-key only. Inventory and store-owned findings remain separate reads, outside that observed-history ranking guarantee; this is not a global endpoint-memory bound.

Removing the strict production call again made the unchanged Go probe fail at runtime with total_count 1 and no error; restoring it passed the focused suite. Removing all traversal changes made the unchanged UI probe fail again at two requests; restored full UI verification passed. An initial attempt to remove the whole Go helper could not compile the expanded helper test and was not counted as runtime RED.

The page tests use the production 10,000 ms staleTime. Both Streams and Consumers verify first-page refresh, fresh-cache continuation refresh, typed eviction recovery, request tokens empty → old continuation → empty → new continuation, delayed refresh, disabled repeat actions, and changed system/account/range/filter while an old response completes. They assert no extra cross-scope requests. The shared request/query model files were not changed by R1.

Validation passed: the focused API/fake/store-provider selection (including real/demo HTTP traversal contracts, read limits, scale, cancellation, rate semantics, and existing summaries), non-UTC historical-ranking/page-history/growth and Unix-bucket cases, and the focused race selection for context concurrency and traversal/cancellation. Loopback HTTP tests needed sandbox escalation; their initial bind refusal was infrastructure failure, not test evidence. Full web with two workers passed 160 files, 1,641 tests, and 24 skips. Typecheck, production build, and the feature-doc audit (39 docs) passed. The existing large-chunk build warning remains. No full Go/checks/broad store-provider suite, benchmark rerun, dependency installation, proto generation, migration, or send-cap change ran.

Production UI verification uses an isolated fixed synthetic demo on port 18932. T3 preview navigation worked; snapshots twice returned `Preview automation snapshot timed out after 15000ms.` The agent-browser fallback confirmed local fonts, 100 of 109 consumers, page 2, and Refresh returning to 100 rows on page 1. Request-body interception was not used as evidence; exact token/request proof comes from the page integration tests. Dedicated bucket pagination and GetStream's embedded-consumer response gap remain as documented above.

The follow-up browser interception attempt did not produce usable request records; its subsequent wait ended with a busy/unresponsive-daemon connection failure. It is not counted as additional verification. The isolated fallback browser eventually reported `Browser closed`, the T3 preview tab was closed, the fixed demo was stopped, and no listener remained on 18932. No live/private data or deployment was accessed.
