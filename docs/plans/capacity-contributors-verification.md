# Capacity range-contributor verification

Board task: `iQvvhCiIVEfVTbC5Xj7pd`. Base: `7fd2d53e`. Status: candidate; policies proposed, not yet confirmed.

## Requirements and implementation

| Requirement | Implemented evidence |
| --- | --- |
| Sort signed measured changes, retain before/after and actual windows | Resources by measured change uses `GetStorageChange`; additive order/type/row selectors apply to the complete bounded ranking before paging |
| Include streams, KV, objects, decreases and resources absent at B | The comparison API supplies endpoint-union incarnations, independent of the latest-collection bounds table; creation identities distinguish recreated names |
| Work on short intervals without daily growth | One-copy byte differences use distinct fresh endpoints; the six-hour growth rule remains separate |
| Show totals, replicas, coverage and residual honestly | Shared API totals and reasons; all types remain in totals when displayed rows are filtered; missing never becomes zero |
| Scoped contributor drill-down | Explore shows before/after, process/configured-replica evidence, two interval charts, historical configuration windows and scoped event/consumer links |
| Link/replay the same selection | Pinned investigation URL, imported/copied selectors and the existing agent request/context pipeline preserve bounds, scope, sort/type and available pins |
| Bounded reads | One explicit 100-row comparison page; no automatic traversal; one selected contributor adds two history series in one batch |

The contributor table is beside the latest-collection bounds/acceptance table. It does not fabricate old limits, findings or acceptance states for resources absent from that collection. Proposed: live contributor comparisons stay frozen until explicit refresh; type/order changes preserve their comparison pins and reset traversal. The bars scale within the current page, with exact signed int64 labels and no severity meaning.

Historical supporting pages/charts do not gain universal pins. Configuration links use the endpoint window with the existing whole-second exclusive-end convention. Consumers read the latest collection in the requested interval. Events and backlog do not establish causation. Resources present only between the two endpoints remain outside this endpoint comparison.

## Checkpoints and receipts

- `40c40495`: public protobuf JSON sorting/filter tests reach runtime RED because the contract is missing. `19b21ca5`: complete ranking/selection GREEN, additive proto and generated code.
- `b443960e`: six UI workflows reach runtime RED. `8becb127`: Capacity contributors/evidence GREEN. Shared rendering helpers avoid separate byte/link/reason semantics.
- `71c8cd5f`: link/copied/agent selection RED. `c1d9a058`: page and exact-context replay GREEN.
- `da8e0282`: changing a type filter and changing it back resurrects an old page; RED. `1fa0a48a`: reset GREEN, with a passing frozen-clock/refresh control.
- `7ba5f700`: recreated names cannot be distinguished through their accessible actions; RED. `c8e6be76`: visible creation identities and unique actions GREEN.

Early UI assertions were corrected to wait for query results rather than the section's loading shell, and to inspect the existing chart's image label rather than assume a heading. Those assertion/setup mismatches are not counted as semantic regression proof.

Focused tests exercise exact bytes above 2^53, deleted KV/new object rows, unknown measurements, signed global sorting, type totals, pinned pagination, frozen-source refresh, scope changes, selector round-trips, invalid enums and recreated identities.

One full web run on `b70469cc` passed: **1,799 tests, 24 existing skips**, 182 files, 67.32s. The later identity-only correction passed the focused page/context set and TypeScript. The earlier full count is not relabeled as a later whole-suite rerun.

Non-UTC race command:

```sh
TZ=Pacific/Honolulu GOCACHE=/private/tmp/nats-storage-go-cache \
  go test -race -p 1 ./internal/api ./internal/api/storeprov ./internal/api/fake \
  ./internal/investigation ./internal/offline \
  -run '^Test(StorageChange|StorageContributor|OfflineStorageChange|EveryReadEffectiveContextRoundTrip)' \
  -count=1 -timeout=5m
```

Passed: API 1.770s, store 7.768s, fake 4.920s, investigation 1.853s, offline 2.221s. Scoped API/investigation vet passed. Feature audit: 43 docs clean. Proto generation used the existing compiler with the install prerequisite bypassed.

Full/focused web commands use a temporary Vite config pointing cache storage to `/private/tmp`, two workers and `--configLoader runner`. The task borrows existing main-checkout dependencies without installation. An earlier borrowed directory was empty; that failed tool lookup was excluded from test evidence. No dependency manifest, migration or check algorithm changed.

## Fix-removal verification

The initial ranking-removal overlay omitted the shared validator and failed compilation. That run is excluded from runtime proof. A corrected overlay retains the validator and reaches the intended ranking/filter assertions (`ranking-corrected.log`). A separate order-only mutation tests signed order independently of filtering.

`/private/tmp/nats-capacity-contributors-removal/results.json` records the removal controls: remove global ranking/filter logic, remove agent projection, remove the Capacity view, remove page-reset logic, remove recreation identity labels. Go overlays never mutate source; UI files are restored in `finally`. Restored focused Go/UI sets pass.

Browser/build/review receipts follow final smoke checks. No full heavy Go, check-engine or broad store-provider run is claimed. No private source, installed server, deployment, push or cluster access is authorized by these checks.

## Final browser findings

Real demo controls passed signed-sort URL updates, full-ranking KV filtering (three matching rows), two interval charts and scoped configuration/event/consumer links. T3 snapshots timed out twice after opening the owned tab; agent-browser fallback was used. A separate response-interception fixture did not apply reliably and is excluded from browser claims; signed/unknown semantics remain covered by API/component tests.

Collapsed-table geometry passed at 360/761/1280 in light/dark with real local fonts. Expanded evidence at 360 failed: document width 369 for viewport 360. The inherited `.cap-history-expanded` width used a viewport-relative query container, extending from x33 to x369. The task's browser assertion captured this runtime RED before the scoped width correction. Screenshot: `/private/tmp/nats-contributors-expanded-overflow.png`.

The scoped chart width fix passes all three expanded viewports. Removing its exact CSSOM rule in the owned browser restores the 369px phone overflow; inserting the same rule restores 360px. Six final light/dark table views and three expanded views pass with real local fonts. Long synthetic process IDs also stay contained. Headline totals explicitly retain all-type/account scope. Receipts: `/private/tmp/nats-capacity-contributors-browser/{real,layout,containment-removal,long-identities}.json`.

Two actual CLI processes imported the pinned KV/ascending contributor URL and then its returned context/continuation: three total matches, distinct page rows, identical endpoint metadata and retained sort/type/pins. Receipts: `/private/tmp/nats-capacity-contributors-cli/`. Browser keyboard activation and native sort/select controls were exercised. No OS clipboard/touch certification is claimed.

Final compact/identity/containment changes passed 24 focused UI/context tests, TypeScript, production build and targeted restored Go tests. The existing bundle warning remains. One Go build emitted a nonfatal module-metadata cache write denial; exit was zero and final assets were verified through the owned HTTP server. The full-web/race counts above retain their original revision scope. No full heavy Go run was added.

Fresh R2 approved executable candidate `c85129ae` against `7fd2d53e`, including the corrected runtime proof and scoped containment changes. No further concrete regression was found. This final receipt records review only; executable source is unchanged. Owned browser/demo/binary cleanup follows integration. Policies remain proposed, not yet confirmed.
