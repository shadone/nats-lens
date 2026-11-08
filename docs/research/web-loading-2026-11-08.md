# Web loading baseline, 2026-11-08

The production UI downloads every page in one JavaScript chunk. Local desktop loading hides the transfer cost: the isolated demo becomes DOM-ready in about 161 ms locally, but about 7.72 seconds under the constrained profile below. This is a browser baseline, not evidence about collector performance.

The [measurement runbook](../operations/measure-web-loading.md) describes reproduction. The [measurement data](web-loading-2026-11-08.json) contains individual samples, resource timings, dependency attribution, and diagnostic trace totals. No application behavior, dependencies, chunk configuration, or warning threshold changed. Fresh read-only review and integration belong to the parent task.

## Build identity

| Item | Observed value |
| --- | --- |
| Source commit | `3a2d0b69d4e20436e2e20d10b046a3b23acc2641` |
| Base | Reviewed integration, including develop `61588abcaca559e720df5086544a7fa40db86748` |
| Research branch | `t3/bundle-size-load-baseline` |
| Host | macOS 15.6.1, build 24G90, arm64 |
| Node / npm | 22.21.1 / 11.7.0 |
| TypeScript / Vite / Rolldown | 6.0.3 / 8.3.2 / 1.2.12 |
| React / React DOM | 19.3.0 / 19.3.0 |
| Go | 1.27.3, darwin/arm64 |
| Lockfile SHA-256 | `4a96e3c9ff19b175d8bc3e9d6024605cc92bd42cb2a01d8892bda100fefc2282` |
| JavaScript SHA-256 | `7c243533df3599e66021323f7631794ca07d9cfdcdc265ba5d053a3b30b25514` |

The installed dependency tree from the approved Markdown checkout was borrowed through a local ignored symlink. No install ran. The temporary Vite configuration merged the production configuration, redirected its cache into private temporary storage, and added only a `generateBundle` metadata observer. Source maps remained disabled. TypeScript passed before the build. A second production build verified the same JavaScript hash.

## Output and actual transfer

Sizes are exact bytes. Gzip uses Node zlib level 9; Brotli uses quality 11. Vite's displayed gzip estimate uses its own settings and reports 333.91 kB for JavaScript; it is not the level-9 value below.

| Production file | Minified/body bytes | Gzip estimate | Brotli estimate |
| --- | ---: | ---: | ---: |
| `assets/index-C79PO9cE.js` | 1,124,674 | 330,584 | 275,246 |
| `assets/index-Bmev4f3N.css` | 116,293 | 20,629 | 17,957 |
| `index.html` | 935 | 565 | 389 |

The HTML references one module script and one stylesheet. Rolldown reports one entry, 535 modules, zero static chunk imports, and zero dynamic imports. There are no module-preload links. Therefore total initial external JavaScript is **1,124,674 bytes**, including every shared dependency. HTML also contains the small inline theme initializer, already counted in HTML bytes.

The freshly built Go binary served `demo -fixed` on isolated loopback port 18439. A GET advertising `Accept-Encoding: gzip, br` returned `Content-Length: 1124674`, no `Content-Encoding`, and a matching body. Chromium reported 1,124,674 encoded and decoded JavaScript bytes, and `transferSize` 1,124,974. That last figure includes the browser's response-header allowance; it is not a packet capture or exact TCP cost. The API responses separately used gzip. Compression estimates do not describe the embedded static-file handler's transfers.

The local Fleet load also fetched 116,293 CSS bytes and five Latin WOFF2 fonts totaling 100,620 bytes. Those assets matter to loading even though they are outside the JavaScript budget. At constrained readiness, one font sometimes remained in flight; the readiness mark does not mean all resources finished.

The default warning stayed visible: `Some chunks are larger than 500 kB after minification.` This is not a build failure.

## Eager contributors

Rolldown `OutputChunk.modules[].renderedLength` supplies attribution before final whole-chunk minification. Its sum is 2,232,428 bytes, larger than the final chunk. These numbers rank included code; they are neither downloadable bytes nor additive predictions of code-splitting savings. Compression, tree shaking, shared imports, and module wrappers change after a split.

| Included group | Rendered length |
| --- | ---: |
| Application modules excluding pages and generated API | 676,881 |
| React DOM | 536,855 |
| Markdown/GFM family, including supporting utilities | 335,177 |
| `@bufbuild/protobuf` | 196,179 |
| All 26 page modules | 173,310 |
| Generated API modules | 88,217 |
| React Router | 84,180 |
| TanStack query core | 64,609 |
| Connect core / web | 31,524 / 7,085 |

The Markdown family is a grouped subset of dependencies, including `react-markdown`, remark, micromark, mdast, hast, unist, unified, vfile, and their included utilities. It overlaps neither React DOM nor the application rows. The data file records the package-level values used to inspect the grouping.

`App.tsx` eagerly imports all pages. The largest page modules are Finding detail (19,195), Events (13,181), Connections (12,672), Checks (12,590), Stream detail (12,508), and Findings (10,464). Page components also pull in larger supporting components: topology maps/tables, account tables, history parts, replay controls, placement panels, and entity details. Splitting only the small route wrapper would misstate the boundary's value.

`components/common.tsx` statically imports `react-markdown` and `remark-gfm` for `Remediation`, alongside basic shared UI. Fleet and Streams import basic UI from that file. Merely making Checks and Finding detail lazy can therefore keep Markdown eager. The first implementation should examine this shared import path alongside routes.

React DOM, routing, query infrastructure, and core API transport belong to the shell or common data path. Their presence alone does not justify manual vendor chunks: preloaded shared chunks still count toward the initial total.

## Browser conditions and samples

The repeatable series used installed Google Chrome **156.0.8078.48**, headless, with Playwright Core 1.56.1 from an existing external installation. Each run created a fresh browser context with service workers blocked, HTTP cache disabled through CDP, and a 1280 × 800 viewport. The same browser process served the series. This is HTTP-cache-cold, not a cold OS/browser-process benchmark. No extensions, persistent profile, or NATS deployment participated.

| Profile | Network configuration | CPU configuration |
| --- | --- | --- |
| Local | Loopback HTTP/1.1, no added latency, unlimited bandwidth | Native desktop, CDP rate 1 |
| Constrained | CDP latency 150 ms, download 200,000 bytes/s (1.6 Mbps), upload 93,750 bytes/s (0.75 Mbps) | CDP slowdown rate 4 |

CPU slowdown is emulation on arm64 macOS, not a measurement on a particular phone. Host contention was not controlled. The earlier T3 Preview session reported 12 logical processors and 32 GiB device memory; hardware model lookup was sandbox-blocked. The fallback browser series is authoritative for comparisons; the Preview browser was a different version.

Each of three runs loaded `/fleet`, clicked the sidebar Streams link, and then clicked Checks. Readiness means the generic fixture's Fleet summary, the `order-jobs` stream row, or the **Search checks** input exists, followed by two animation frames. Times include automation click scheduling and readiness observation. They are not interaction latency, LCP, or a guarantee of completed paint.

| Step | Local samples, ms | Local median | Constrained samples, ms | Constrained median |
| --- | --- | ---: | --- | ---: |
| Cold Fleet, navigation start to readiness | 236.6, 160.5, 159.4 | 160.5 | 7,723.3, 7,772.6, 7,723.6 | 7,723.6 |
| Fleet → Streams, before click to readiness | 140.3, 145.9, 144.6 | 144.6 | 923.5, 907.3, 907.7 | 907.7 |
| Streams → Checks, before click to readiness | 96.4, 101.5, 99.6 | 99.6 | 878.2, 845.3, 896.7 | 878.2 |

Neither navigation downloaded JavaScript. All page code had already arrived on Fleet. The current navigation numbers are a useful guardrail when later lazy routes add requests.

The first independent T3 Preview cold `/` load redirected to Fleet, with FCP 176 ms and DOMContentLoaded 115 ms. Two subsequent `preview_snapshot` attempts failed with the identical error `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` The research then used the isolated installed Chromium fallback. The failed initial stream selector (`orders` instead of `order-jobs`) was corrected and the entire measured series restarted; no partial failed series enters the table.

## Transfer, API wait, parsing, and execution

| Observation | Local | Constrained |
| --- | --- | --- |
| JavaScript response body interval, median | 1.2 ms | 6,376.2 ms |
| DOMContentLoaded, median | 53.4 ms | 6,698.4 ms |
| Fleet API resource window, first start to last completed response | 24.2–26.1 ms | 484.0–512.3 ms |
| Fleet cumulative CDP `ScriptDuration`, median | 57.9 ms | 269.1 ms |
| Fleet cumulative CDP `TaskDuration`, median | 100.1 ms | 537.3 ms |
| Streams incremental `ScriptDuration`, median | 10.0 ms | 49.0 ms |
| Checks incremental `ScriptDuration`, median | 26.2 ms | 106.1 ms |

The asset interval is `responseEnd - responseStart`. It includes emulated delivery waits and competition with CSS, not pure link throughput. At 200,000 bytes/s, JavaScript alone has a 5.62-second ideal body-transfer floor; the observed 6.38 seconds exceeds that floor.

Fleet makes `GetLensInfo`, `GetShellStatus`, another `GetLensInfo`, and `ListSystems` requests. Streams adds shell calls and `ListStreams`; Checks fetches `ListCheckDefinitions`. These windows are not summed because calls overlap. `responseStart - requestStart` includes scheduling, transport, and provider work, not isolated server execution. The constrained CDP network model places much of its delay after response start. The data file retains all three timestamps so that apparent low TTFB is not mistaken for a fast complete response. The fake API does not establish real database or deployment latency.

`ScriptDuration` measures aggregate script execution through the readiness observation, including API decoding, React work, and tiny measurement callbacks. `TaskDuration` includes other main-thread work. Neither is an independent parser CPU timer, and subtracting either from wall time would not isolate API wait.

Two additional diagnostic runs traced `devtools.timeline,v8,v8.execute,disabled-by-default-v8.compile`. Inclusive `V8.ParseProgram` event totals were 17.529 ms locally and 78.404 ms constrained; `V8.ParseFunction` totals were 10.314 and 48.315 ms. They show parsing work separately from execution metrics, but include instrumentation scripts and nested events. They must not be added to each other or to `ScriptDuration` as a causal wall-time breakdown. Background streaming-parser spans include network waits: a constrained `v8.parseOnBackground` span lasted 6,213.86 ms. Calling that six seconds of parsing CPU would be false. A per-function exclusive CPU profile was outside this baseline.

The constrained cold runs recorded two long tasks each; the first run's tasks lasted 134 and 76 ms. Checks navigation recorded one 87–92 ms task per constrained run. Local runs recorded no tasks exceeding 50 ms before their readiness observations. FCP was 7,064–7,100 ms constrained; two local runs reported 112 ms. The first local FCP was not available at its readiness snapshot, so no three-sample local FCP median is claimed.

## Follow-up boundaries and budgets

All choices below are **proposed, not yet confirmed**, for task `qo8gHkd9mzHfUvWrf0EgY`.

1. Separate Markdown rendering from basic `common.tsx` exports, then load check guidance and finding detail on demand. The 335,177-byte rendered Markdown family and its eager shared import path make this a higher-value boundary than cosmetic vendor chunking. Verify both direct check links and finding guidance.
2. Lazy-load route families and their dedicated helpers: findings/checks; streams/consumer/server details; Events/Replay; topology/accounts/history/placement. Preserve initial Fleet and the shared shell. The 26 page modules contribute 173,310 rendered bytes before their dedicated helpers. Measure each change's complete initial static import/preload closure rather than claiming those attribution bytes as savings.
3. Measure compression of embedded static assets as a separate follow-up. The JavaScript gzip estimate is 70.6% smaller than the actual uncompressed body, but compression does not remove parse/execution work. This research does not change HTTP serving or assume a proxy supplies compression.
4. Revisit generated protobuf and broad shared-component reachability only after the first boundaries have before/after measurements. Avoid manual Rolldown grouping until network traces show the chosen shared layout avoids serial waterfalls and duplication.

Proposed comparison budgets use decimal bytes and the same browser series:

| Check | Proposed budget | Basis |
| --- | --- | --- |
| Baseline regression ceiling before optimization | Initial JS ≤ 1,125,000 minified bytes and ≤ 334,000 gzip-estimate bytes | Rounds the measured chunk and Vite gzip output; prevents growth without claiming optimization |
| First splitting target | Initial JS ≤ 800,000 minified bytes and ≤ 250,000 gzip level-9 bytes | About 29% raw and 24% gzip reduction; the measured eager Markdown/pages/helpers justify testing this target |
| Constrained cold Fleet | Median readiness ≤ 6,000 ms | Requires a meaningful reduction from 7,724 ms; 800,000 raw bytes have a 4-second ideal transfer floor at this profile |
| Constrained first visit to Streams and Checks | Median readiness ≤ 1,100 ms each | Allows about 200 ms for lazy-request overhead over 908/878 ms baselines; reject larger serial waterfalls |
| Local cold Fleet | Median readiness ≤ 250 ms | Retains margin over the 161 ms median and 237 ms slowest observed run |

These are candidate review thresholds, not CI enforcement or a guarantee that the first implementation reaches them. Timing budgets require repeat samples on the same environment; three samples cannot establish a p95 or fleet-wide service objective. The byte targets need validation after actual splitting. Count every initially fetched JS chunk, including preloads, and record largest lazy chunks too. Keep the existing 500 kB warning until a measured shared-chunk tradeoff warrants a separate decision.

The follow-up should compare the production Go binary under these profiles, add direct deep-link/refresh and Back/Forward samples, and test chunk failure recovery, scope, focus, and scroll preservation. This baseline exercises Fleet → Streams → Checks only; it does not claim those behavior tests or auth/proxy-path coverage.
