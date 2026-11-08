# Web code splitting: R1 revision, 2026-11-08

The revised split reduces complete initial JavaScript by 8.2% and observed constrained Fleet readiness by 6.4%. Streams and Checks remain close to the approved baseline; several deferred first visits cost 25–93 ms more under constrained transfer. The split and its tradeoffs are proposed, not yet confirmed. Fresh R2 and integration belong to the parent task.

The [original candidate record](web-code-splitting-2026-11-08.md) and its JSON remain historical evidence. They measured `d1b3e7cd`, before approved slow-consumer investigation code. The original automation-observed local navigation figures around 850 ms include locator polling: they do not mean the content was invisible for that whole interval. R1's visibility-aware review found roughly 355 ms of actual first-route delay, including an avoidable roughly 300 ms fallback retry. Its pre-imported control still showed that delay with no navigation JavaScript requests. Historical samples are neither relabeled nor combined with this revision's series.

## Sources and method

- Approved before: `b783bf51` (full identity in the JSON), including approved slow-consumer routes and API, Capacity growth/types, bucket details, safe Markdown/GFM and shared navigation/scroll behavior. This baseline is **1,136,785 initial JS bytes**, with a Node 22 gzip-9 estimate of **334,183 bytes**. The older 1,127,667-byte comparison is a different source.
- Integrated rejected candidate: `93cdb771`, a merge of `ec0453de` into that approved baseline. Only the App import conflict needed reconciliation; the approved slow-consumer route stays present. No storage-history sibling changes were merged.
- Proposed production source: `1a39eab075cce7ac84c4b7c32ce6a5c1e05a463c`. Subsequent probe/document commits do not change production code.
- The [measurement runbook](../operations/measure-web-loading.md) defines the observer. In-page mutations plus animation frames check the original visible markers inside `main`; automation observation, pre-click marks, trusted-click timestamps, first visible DOM and two subsequent frames remain separate. Hidden content and the header's search control do not count.
- Installed Chrome 156.0.8078.48, existing Playwright Core 1.56.1, Node 22.21.1, Go 1.27.3; viewport 1280 × 800, fresh contexts, service workers blocked, CDP HTTP cache disabled. Local uses native CPU/unlimited loopback. Constrained uses 4× CPU, 150 ms latency, 200,000 B/s down and 93,750 B/s up. Real bundled fonts remain enabled. No dependency installation or lockfile change occurred.
- T3 Preview was tried first. Both inspections failed with the exact error `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` Owned isolated installed Chrome processes provide the browser evidence.

## Loading strategy and alternatives

The eager page modules retain their existing hooks, URL parsing, state and callbacks. Accounts, Applications, Connections, Data history, Micro services, Placement, Replay and Topology pass a typed model to a deferred view. There is no second routing/scoping/loader map. Query results are spread in the eager module: property tracking must observe failures/loading changes even when a view has not yet mounted. The view cache stores only the component/module promise, never response data or a system/account model.

Explicit module state replaces new per-page `React.lazy`/Suspense boundaries. Pending views announce “Loading page…”; a resolved module updates the view without a Suspense fallback retry. A warm view renders synchronously. Unmounting cancels its notification, and loading a new scope uses the latest model. Import errors reach the retained route error boundary; its generic alert and native full-address reload remain, with the shell available.

Streams, Checks, Consumers, Capacity, resource details, Servers, Findings, Events and other eager pages issue their existing reads directly. Pages with additional read-owning nested components stay eager. This includes the approved slow-consumer investigation. The safe Markdown renderer itself is unchanged; it remains eager through Checks and the read-owning pages. Replay's existing timeline/default-moment dependency remains intentional: its timeline read starts independently of view code, and its comparison starts once its moments are known. Connections' finding read still depends on its returned epoch. No speculative reads for unrelated pages, per-row reads or new unbounded reads were added.

| Build alternative | Initial JS bytes | Initial requests | Largest chunk | Total JS files |
| --- | ---: | ---: | ---: | ---: |
| Approved before | 1,136,785 | 1 | 1,136,785 | 1 |
| Rejected integrated family split | 676,441 | 3 | 404,781 | 13 |
| Three deferred views, core pages eager | 1,101,601 | 3 | 696,114 | 6 |
| Broader presentation prototype | 994,797 | 3 | 589,310 | 20 |
| Eight views, old manual grouping | 1,044,669 | 3 | 639,182 | 11 |
| Eight views, automatic grouping | 1,043,186 | 2 | 725,185 | 10 |
| Proposed final, canonical service selection key | 1,043,219 | 2 | 725,218 | 10 |

These are separate build experiments, not a single timing series. The three-view prototype saved only 3.1%. The broader prototype introduced small splits and retained additional data-owning components behind code, so it was narrowed. The proposed eight views save **93,566 bytes (8.2%)** from the complete initial closure. In the grouping experiment, removing obsolete manual page-support grouping saves 1,483 bytes and one initial request; automatic grouping needs no tiny shared runtime request. 

Initial JS consists of the 725,218-byte entry and its 318,001-byte shared static import. The eight deferred views range from 6,000 to 17,871 bytes; each first visit fetches one view, with common dependencies already present. No duplicate output module IDs were found. The service-selection parameter has one authoritative definition in the eager page and reaches its view through the model; this avoids both a copied key and new runtime import cycles.

The 500 kB warning threshold is unchanged and the production build still warns. Keeping Markdown, the shell/runtime and read-owning core pages eager is a deliberate proposed tradeoff; splitting solely to make this warning disappear would repeat the rejected behavior. The result does not meet the historical proposed 700,000-byte initial ceiling. That proposal was never an installed gate. No size or timing budget was changed or enforced to make this result pass.

## Transfer and browser results

Initial gzip-9 estimates change from 334,183 to 313,688 bytes; Brotli-11 estimates from 278,266 to 264,978. These estimates use Node 22 and emitted files after build completion. Actual embedded Go JavaScript responses remain uncompressed: initial encoded bodies total 1,043,219 bytes, not the gzip estimate. CSS and fonts are counted separately from JavaScript. Browser header allowances are not packet-level wire measurements.

The corrected observer is recorded in probe commit `15a654a6`, separately from the production source. The corrected series is the authoritative new comparison in the [measurement JSON](web-code-splitting-r1-2026-11-08.json). It uses explicit `system=example-prod&range=6h`, all accounts, throughout Fleet and sidebar navigation. Historical series with different source/scope or polling semantics are not comparable observations. Three fresh contexts per variant/profile run interleaved before/after in one Chrome process, followed by separate cold-direct and pre-imported controls.

The parent granted the corrected window at 01:36:53 UTC. The browser process ran 01:37:53–01:40:32 UTC. Root, pagination and storage review acknowledged no task workloads; no tests, builds or source edits ran during it. This is **coordinated task quiet**, not a whole-host uncontended claim: host load averages rose from 2.89/3.19/3.17 to 7.12/4.19/3.53. Other host activity was not controlled, and the load increase is not attributed to any particular process.

All values below are medians relative to the pre-click mark (Fleet uses navigation start), including trusted-click automation overhead. Capture-phase click times and individual samples remain in the JSON. The two-frame column records the two frames after actual visibility, rather than after a locator's polling result.

| Page | Local visible DOM, before → after (ms) | Constrained visible DOM, before → after (ms) | Constrained two frames, before → after (ms) |
| --- | ---: | ---: | ---: |
| Fleet | 101.4 → 93.0 | 7713.8 → 7220.9 | 7726.6 → 7228.9 |
| Streams | 97.4 → 97.0 | 343.6 → 348.0 | 358.6 → 358.4 |
| Checks | 83.1 → 84.4 | 678.9 → 676.6 | 687.0 → 686.2 |
| Capacity | 80.9 → 79.5 | 272.8 → 276.7 | 280.1 → 281.7 |
| Topology | 58.0 → 57.7 | 243.3 → 306.4 | 263.2 → 329.0 |
| Placement | 63.9 → 61.7 | 265.1 → 343.4 | 272.3 → 351.9 |
| Replay | 83.7 → 84.2 | 531.3 → 477.1 | 537.5 → 484.0 |
| Accounts | 57.0 → 56.4 | 247.5 → 312.0 | 263.3 → 328.4 |
| Applications | 69.6 → 70.7 | 308.1 → 352.8 | 314.5 → 362.3 |
| Connections | 76.0 → 73.2 | 333.7 → 358.4 | 349.4 → 375.8 |
| Data history | 55.3 → 56.6 | 253.0 → 291.3 | 263.8 → 310.9 |
| Micro services | 56.3 → 55.4 | 238.7 → 331.3 | 247.7 → 346.1 |

Constrained Fleet improves by 492.9 ms (6.4%) in this sample set. Local Streams/Checks return to the approved baseline's range rather than retaining the rejected candidate's fallback plateau. Constrained Streams changes by +4.4 ms, Checks by −2.3 ms and Capacity by +3.9 ms; three samples do not establish a stable distribution or SLO.

Several deferred first visits are slower under constrained transfer: Topology +63.1 ms, Placement +78.3, Accounts +64.5, Applications +44.7, Connections +24.7, Data history +38.3 and Micro services +92.6. The view request overlaps primary data but still consumes bandwidth and may finish later. These costs are explicit proposed tradeoffs for the initial saving, not owner acceptance. Replay is 54.2 ms faster in this series; its dependent reads and rendering overlap differently, but this comparison does not isolate that cause. No intent preloading or hidden prerender is used to obtain the navigation numbers.

The constrained primary-read/view intervals below are medians from the same navigation series. A fetch start is Resource Timing `startTime`, not completion or exclusive server processing; wire `requestStart` is retained separately in JSON. Before and after request counts/intervals are retained individually. Each deferred primary read starts before its view code ends. The held-code probe additionally proves the read starts even if that code never arrives.

| Destination / primary API | After fetch start (ms) | After API end (ms) | After view code end (ms) |
| --- | ---: | ---: | ---: |
| Capacity / GetCapacity | 67.6 | 254.9 | no navigation JS |
| Topology / GetTopology | 63.7 | 232.9 | 298.9 |
| Placement / GetPlacement | 63.3 | 246.9 | 316.4 |
| Replay / GetStatusTimeline | 66.5 | 252.2 | 318.9 |
| Accounts / GetAccountsAndAuth | 64.3 | 234.3 | 303.4 |
| Applications / GetApplications | 63.8 | 297.0 | 326.6 |
| Connections / ListConnections | 63.9 | 279.5 | 286.3 |
| Data history / GetDataHistory | 70.4 | 235.6 | 280.8 |
| Micro services / GetMicroServices | 62.7 | 238.4 | 320.2 |
| Streams / ListStreams | 138.0 | 333.2 | no navigation JS |
| Checks / ListCheckDefinitions | 63.6 | 564.5 | no navigation JS |

A separate pre-fix diagnostic on the integrated rejected candidate observed Capacity code ending at 1,604.6 ms and `GetCapacity` only starting at 1,623.6 ms (its paired approved-before read started at 131.0 ms). Those are single diagnostic observations with contention unverified, not medians of the corrected series.

Cold/direct links and pre-imported controls are separate from navigation medians:

| Constrained direct route (one pair) | Before visible / two frames (ms) | After visible / two frames (ms) |
| --- | ---: | ---: |
| Checks | 8,189.8 / 8,199.2 | 7,688.5 / 7,697.4 |
| Capacity | 7,708.1 / 7,716.9 | 7,241.8 / 7,249.9 |
| Topology | 7,659.2 / 7,677.2 | 7,355.1 / 7,377.8 |
| Placement | 7,707.0 / 7,715.6 | 7,392.9 / 7,401.0 |
| Replay | 7,965.9 / 7,980.0 | 7,530.4 / 7,545.3 |

These direct values include the entire initial closure and existing system-validation reads. On direct Topology, Placement and Replay visits, view-code and primary-data starts are within about 3–4 ms of one another; view reads do not wait for code completion. Replay's comparison then waits for its timeline-derived moments, as before.

| First visit after code evaluation (local diagnostic) | Visible DOM (ms) | Two frames (ms) | Navigation JS requests |
| --- | ---: | ---: | ---: |
| Streams (eager) | 95.8 | 112.1 | 0 |
| Checks (eager) | 83.7 | 85.8 | 0 |
| Topology (explicitly imported view) | 55.7 | 79.4 | 0 |
| Placement (explicitly imported view) | 59.9 | 79.6 | 0 |
| Replay (explicitly imported view) | 83.4 | 95.8 | 0 |

The earlier integrated-candidate pre-imported diagnostic had two-frame Streams/Checks readiness of 340.3/338.1 ms with no navigation JS transfer. It is retained separately, with its own non-coordinated diagnostic conditions. The structural regression controls, not an arbitrary millisecond target, demonstrate that ready code/data now render without the code-fallback retry.

Every one of the six Fleet contexts per variant fetched exactly the recorded initial static/preload JS closure: one 1,136,785-byte file before, two files totaling 1,043,219 bytes after. CSS is 116,976 bytes for each. The five local font responses total 100,620 bytes; under constrained transfer some fonts remain in flight at visible readiness. Real fonts are enabled, and the marker does not mean all resources finished. Visiting every deferred view would download 1,146,554 total JS bytes, 9,769 (0.9%) more than the eager baseline, due to splitting overhead.

The first window's raw series remains diagnostic. Its generic Connections row marker could match the preceding Applications table during a URL/DOM transition; **those Connections readiness values are invalid and excluded**, while other unique-marker observations remain individually qualified. The corrected observer requires a visible Connections filter form and its own visible data row in the table identified by “Server / CID”. A deterministic virtual-HTML control excludes the old Applications table, mixed old table/new form, hidden form and hidden own row; removing the guard produces runtime RED, restoring it passes. No source/build changed during either timing series. The corrected series does not relabel the first series or the historical rejected split.

## Regression and production verification

- Runtime RED checkpoint `7de39006` exposed primary reads blocked by held route code; `b5c7f789` corrects Replay's RPC selector to `GetStatusTimeline` and reruns RED. Earlier attempts with the wrong Replay method are not evidence for Replay. `9d2b1dc3` records the separate executed pre-imported readiness RED: no JS transfer, completed primary response plus two frames, but actual content absent and the page fallback still visible. It sets no millisecond SLO.
- GREEN fix `ec302c32`: both unchanged browser probes pass, along with focused tests and the full web suite. `b9762af4` records the measured request-grouping refactor. `6d228a5c` expands the barrier probe to every deferred view plus Capacity; all nine correctly scoped primary requests start while every new JS request is held. Applicable account scope is asserted too.
- A disposable export of the GREEN fix had the production fix patch reversed. Rebuilding it reproduced the integrated rejected candidate's exact chunk filenames/sizes. Both unchanged probes returned runtime RED; returning to the final production build restored GREEN. No original checkout, live database or deployment participated.
- Full web suite: 158 files, 1,632 passed, 24 existing skips, two workers. TypeScript passes. After the grouping refactor, 14 focused loading/error/Markdown tests pass. A plain production build reproduces all 10 observed chunk filenames/sizes. The final authoritative service-key/model refactor passes TypeScript and 56 focused Micro services, view-rule and loading tests; its probe/document commits do not alter the measured production identity. No numerical coverage claim or coverage dependency installation is made.
- Focused Go tests pass: `TestUIFallback`, `TestAllowHosts`, `TestDefaultAllowedHosts`, `TestServerLimits`, `TestOnCallReadWithIAP`. A first sandboxed attempt could not open synthetic listeners; the permitted rerun passed. No full Go/check-engine suite claim.
- Embedded production behavior: 27 direct routes including the approved slow-consumer investigation, bucket details and all deferred views; stream detail refresh/native Back/Forward; finding → check → Replay with fixed bounds; exact 1,800 px catalogue POP restoration; repeated Skip-to-content focus. Aborting a real Topology view chunk gives an accessible alert and retained shell, navigation to Streams works, Back retains the failure, and native full reload after restoring the request recovers the scoped Topology page.
- All 10 real Go chunks pass GET/HEAD success, JavaScript MIME/body length/security headers and byte-range 206 checks. Missing assets and unknown API methods return 404. No new cache policy, static authentication or subpath support is claimed: existing root UI/API mounts and whole-service authenticating proxy scope remain. Static responses have no Content-Encoding, Cache-Control, ETag or Last-Modified. Synthetic Host/IAP tests do not establish compatibility with a private proxy or real authentication setup.
- Navigation, all eight adjacent capabilities and both feature index sections describe the behavior. Feature audit: clean, 39 documents, map references required. Safe Markdown/GFM tests retain the approved renderer's behavior.

## Limits

The final timing window is coordinated task quiet, with observed workload conditions, never proof of an uncontended host. Three samples cannot establish a percentile or product SLO. Visible DOM/two frames do not establish completed paint, LCP or fully arrived fonts. There is no live/private DB/config/log/archive/NATS/cluster/auth/proxy, port 18900, Safari, touch-device, full Go/check-engine, deployment or push verification. Rejected candidates and prototype evidence remain separate from this proposed result. Fresh R2 remains required.
