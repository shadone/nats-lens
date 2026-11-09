# Shared captured Timeline selections

Status: proposed, not yet confirmed. Based on accepted `2273ab42`. This implements the captured-group all-results/share slice, not whole-Timeline or full-repository acceptance.

## Behavior and bounds

An overlapping group offers **Open all captured events**. Its separate `/timeline/events` route retains explicit membership, fixed context, optional event boundary, source-limit notices and current page. The bounded, data-only base64url fragment is capped at 64 KiB decoded and 2,000 canonical decimal int64 IDs. Query scope must agree with the fragment. The fragment avoids sending membership in HTTP request headers; it remains visible in history/shared URLs and provides neither authentication nor an immutable snapshot.

The additive `ListEventSelection` RPC defaults to/maxes at 50 summaries. It invokes the existing authoritative `GetEventBatch` once for only that page's keys, retaining the older 1–50-ID batch contract. Missing rows consume captured positions and remain unavailable, never substituted. Full membership count and current-page retained count are distinct. Responses obey the existing 512 KiB JSON/protobuf limit. No payload is fetched until a row is opened. No SQL business logic, migration, dependency or check behavior is duplicated or changed.

Both browser and remote CLI check membership, boundary, page metadata and returned row scope. Full URLs and copied contexts preserve the exact request through CLI serialization/reimport. The same native/fake/captured-source handlers implement the read. A machine-returned Events-feed link stays explicitly broader/partial context. The new method defaults to 50 in the CLI; an explicit larger page is rejected by the handler. Inferred-window support above 2,000 IDs retains its separate complete-window route; without an explicit system no share link is offered. Phone headers name captured event/window destinations while retaining their Overview navigation parent.

## Runtime checkpoints and removal controls

- `7053f2dd`: page/invalid-input/native/demo assertions failed at runtime; bounded reader GREEN `a0b98101`.
- `121614e5`: three share-codec assertions and agent admission failed at runtime; scoped codec/acknowledgement GREEN `d51aed3c`.
- `ebb8ea27`: two all-results view assertions failed; GREEN `297a70d9`.
- `f45c2f65`: CLI context import failed; GREEN `3f7e0a77`.
- `8e55fc5f`: global handoff omitted the bound query; GREEN `84a74566`.
- `b5377e70`: phone destination label failed; GREEN `8e6ff4d9`.
- The actual CLI caught an additional trusted-URL gate rejection. `9b265697` records that runtime assertion; GREEN `51d3f825` admits only the validated fragment after trusted-origin checking.

Corrected Go removal overlay preserves the canonical ID validator while removing paging, CLI URL import and reply checks: all affected assertions reach runtime failure. `/private/tmp/nats-selection-go-removed-corrected.log`; source is unmodified by overlays; restored suites pass in `nats-selection-go-restored.log`. An earlier overlay accidentally removed a shared validator and caused downstream compile failures; those failures are explicitly excluded.

Web production files temporarily restored to the RED checkpoints: **6 runtime failures → 6 restored passes**, restoration in `finally`. Separate phone-label and trusted-URL-gate removals each fail at runtime and pass restored. Logs: `/private/tmp/nats-selection-{web,header,url-boundary}-{removed,restored}.log`.

## Focused verification

- New transport parser/page package: **92.0% statement coverage**; invalid keys, bounds, membership, missing fields, page values and source changes covered.
- Focused native/API/demo/non-UTC race checks pass. Initial investigation/offline attempts were blocked by sandbox loopback binding; the authorized retry passes, including expanded frozen live/offline parity for the new method. `/private/tmp/nats-selection-race.log`, `nats-selection-race-loopback.log`.
- Maximum 2,000 int64 IDs fit the portable 64 KiB context budget, retain exact strings, and read only one 50-key batch. Cancellation/invalid input causes no batch read.
- Final affected UI selection: **32 tests pass**; additional clipboard/scope/prompt regression receipt recorded separately. Types pass. Production build passes with the existing large-chunk warning unchanged. One build overlapped a temporary navigation-removal control and is not counted; the final build after restoration is `nats-selection-build-final-restored.log`.
- Focused modified-package vet passes. Proto STANDARD lint/format/generation passed in `nats-selection-proto.log`; new RPC/messages/fields are additive. No installs. Feature audit: **46 capability docs, clean**, both index sections updated.
- No full Go/check-engine, broad store-provider or full-web suite repeated for this slice. Existing frozen full acceptance failures remain separate.

## Synthetic browser and real CLI

Only owned loopback demos were used; no live/private 18900 access. T3 snapshots twice returned `Preview automation snapshot timed out after 15000ms.` Installed agent-browser fallback used real production UI and fonts.

Keyboard opened a real **28-member** overlap and followed its all-results link. Its page retained all 28 IDs and had no lingering dialog. Opening a row displayed its stored event and explicit payload action. A bounded fixture built from 50 actual demo event IDs plus one deliberately unavailable ID exercised **51-member** paging. Native Next reached that unavailable identity, retaining its slot and original source-limit notice. Geometry checks at **360/761/1280** show no horizontal overflow. This fixture is explicitly supplied membership, not proof that a native visual overlap had 51 members.

The actual global button copied the second-page prompt through a controlled clipboard sink. The real CLI replay returned the same 51-ID membership, offset 50 and no substitute record. Full shared URL import then produced a byte-equivalent effective request. Physical touch, native OS clipboard ordering/readback, deployment recovery and throughput are not certified.

Receipts: `/private/tmp/nats-selection-browser-group.json`, `nats-selection-360.png`, `nats-selection-{761,1280}.json`, `nats-selection-browser-prompt.json`, `nats-selection-cli-{copied,url}-page.json`. Browser evidence before the small phone-label correction is qualified; the label itself has a focused removal/restoration test and final build. Fresh read-only review and integration are root-owned.
