# Global metric ranking verification

Base: `a79b91d3`. Task `bXMlS0kOLTauMBDTEL280` stays in progress for its broader remaining acceptance. This change adds native global ranking and a separate shared panel; it does not relabel the existing loaded-history table sorting as global.

## Scope and implementation

- Additive `ListMetricContributors` RPC, `MetricContributor` and `MetricRanking` contracts. Existing field numbers and RPC descriptor positions are retained; the new RPC is appended and explicitly admitted to the read-only method boundary.
- All eighteen server/stream/consumer catalogue metrics use the exact existing observation SQL/scanner. Candidate selection reads historical reports across the full interval. One bounded identity query precedes one native correlated summary statement; no per-candidate series RPC or history materialization runs in application code.
- Time/filter values remain parameters; the only substituted SQL fragments are trusted correlated entity-column names. Identity selection and summaries share a read transaction. Captures are ranked before paging, preserving exact integer order, unknown/nonfinite-last behavior and deterministic key ties.
- Reuse the existing five-minute/eight-context pager with metric rows and source clock. Proposed, not yet confirmed: 20,000 identities / 2 MiB serialized capture; 100 rows / 512 KiB response; UI twelve rows; observation interval at most thirty days. Overflow errors rather than silently limiting a global rank. Native engine work and aggregate in-flight memory are not hard-bounded by those application limits.
- Legacy stream/consumer snapshot hashes keep their old components; metric captures add their own deterministic payload. Validated immutable sources rehydrate after reopening. No new database files, migration, dependency or collection behavior.
- UI panels cover fifteen metric IDs on server/resource comparison pages. Scope, metric, statistic and order are explicit; resource name filtering can narrow an oversized account. Ranking choices/range are shareable; the cursor is local. Links open typed selected history or diagnostics with fixed capture bounds. Individual histories/findings remain separate reads.
- Existing specialist storage/detection views remain separate. Complete volumes, resource sequence/incarnation semantics, aggregate historical attribution and universal pins are not claimed. Browser ranking controls are not automatically a copied agent request; explicit JSON and returned exact machine context are supported.

## Runtime regressions and controls

- Native RED checkpoint `85e7f0b5`: global peak beyond the initial alphabetical page and historical/account-scoped resource ranking both fail at runtime against the stub. Shared UI/client RED `37c74d24`: three failures / one pass. Resource filter RED `4ce918f3`: one failure / one pass before its form exists.
- Complete native implementation removal reproduces both runtime failures. Removing global sorting reproduces exact-order failure above 2^53. Escaping the capture transaction produces a concurrently updated `orders-00000` peak of 999999 instead of the snapshot's `orders-00001`. Restored native tests pass. Logs: `/private/tmp/nats-ranking-removal/`.
- Removing the shared panel gives a runtime missing-control failure; restoration passes thirteen focused tests. Independently removing only the resource name-filter form gives one failing / one passing component case; restoration passes both. Final focused client/panel/server/resource suite: **four files, fourteen tests pass**. Logs: `/private/tmp/nats-ranking-ui-{red,removed,restored,final}.log`, `nats-ranking-filter-{red,green,removed,restored}.log`.
- Every entity metric's ranking summary matches its direct single-series observation summary, including rate/counter, replica bytes, unknown values and slow-consumer totals. Exact integer sorting, unknown/nonfinite order, frozen continuation despite writes, source/scope changes, read-only admission, handler range/filter/token limits, row/byte overflow, narrowing recovery and cancellation are covered.
- A 1,200-stream retained-history fixture returns the correct top ten of 1,200. This is a correctness/scale guard, not a production latency benchmark. Live HTTP/offline parity covers CPU, exact traffic counter and stream bytes; a closed offline source continues its ranking after full close/reopen.

## Executed verification

Go commands use `GOCACHE=/private/tmp/nats-storage-go-cache GOPROXY=off`. Synthetic HTTP fixture tests required approved loopback permission.

```sh
TZ=Pacific/Honolulu go test -race ./internal/api ./internal/api/storeprov ./internal/api/fake ./internal/offline ./internal/investigation -run 'TestMetricContributors|TestDemoMetricContributors|TestEntity.*(Immutable|Scope|Budget|Expired)|TestOfflineLiveParity$|Test.*PageSize' -count=1 -timeout=5m
TZ=Pacific/Honolulu go test -race ./internal/api/storeprov ./internal/offline -run '^TestMetricContributorsShareOneReadBoundary$|^TestOfflineMetricRankingReopenContinuation$' -count=1 -timeout=3m
go test -race ./internal/api ./internal/api/storeprov ./internal/api/fake -run '^Test(Entity|ImmutableEntity|StoreEntityListContract|DemoEntityListContract|ConsumerListScale|StreamPageCancels|StreamNameDiscovery)' -count=1 -timeout=5m
go test ./internal/investigation -run '^TestPageBudgetReachesEntityRPCAndResponse$' -count=1 -timeout=2m
go vet ./internal/api ./internal/api/storeprov ./internal/api/fake ./internal/offline ./internal/investigation
make -o web/node_modules/.package-lock.json proto
npm --prefix web test -- --configLoader runner --maxWorkers=2
npx --prefix web tsc --noEmit -p web/tsconfig.json
cd web && npx vite build --configLoader runner --outDir /private/tmp/nats-ranking-ui
```

All listed focused Go checks pass, including existing stream/consumer traversal and immutable contracts. Proto lint/format/generation, final TypeScript/build, scoped vet and the 46-capability audit pass. Full web: **204 files, 1912 passed, 24 existing skips**, before the small name-filter/timestamp refinements. Final refinements are covered by fourteen focused tests, final types/build and native browser checks; the full suite was not repeated for that delta. The existing >500kB bundle warning remains.

Setup errors are excluded from runtime RED claims: the initial buf formatter used two positional paths; a test helper name collided with an existing helper; the client briefly compared init timestamps with message timestamps before using resolved numeric bounds. All were corrected. No compile/setup failure is presented as behavioral proof. No full Go/check-engine/broad store-provider suite or benchmark ran.

## Browser acceptance

A disposable in-memory native store and production assets use only generic synthetic names on loopback. T3 keyboard Enter opens ranked `nats-15` (160%, outside the first twelve alphabetical names) with exact fixed bounds; history Back restores ranking choices. Native Next page goes from twelve of sixteen to four of sixteen. Consumer ranking preserves APP/orders and changes order correctly.

T3 snapshot timed out once; targeted DOM evaluation and native pointer/keyboard actions still worked. Two 360px resize calls both reported/retained actual 1280px, so phone claims use the installed isolated browser fallback. Actual 360px viewport/document stay 360px; controls fit, native key filtering narrows 25 consumers to five matching `worker-2`, source/account/stream/range remain in the URL, and the table scrolls within its 328px region. Fonts load. Physical touch was not independently tested; existing scroll-region keyboard policy is reused.

Phone inspection found a timestamp split into three lines. The final semantic `time` markup and scoped nowrap rule retain one complete label. Removing that coupled markup in the real browser restores three lines; restoring it returns one line (document stays 360px throughout). A style-tag-only override did not change layout and is excluded as an ineffective control. Receipts: `/private/tmp/nats-ranking-phone.json`, `nats-ranking-phone-final.png`, `nats-ranking-time-{red,proof}.json`.

No private 18900, existing database/config/log/archive, cluster, authentication or deployment was read or changed. Owned fixture/browser processes and temporary dependency link are cleaned before handoff. Fresh read-only review remains required before integration.
