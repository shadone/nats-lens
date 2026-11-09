# Captured Timeline event evidence verification

Status: proposed, not yet confirmed. This extends accepted `ca8b2957` retained finding occurrences. It does not close the whole Timeline task or certify a fresh repository-wide Go pass.

## Result and bounds

StoryLanes and the expanded Kubernetes lane group colliding 32-pixel hit regions while retaining their captured members. ZoomStrip retains event metadata and groups same-time/same-side observations. Single records, contributor pages and inferred windows open inside the refresh-stable investigation. Payloads are requested only for the explicitly selected record. A dedicated captured-window route reconstructs its exact ordered-input identity or reports unavailable; no current window is substituted.

The same additive API serves live, demo and captured data. Exact batches have 1–50 IDs, selected feeds at most 1,000; advanced responses use the existing 512 KiB JSON/protobuf response budget. Native queries bind validated `[]int64`, not string lists cast by the driver. Missing IDs, old-server acknowledgement failures, input limits and unknown continuity remain explicit. The demo reconstructs a captured reference frame rather than shifting selected records with its current clock. See [the request contract](../reference/timeline-event-evidence.md).

## Runtime checkpoints

- `5ab7d39b`: window identity/ordered support and native selected membership RED. Native minimal GREEN `64732590`.
- `b97b9454`: selected-response byte and unfiltered-count RED; GREEN `d49f0e0d`.
- `a1c88918`: demo selected identity shifted with its clock; GREEN `c7dd916f`.
- `6f2814ed`: resolved query range absent; GREEN `eb8024a9`.
- `1927c5e4`: three UI behaviors and adapter RED. Adapter and event/group/window UI pass in the integrated feature commit.
- `c2e86fc7`: ignored fields/wrong returned IDs accepted by a previous-compatible server; GREEN `fbe0ca7c`.
- `e6d67b57`: exact identity-only window batches return zero; GREEN `1644ba15`.
- `60dbd8c8`: portable bounds lose submillisecond precision; GREEN `4914fc0d`.
- `126168f5`: the window page's general copy action lacks its captured query; GREEN `bfe1e7f7`.

All RED claims above are runtime assertions, not compile failures. Intermediate build failures and a driver string-list binding panic were corrected and are not claimed as regression proof.

## Verification executed

Go commands use `GOCACHE=/private/tmp/nats-storage-go-cache GOPROXY=off GOTOOLCHAIN=local GOMAXPROCS=2`.

- Focused native/demo event, window, selection, raw payload and compatibility suites pass. The initial sandboxed race command passed API/store/demo, but its investigation/offline packages could not bind disposable HTTP listeners. Only those failed packages were retried with loopback permission.
- `TZ=Pacific/Honolulu go test -race ./internal/api ./internal/api/storeprov ./internal/api/fake ./internal/investigation ./internal/offline -run 'Test(TimelineCapturesEvent|SelectedTimelineEvents|SelectedEventResponses|TimelineFindingEvent|WindowEventBatch|DemoTimelineEvent|DemoRecordedEventRead|DemoWindow|ExactEventReads|OfflineLiveParity|K8sWindow|KubernetesWindow)' -count=1 -timeout=5m`: native/store/demo pass. Qualified retry: `go test -race ./internal/investigation ./internal/offline -run 'Test(ExactEventReads|OfflineLiveParity|TimelineEventContextRoundTrip)' -count=1 -timeout=5m`, same non-UTC environment: pass. Live/offline parity includes the new batch method and exact >2^53 IDs.
- `go test ./internal/investigation -run '^TestTimelineEventContextRoundTrip$' -count=1`: exact event/window URL import and prepared query pass.
- `go vet ./internal/api ./internal/api/storeprov ./internal/api/fake ./internal/investigation ./internal/offline`: pass.
- `buf lint`, `buf format --diff --exit-code`, generated Go/TypeScript: pass. No migration, dependency, or check-count changes.
- Web suite: 211 files, **1,932 passed, 24 existing skips**. During that run the final fixed lane-link wiring landed; the subsequent affected focused tests pass. This is a broad receipt plus final focused validation, not a frozen-tree all-suite claim. Later window-copy/paging controls add two tests and pass in the final focused selection (**17 tests**).
- `npx tsc --noEmit`: pass. `npx vite build --configLoader runner`: pass; existing >500 kB chunk warning retained. The last production build includes the page-copy correction.
- Feature audit with `--map-refs require`: clean, 46 capability docs. Both index sections updated.

## Removal/restoration controls

Private evidence is under `/private/tmp/nats-event-evidence-controls/`:

- `batch-red.log`: Go overlay replaces native/demo batch implementations with empty replies; both exact-identity tests fail at runtime. Restored production passes focused/race tests.
- `compat-red.log`: removing the acknowledgement guard makes six adversarial server replies succeed incorrectly. Restored guard passes the loopback race suite.
- `ui-red.log`: restoring old lane wiring and event details produces the same three UI assertion failures. `ui-restored.log`: 18 affected tests pass.
- `context-red.log`: removing precise selected bounds produces one runtime failure; restored context test passes.
- `window-copy-red.log`: removing window-page inference produces the new runtime failure; `window-copy-restored.log` passes.

Temporary UI mutations were restored in `finally` before subsequent validation. Go controls used overlays without modifying production files.

## Browser checks and limits

Only owned fixed synthetic demo data on `127.0.0.1:18612` was used. Initial `system=prod` was not a demo identity; inspection used the actual `example-prod` scenario. No private port 18900 access occurred.

T3 preview snapshot failed twice with `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` Installed agent-browser fallback then verified:

- Native pointer activation opens the 47-member overlap. Explicit member read loads its actual stored summary, reason, UTC times and resource links. Keyboard activation after scrolling the control into view opens the payload. Out-of-view click/focus attempts did not activate; they are not counted as passes.
- Real fonts, 47 members, retained payload and no dialog/page horizontal overflow at **360/761/1280**. Scoped resource links retain fixed dates/system, including canonical object-bucket navigation. JSON: `/private/tmp/nats-event-evidence-browser.json`; screenshots `/private/tmp/nats-event-evidence-{360,761,1280}.png`.
- Native keyboard window activation and link navigation reach the exact window route. It reconstructs **17 actual supporting inputs** with the derivation and unknown-continuity disclosure. Route containment passes at all three widths. JSON: `/private/tmp/nats-window-evidence-browser.json`; screenshots `/private/tmp/nats-window-evidence-{360,761,1280}.png`.

Browser workflow assets predate only the final general window-copy correction; that correction has runtime UI and removal/restoration controls plus the final production build. Physical touch, OS clipboard ordering/readback and production deployment are not certified. Universal snapshot pinning, capture reconstruction after retention, complete causal attribution, universal keyboard aggregate equivalence and the whole Timeline umbrella remain open.
