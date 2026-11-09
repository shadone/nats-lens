# Selected Timeline input-limit disclosure

Status: proposed, not yet confirmed. Based on accepted `74106495`; the aggregate sharing/all-results portion of the Timeline task remains open.

## Change

A shared notice carries the original Timeline event-input limits into hover/focus previews, selected single/group details and both zoom layouts. Selected notices survive refresh because they belong to the stored evidence snapshot. Copied event annotations retain the same limitation. NATS/collector events, finding transitions and Kubernetes event inputs remain distinct. A source-range limit does not prove that a particular selected group or zoom window omitted members. It does not reclassify per-check state rows or separately derived window inputs, and false flags do not establish complete capture.

The notice explicitly names an input limit on the returned Timeline, rather than claiming a collector failure. No API query parameters, budgets, polling, payload loading, backend, proto, dependency, migration or check behavior changed.

## Focused verification

- Runtime RED checkpoint `30526a6e`: **5 failures /1 passing control** against the old code.
- Minimal GREEN `dee7cc60`; expanded controls exercise both source flags, expanded Kubernetes, wide/narrow zoom, selected single-record annotation, refresh and unrelated lane/window limits.
- Four affected suites: **26 tests passed** (`/private/tmp/nats-timeline-limits-focused.log`). Final wording-focused selection: **8 tests passed**. TypeScript and production build pass; the existing large-chunk warning remains.
- Final implementation removed by restoring six production files from the accepted base: **6 runtime failures /2 controls passed**. Production restored in `finally`; **8/8 pass**. `/private/tmp/nats-timeline-limits-{removed-final,restored-final}.log`. No compile failures count as RED.
- Feature-docs audit with `--map-refs require`: clean, **46 capability docs**, both index sections updated. No broad web or Go suite was repeated for this UI-only fix.

## Focused browser check

Only owned synthetic loopback fixtures were used. T3 snapshots twice returned `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` Installed agent-browser fallback was used. Its first request-interception attempt did not apply, so that attempt is not counted as a pass. A small local proxy on18616 forwarded only to the owned fixed demo on18615 and explicitly changed its Timeline input flags. This verifies production UI handling of a synthetic limited response, not native query truncation or throughput.

Native focus and Enter opened a **48-member** group. The final input-limit notice wraps within the phone dialog at **360px**, with no dialog/page horizontal overflow. The actual selected-record Copy as prompt action, activated by keyboard with a controlled clipboard sink, retains both input-family warnings and the exact GetEvent request. Physical touch and OS clipboard ordering/readback are not certified.

Receipts: `/private/tmp/nats-timeline-limits-browser-final.json`, `nats-timeline-limits-final-360.png`, `nats-timeline-limits-copy.json`. Existing component tests cover the other representations and source-scope controls. This does not claim complete capture, full repository acceptance, or completion of the Timeline umbrella.
