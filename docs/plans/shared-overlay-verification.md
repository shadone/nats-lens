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
