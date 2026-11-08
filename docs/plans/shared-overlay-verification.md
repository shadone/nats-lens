# Shared readout readability verification

Task `bOYEYXfW9eJs7h4oOb8pp`; base `542a13bf`.

## Acceptance and scope

The existing body/native-dialog portal already removes the Busiest consumers Range cell's width constraint. The remaining gaps were clipped long content, inability to cross from hover trigger to reading, keyboard scrolling, and visibility when a trigger leaves a nested clipped region. Chart inspection, help and Timeline previews now share these constraints. Detailed interactive Timeline investigations keep the existing Dialog and focus lifecycle; previews remain noninteractive tooltips.

Readouts retain a maximum 24 rem reading width bounded by the visual viewport, wrap long words and scroll excess height. Page Up/Down scroll overflowing readings from the focused trigger. Proposed, not yet confirmed: 160 ms crossing grace joins trigger and overlay, while Escape/blur remain immediate and pinned help remains pinned. Pointer movement inside a readout does not change the underlying plot's selected sample. Shared listeners observe nested scroll, window/visual viewport resize and ResizeObserver changes, with cleanup on unmount.

## Runtime evidence

- `5c131317`: three runtime failures: pointer crossing loses help, PageDown does not scroll, nested clipping leaves a detached reading visible.
- `423e7fa8`: GREEN after shared hover/scroll/clipping fix. Fixture specifies overflow axes explicitly because jsdom does not expand the shorthand as browsers do. Initial zero-size jsdom bounds falsely hid all previews; that development failure is not counted as regression proof.
- `ed18e977`: additional runtime RED, initial native-dialog portal incorrectly outside its top-layer subtree; cleanup and edge controls pass.
- `186536c3`: native portal resolution GREEN, 30 focused tests and typecheck.
- Both original shared implementations removed together: **4 runtime failures / 1 pass**; byte-exact restoration: **5 passes**. Private receipts `/private/tmp/nats-overlay-{removed,restored}.log`. No compiler failure is counted.
- Existing Timeline tests now wait through the explicit hover-crossing grace rather than requiring immediate pointer-leave dismissal. Detailed modal and Escape tests remain unchanged.

The unused `.infotip-t` absolute positioning rules were removed: no component produces that class. This clears stale card-width constraints from the shared stylesheet; the active portal class is `.chart-readout`.

## Verification scope

No API, Go, proto, dependency or native service behavior changes. One full web run, focused regressions, typecheck, production build, feature-doc audit, owned generic demo checks and fresh read-only review are used. No full Go/check suite or private source is necessary. Installed port 18900 remains untouched.

## Final acceptance

- Full web initially found one stale geometry assertion requiring `pointer-events:none`. Updated it to require reachable `auto` and scrolling overflow; its 11-test focused rerun passes. Final full web: **185 files, 1,823 passed, 24 existing skips**, 74.73 s. `/private/tmp/nats-overlay-full-web-final.log`.
- Typecheck and production build pass; existing bundle warning remains. Feature-doc audit is clean across **44 capabilities**. No Go tests were rerun for this UI-only change.
- Fresh focused read-only review approved `f1645d53` against `542a13bf`, with no concrete findings and no duplicated suites. The later delta only updates the stale CSS test expectation and this evidence.
- T3 Preview opened the owned demo at port 18683 but twice returned `Preview automation snapshot timed out after 15000ms.` Switched to the documented agent-browser fallback; no private tab was inspected or changed.
- Actual Busiest consumers Range readings passed at **360/761/1280**, light/dark, with real fonts. Readouts remained viewport-bounded, substantially wider than their triggers, outside the card/table, and without page-level horizontal overflow. Six screenshots: `/private/tmp/nats-overlay-{360,761,1280}-{light,dark}.png`.
- The browser added deliberately oversized generic text to the actual readout DOM to exercise its CSS/observer behavior. This is synthetic layout evidence, not API-data or collection completeness evidence. Page Down from the focused trigger and targeted container scrolling reach the remaining text. `/private/tmp/nats-overlay-long-reading.png` shows the bounded reading.
- The fallback CLI's generic wheel command emitted at `(0,0)` despite moving the pointer; this diagnostic attempt is excluded. A bounded direct CDP input to the owned tab's actual readout center confirmed old CSS (`overflow:hidden;pointer-events:none`) stays at scrollTop **0**, while restored CSS reaches **200**. `/private/tmp/nats-overlay-wheel.json` records it. An initial CDP attempt found the anchor already scrolled out of view by that incorrect CLI wheel; it was excluded and the anchor restored before measurement.
- Real nested consumer-table scrolling (156 → 0) hid the now-clipped anchor's overlay; returning to 156 made it visible again. Keyboard, long-content metrics, six layouts and clipping results: `/private/tmp/nats-overlay-browser.json`.
- Physical touch is not certified. Existing touch/inspection and Timeline detail behavior remains covered by component tests; the shared preview contains no interactive controls. Interactive investigation content remains in the existing focus-managed, scroll-locked Dialog.

Owned demo/browser/binary and borrowed dependency links are cleaned up at integration. No live 18900 access, private database/config/log access, install, push or deployment.
