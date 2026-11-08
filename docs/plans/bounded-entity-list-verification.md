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

## R2 metadata follow-up

Status: proposed, not yet confirmed. The focused branch starts at approved `62c567cb` and fast-forwards through approved candidate `44685abe`. It contains no root integration changes. The parent owns fresh R3, board updates, and integration. The technical-writing and feature-docs skills govern these documentation updates.

The unchanged R2 probe is `TestReviewerHiddenBucketMetadataBound`, SHA-256 `25c99104eb1886feec1197faf83a4d8876810d24b4483db0ded4a7d922ac4f63`. Its initial executed RED reported 20,002 placement-map identities while the default summary-only stream list succeeded with one match. Commit `442ecd89` records the unchanged probe before the fix. `10158d99` records the focused GREEN implementation. Removing the complete R2 implementation reproduces that runtime failure (0.73 seconds for the probe); restoring the implementation returns one metadata identity and one match (0.68 seconds). Compilation/setup failures are not counted as RED.

### Ranking-to-enrichment audit

The audit follows first capture, continuation, summary-only discovery, account filters, and included/excluded KV/object streams:

- Collection lookup and retained-collection validation read one scalar row. Context capture freezes order, base summaries, ranking rates, and totals, with the existing 20,000-row / 2 MiB context policies. It remains separate reads rather than a database snapshot.
- Stream base facts filter account and hidden buckets in SQL. Configuration disagreement, replica counts, and durable/ephemeral counts are SQL aggregates joined to those base keys; they do not become unrestricted application maps. Stream and consumer base projections have a SQL output/scanner cap of 20,000 rows plus an unscanned overflow sentinel, and an 8 MiB cumulative encoded-payload budget. This also prevents large base strings from relying solely on the final context cap.
- Stream placement receives the retained base ranking keys in both server/config-report and leader-peer queries before either map is built. The two queries share 100,000 rows / 8 MiB of encoded projections. The asset map admits at most 20,000 identities. Placement construction separately caps total squares before allocating configured-but-unnamed missing replicas. Unknown/current/offline/lag values remain nullable; overflow returns a resource error, never partial or invented healthy placement.
- The ordinary no-prefix/no-explicit-stream `streamReplicas` helper now means visible streams. Existing bucket, placement-matrix, and summary-tile callers explicitly use `streamReplicasAll` to preserve their all-asset scope. Explicit stream and bucket-prefix reads remain valid. Shared replica readers inherit metadata overflow errors; this does not paginate or bound their whole endpoints.
- Stream inventory reads aggregate expected consumer counts and named-stream flags only for retained keys. Consumer inventory resolves account aliases, filters the requested account/stream/consumer in SQL, projects only summary fields, and uses the 20,000-row bound. The present/missing union checks its size before inserting a missing consumer. Full inventory configurations and filter-subject arrays are absent from list capture.
- Finding status reads use retained entity keys. Stream keys also select their consumer findings. The projection contains only ID, check code, entity, severity, episode start, and mute existence. SQL evaluates current/past firing state and active whole-check/exact-entity mutes. List capture no longer materializes finding details, related configs, cause events, acknowledgements, or whole mute lists. Existing detailed-finding readers retain their contracts. Parity tests compare status and code order with the legacy reader for current and past ranges with active mutes.
- Check-run status uses the same bounded projection reader. Finding status, inventory, check runs, and page account names each have a separate 100,000-row / 8 MiB budget; base summaries and consumer inventory use 20,000 rows instead. These are proposed policies, not an aggregate endpoint-memory budget.
- Real-provider account decoration selects only accounts on the returned page through an optional provider interface. Other providers retain the existing interface. Metadata failure rejects a real-provider list page rather than omitting names. The final decorated 512 KiB page check still applies.
- R1 delivery-rate SQL and its strict production scanner remain unchanged: at most 20,001 returned rows and rejection before inserting row 20,001. The ordinary inspection helper still permits its bounded sentinel. Historical-only identities cannot bypass the strict production path.
- Selected-page stream history still uses two batched key-restricted queries for at most 100 candidate summaries. Summary-only discovery performs neither history query. History-test spies now distinguish metadata key restrictions from history key restrictions. Continuations reuse ranking and only validate the retained collection, enrich the selected page when requested, and decorate selected account names.

The metadata reader restricts cumulative SQL wire payload bytes, not just each row's bytes. SQL returns a bounded payload prefix and one empty overflow sentinel carrying its original byte size. The scanner checks row overflow before scanning and byte overflow before decoding or retaining the payload. A small-budget test independently executes captured SQL and checks row count, cumulative wire bytes, sentinel substitution, visitor count, and budget sharing across queries. Database joins, sorting, aggregation, and intermediate JSON encoding remain outside the application-materialization bound and subject to the existing deadline/cancellation. No database-engine memory or runtime guarantee is claimed.

### Adversarial checks and limits

Tests cover one visible stream beside 20,001 hidden KV/object metadata identities; visible/hidden/account/empty scopes with both summary modes; unchanged truthful match counts; existing chosen-leader, disagreement, offline, missing, and unknown placement cases; 100,000 peer reports for one key; an oversized config hash; an extreme configured replica count; oversized cumulative base projections; filtered and oversized missing-consumer inventory; large unused inventory/finding content; scoped account names and typed HTTP overflow; current/past finding parity; cancellation; and exact production query arguments and SQL output limits.

The unchanged R1 Go probe remains SHA-256 `4ed031b3b4e7348915f2e3f872d56a6a8d5334acd107b1a1285b49c762c12e85`; the unchanged UI probe remains `2d6db44f18c2002f1e4d8ae83326cd65e5dd330d50607d93e6408cc4c07594b6`. The UI probe and real-page refresh cases pass together: two files, nine tests. No frontend source, proto, generated contract, migration, dependency, slow-consumer RPC, or send limit changes. No dependency installation ran. A temporary symlink used the existing matching private dependency cache for those UI probes and was removed.

The focused API/fake/store-provider selection passes, including real/demo HTTP conformance, both unchanged Go probes, metadata SQL/scanner tests, context traversal, history scope, placement matrices, delivery-rate semantics, snapshot summaries, and finding/mute regressions. Focused `go vet` passes. The final `TZ=America/Los_Angeles` selection passes in 10.314 seconds; the focused race selection passes in 4.998 seconds for the store provider and also passes the API context-concurrency test. The final focused HTTP/store-provider selection passes in 28.794 seconds for the store provider; API and fake-provider selections pass too. These durations identify correctness runs, not comparative performance measurements. Logs are recorded with the final verification artifacts. Initial sandbox loopback bind refusals are infrastructure failures, not behavioral evidence.

Artifacts are private synthetic test logs under `/private/tmp/nats-r2-evidence-9d79e216`. The original R1 artifacts remain untouched. No full Go/checks/broad store-provider suite, full web rerun, benchmark, deployment, live/private data source, or board write ran. The historical benchmark above remains its original enrichment-only measurement and is not a measurement of this metadata revision. Dedicated bucket-list pagination and `GetStream` embedded-consumer response gaps remain open; neither the shared metadata caps nor these tests close those gaps.
