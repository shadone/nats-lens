# Web code splitting, 2026-11-08

The implementation reduces cold Fleet JavaScript by 40.8%, from 1,127,667 to 668,017 bytes. Constrained Fleet readiness improves from 7,768.0 to 5,360.7 ms. First Checks navigation becomes slower because the Markdown parser is deferred. This tradeoff and the candidate navigation-budget misses require the parent's fresh review; integration is not implied.

The [measurement data](web-code-splitting-2026-11-08.json) retains emitted chunk sizes, import edges, module membership, all timing samples, and production HTTP observations. The [measurement runbook](../operations/measure-web-loading.md) defines the measurement semantics. All implementation choices and budgets here are proposed, not yet confirmed.

## Source and method

- Exact before source: `7ec15812ff38`, the clean starting point of branch `t3/lazy-markdown-web-bundle`. Its production code equals `d8af23ef37e0e211bdc131ce13d8f80eeb0d8137`; their differences are the root README and baseline research/runbook documents only.
- Measured implementation source: `d1b3e7cd0f0b862130ce73fda67b733a3ada8cac`. No unreviewed slow-consumer changes were merged. Approved Markdown, bucket details, Capacity growth/types, and shared scroll restoration remain included.
- Host: macOS 15.6.1 build 24G90, arm64. Node 22.21.1, npm 11.7.0, Go 1.27.3, TypeScript 6.0.3, Vite 8.3.2, Rolldown 1.2.12, React 19.3.0.
- Lockfile SHA-256: `4a96e3c9ff19b175d8bc3e9d6024605cc92bd42cb2a01d8892bda100fefc2282`. Dependencies came from a private copy of the existing matching tree. No dependency installation or lockfile change occurred.
- Builds used the normal production configuration plus a private metadata-only observer. Sizes and gzip level-9/Brotli quality-11 estimates came from actual emitted files after `await build()`. No `generateBundle` code-size values were used. A subsequent `npm run build` produced identical SHA-256 values for all 13 JavaScript files.
- Production Go binaries embedded each completed build and served only the generic `demo -fixed` fixture on dedicated loopback ports. No deployment, database, collector, NATS server, authentication configuration, or existing process participated.

The previous [baseline](web-loading-2026-11-08.md) measured source `3a2d0b69`, before the approved shared scroll/types changes. Its 1,124,674 bytes are context, not this implementation's exact before value.

## Emitted bytes and actual transfer

All sizes are bytes. Initial totals include the HTML entry, both module-preload links, their recursive static imports, and every JS resource fetched before Fleet readiness. The browser confirms exactly that closure; no additional dynamic JS loads on Fleet.

| Measurement | Before | After | Change |
| --- | ---: | ---: | ---: |
| Initial JS files | 1 | 3 | 2 more parallel requests |
| Initial JS body bytes | 1,127,667 | 668,017 | −459,650 (40.8%) |
| Initial gzip level-9 estimate | 331,524 | 203,055 | −128,469 (38.8%) |
| Initial Brotli quality-11 estimate | 276,245 | 175,377 | −100,868 (36.5%) |
| Largest emitted JS file | 1,127,667 | 402,707 | −724,960 |
| All emitted JS files | 1 | 13 | 7 route-family entry points plus shared files |
| Largest deferred shared file | none | 166,555 | Event rows and Markdown/GFM |
| First Streams visit: new JS files / bytes | 0 / 0 | 1 / 12,728 | List code only |
| First Checks visit after Streams: new JS files / bytes | 0 / 0 | 3 / 203,333 | Guidance, events adapter, and Markdown |

The initial files are `index-Cjy5T1tS.js` (264,594), `page-support-t30_4_8h.js` (402,707), and `rolldown-runtime-hePW80VL.js` (716). Largest deferred files are `EventRow-DuQOYcz0.js` (166,555), infrastructure (105,387), messaging (59,500), and activity (47,110). The data file lists every file and compression estimate.

Go HTTP returns no `Content-Encoding`, even with `Accept-Encoding: gzip, br`. Every JS response body equals its emitted file size. Thus actual initial encoded JS bodies total 668,017 bytes, not the gzip estimate. Chromium's initial `transferSize` adds 900 bytes for its three response-header allowances; this is not a packet capture. CSS is 116,787 bytes after splitting; the five fonts observed on Fleet remain 100,620 bytes. HTML, CSS, and fonts remain outside the JS total.

The normal 500 kB warning threshold is unchanged. No resulting chunk exceeds it. The improvement is the complete initial closure, not the removal of the warning alone.

## Boundaries and request ownership

Markdown moved unchanged from the basic shared UI module into a dedicated renderer. Its HTML, image, control-character, URL, GFM table, and accessible table-scroll policies remain intact. Every Markdown consumer, including event rows and the phone finding card, uses the dedicated renderer.

Fleet and the shell remain eager. Seven deferred families cover Overview; guidance/findings; resource lists; resource details; infrastructure; messaging; and activity/history. Resource lists have their own consumer-table and pagination helpers, so Streams does not import detail events or Markdown. Primary Streams and Checks reads mount outside the code-loading boundary and start alongside their code requests. Their page hooks reuse the same query keys, so each observed first navigation issues one primary data request. Query and import timing in the data file shows that they overlap.

The first automatic split produced 20 files, including seven shared files below 3 kB, and fetched Markdown on Streams through detail helpers. After helper separation, manual grouping combines identified page helpers and their dependencies into `page-support`. Dependency inclusion stays at Rolldown's default, preserving dependency initialization rather than forcing unrelated vendor groups. No module ID occurs in more than one output chunk. Source maps, warning limits, and dependency versions remain unchanged. An experimental group without recursive dependencies added wrappers and small requests and was discarded.

The selected group increases initial JS relative to the 606,986-byte automatic prototype. It avoids the prototype's seven new JS requests on Streams; the final Streams visit fetches one. Two small deferred shared chunks remain: entity-detail parts (2,327 bytes) and the event adapter (2,690 bytes). Their complete dependency closures are requested in parallel by Vite's preload helper, rather than through a sequence of tiny dynamic imports.

## Comparable browser series

The authoritative before and after series uses one Google Chrome 156.0.8078.48 headless process and Playwright Core 1.56.1. Each sample creates a fresh 1280 × 800 context, blocks service workers, and disables HTTP cache through CDP. Local uses native CPU and unlimited loopback bandwidth. Constrained uses 150 ms latency, 200,000 bytes/s download, 93,750 bytes/s upload, and 4× CPU slowdown. Browser-process startup and OS-cache coldness are excluded. No test or build workload ran during the timing series; separate behavior checks briefly shared the host. Host contention is not controlled.

Each sample loads Fleet, clicks Streams, then clicks Checks. Readiness uses the same fixture markers and two animation frames as the baseline. This is automation-observed DOM readiness, not LCP, paint completion, or a production service objective.

| Step | Before local samples, ms | After local samples, ms | Before → after local median |
| --- | --- | --- | --- |
| Cold Fleet | 159.0, 159.9, 152.0 | 126.2, 146.0, 128.0 | 159.0 → 128.0 |
| Fleet → Streams | 173.8, 159.8, 179.6 | 861.3, 863.8, 898.5 | 173.8 → 863.8 |
| Streams → Checks | 115.3, 114.9, 96.6 | 847.1, 881.5, 875.4 | 114.9 → 875.4 |

| Step | Before constrained samples, ms | After constrained samples, ms | Before → after constrained median |
| --- | --- | --- | --- |
| Cold Fleet | 7,768.0, 8,191.7, 7,724.3 | 5,346.9, 5,360.7, 5,368.6 | 7,768.0 → 5,360.7 |
| Fleet → Streams | 936.0, 908.7, 937.8 | 917.2, 926.4, 937.1 | 936.0 → 926.4 |
| Streams → Checks | 878.3, 880.7, 878.8 | 1,878.4, 1,879.0, 1,895.8 | 878.8 → 1,879.0 |

Constrained Fleet DOMContentLoaded changes from 6,707.4 to 4,361.1 ms (medians). Fleet cumulative `ScriptDuration` changes from 254.4 to 198.7 ms. Resource intervals, paint entries, long tasks, and execution/task metrics for all steps remain in the data file. They are overlapping observations, not an additive causal breakdown.

Cold transfer improves, but local first visits regress substantially and Checks misses the baseline's proposed 1,100 ms constrained navigation target. Its deferred JS and catalogue response compete for bandwidth. Streams' query begins alongside its one code request, and Checks' query begins alongside its three code requests; neither waits for code completion. The measurements do not establish that import/data overlap removes React scheduling or readiness-observation delays. No claim of improved overall navigation latency is made. A fresh review must weigh the cold-load saving against these costs; intent preloading or a different loading strategy remain proposed follow-ups.

T3 Preview opened the production demo. Both inspection attempts failed with the identical raw error `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` The isolated Chrome fallback supplied every timing sample. A preliminary selector used a placeholder where the Checks input has an accessible label; that partial run was discarded. Prototype timing runs are diagnostic only and are not mixed into the final series.

## Behavior and production serving

The isolated production browser exercised 24 direct routes across all seven families: Fleet, Overview, Streams, Consumers, Checks and a check reference, Findings, Events, Replay, Topology, Accounts, Capacity, Placement, Data history, Subjects, Micro services, Applications, Connections, KV Stores, Object Stores, Servers and server detail, stream detail, and consumer detail. Each retained the requested system/range/account query and rendered without a page exception or loading-error link. This is route coverage, not an assertion about every chart's semantic correctness.

A real stream investigation link opened its detail, survived refresh, and returned through native Back/Forward with system and range retained. A separate finding → check reference → Replay investigation retained its system and range and supplied fixed A/B time bounds. Browser Back restored a catalogue reading position of 1,800 px; following the native “Skip to content” anchor twice restored focus to `main`. An intercepted guidance chunk failed in a fresh context: an accessible alert appeared, the shell navigation remained usable, the recovery link retained the complete scoped URL, and removing the interception then following **Reload page** recovered the catalogue. Unit/integration tests cover pending-code announcements, shell retention, query overlap, URL scope, system changes, heading focus, delayed content, saved positions, repeated anchors, and drawer focus. Existing Markdown/table safety tests pass unchanged in behavior.

Every emitted JS file passed actual Go GET and HEAD checks for success, JavaScript content type, matching body length, and existing security headers. Byte ranges return 206; absent assets and unknown `/api/` procedures return 404. Static files remain uncompressed with no `Cache-Control`, ETag, or Last-Modified header from the embedded filesystem. No new cache policy was added.

Read inspection confirms Vite's root base, root BrowserRouter, and Go UI mounted at `/`; a configurable subpath is not implemented. The API stays at `/api/`. Go has no static-asset authentication; the supported deployment puts the whole service behind an authenticating proxy. Synthetic Host-allowlist and IAP-read tests pass. No private proxy, token, or live authentication path was exercised, and no arbitrary subpath/proxy deployment compatibility is claimed.

## Validation and proposed budget

- RED checkpoint `0153ac14`: two executed runtime failures for missing loading/error containment. GREEN `c900e586`: both pass. Taking out the route implementation reproduced both failures; restoring it passed both again.
- RED checkpoint `bb856a45`: the executed pending-route test observes zero catalogue requests. GREEN `458845c3`: the request starts while code is pending. Removing that fix reproduced the failure; restoring it passed again.
- Refactor `d1b3e7cd`: list/helper ownership and request grouping; expected benefit is fewer small requests and no Markdown on Streams. Output graphs and browser resources verify both.
- Final full web suite: 156 files passed, 1,623 tests passed, 24 skipped, with two workers. TypeScript and the normal `npm run build` pass. No coverage percentage is claimed; no coverage dependency was installed.
- Focused Go tests pass: `TestUIFallback`, `TestAllowHosts`, `TestDefaultAllowedHosts`, `TestServerLimits`, and `TestOnCallReadWithIAP`. The full Go/check-engine suite was deliberately outside scope.
- Feature-doc audit: clean, 39 capability docs, index consistent. Both navigation index sections and its capability doc describe loading/reload recovery. Fresh read-only review belongs to the parent.

Proposed, not yet confirmed: a lightweight CI budget should measure emitted files after build completion and recursively count the HTML entry/preload closure, with ceilings of **700,000 initial JS bytes**, **215,000 gzip level-9 estimate bytes**, and **three initial JS requests**. These leave about 5% byte headroom over two identical builds. Keep the 500 kB per-chunk warning unchanged. An enforced timing budget is premature: three samples cannot establish a stable percentile, and the local/Checks navigation regressions need review before choosing a threshold. No budget was installed merely to turn these results green.
