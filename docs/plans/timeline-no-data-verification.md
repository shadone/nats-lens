# Aggregate Timeline missing-state correction

Status: proposed, not yet confirmed. Base accepted `942a7ec4`; the full Timeline acceptance inventory remains separate.

## Correction and scope

StoryLanes no longer discards aggregate no-data runs or labels an empty returned state set “ok throughout”. Missing runs use inspectable no-data marks and an explicit explanation. No returned buckets creates no invented mark. Known observations retain the worst-observed-state aggregation; one missing check does not erase another check's known state. “OK in observed buckets” is not complete configured evaluation or continuous capture. Retained occurrences can overlap a missing interval without proving evaluation in those buckets.

Overview also requests the existing API's `includeOk` rows. Without them, an empty failed-row response could not distinguish known OK from omitted rows. The flag partitions the query cache; other consumers keep their false default and the same refresh cadence. No new RPC, SQL algorithm, schema, dependency or check change is added. The agent UI-link proof treats this unencoded row-selection option conservatively, for both false and true, rather than assuming every UI page uses false. Exact machine requests are unchanged.

## Focused evidence

- `affda41e`: **five runtime failures** for all-missing, mixed warning/missing across midnight, observed OK, no buckets and partial missing-check controls. GREEN `9acb025f` retains the existing aggregate model's no-data runs and qualifies labels.
- Request/cache/proof checkpoint `7d4d25b1` independently fails because Overview omits OK rows, two cache modes collapse into one request, and false `includeOk` silently inherits a UI-equivalence policy. A missing test import was corrected before counting the actual Go runtime failure; compilation failure is not RED.
- GREEN `0ac19f77`: **172 affected web tests passed, 24 existing placeholder skips**; TypeScript passes. Earlier mark/source-limit regressions: **31 passed**. Scoped Honolulu race proof/boolean checks pass in **2.169s**; scoped vet passes.
- Mark correction removed: **five runtime failures → five restored passes**. Request/cache fixes removed: **two runtime failures → two passes**. Proof policy removed: **one runtime failure → restored pass**. Web mutations restore exact bytes in `finally`; Go proof uses an overlay. Logs `/private/tmp/nats-timeline-no-data-{red,removed,restored,request-red,cache-red,request-removed,request-restored,ui-proof-red,proof-removed,proof-restored}.log`.
- Final production build passes with the unchanged large-chunk warning. Feature audit: **46 capability docs clean**, both index sections updated. No full web/Go/check-engine or broad store suite repeated. Numeric coverage was not instrumented or newly installed.

## Owned synthetic browser

The established agent-browser fallback followed two T3 snapshot timeouts earlier in this same Timeline acceptance work. Only an owned fixed demo on loopback18625 was accessed; no private18900 or other deployment.

An interval before the demo's 30-day retained history shows a real no-data lane. Native keyboard activation opens the shared persistent view with the missing-state explanation and unavailable retained-occurrence result. Escape closes and returns focus. At **360/761/1280**, the one actual no-data run has its default dashed outline, stays inside its track and causes no document overflow. A focused mark correctly uses the stronger solid accessibility outline; the default-style check explicitly blurs it first.

The first chosen date was still inside the demo's history and is excluded as a no-data fixture. A later optional observed-OK browser control instead contained three scenario warnings, so it is not counted as a positive OK pass. Its pending text wait ultimately failed with `Resource temporarily unavailable (os error 35)`; its valid earlier no-data JSON/interaction receipts remain separate. The observed-OK contrast and mixed/partial missing inputs are proven by the focused tests, not by that failed optional browser control.

Receipts: `/private/tmp/nats-timeline-no-data-browser.json`, `nats-timeline-no-data-browser-detail.json`, `nats-timeline-no-data-browser-close.json`, `nats-timeline-no-data-360.png`. Physical touch, native OS clipboard, throughput and full repository acceptance are not certified. Root owns fresh read-only review, final cleanup and integration.
