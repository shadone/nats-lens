# Timeline retained-occurrence slice verification

Base: accepted `develop` at `d87bb7daa8563b47a89edf81b8333d1a2ed003db`. This is a partial implementation of the Timeline investigation task, not completion of its overlapping-event/window work. Policy is proposed, not yet confirmed.

## Result

Retained firing occurrences remain separate from later recurrences. Timeline captures a transition high-water mark in its read transaction, including a known empty mark. Selected ranges use this mark; each occurrence has an exact route and bounded raw history. Historical transition state, observed resolution and latest retained finding state are distinct. Missing historical onset/configuration/operator facts are unavailable. A missing resolution does not extend an old fragment into a separately recorded recurrence.

The same generated handler and read-only admission serve live and captured sources. Frozen HTTP/offline parity covers occurrence lists and exact raw history. `TestOfflineOccurrenceCLITwoProcessesContinue` uses the real CLI dispatcher in two OS processes: page one returns occurrence 3, page two reopens the closed source and returns occurrence 1. Source fingerprint, scope, membership, timestamp precision, expiry and response limits remain explicit.

The Timeline read transaction also avoids a second-connection event-decoration deadlock. Labels share the existing formatter; unused current-finding decoration is not fetched for Timeline lane DTOs. The one-connection test failed with a deadline before this correction and passes restored.

## Executed checks

- Full web once: `vitest run --configLoader runner --maxWorkers=2`: **207 files, 1,923 passed, 24 existing skips**, 74.45 seconds. `/private/tmp/nats-timeline-occurrence-full-web.log`.
- TypeScript and production Vite build passed. The existing 500 kB chunk warning remains visible. No threshold was raised.
- Focused non-UTC race command:
  ```sh
  TZ=Pacific/Honolulu GOCACHE=/private/tmp/nats-storage-go-cache GOPROXY=off GOTOOLCHAIN=local GOMAXPROCS=2 go test -race ./internal/store ./internal/api ./internal/api/storeprov ./internal/api/fake ./internal/offline ./internal/investigation -run '^TestFindingEpisode|^TestEpisodeCursor|^TestTimeline|^TestDemoFindingEpisode|^TestOfflineLiveParity$|^TestFindingOccurrence|^TestAddedReadBoolean|^TestEveryReadBoolean' -count=1 -timeout=5m
  ```
  All six packages passed; `/private/tmp/nats-timeline-occurrence-race.log`.
- Full focused investigation package passed in 1.037 seconds. Disposable HTTP tests needed loopback permission; sandbox listener denials are not correctness failures.
- Supplemental wire-budget/cancellation and frozen live/offline controls passed under Honolulu (provider 1.863 seconds, offline 2.822 seconds). Two-process CLI passed in 1.392 seconds.
- Modified-package vet, proto lint/format checks, whitespace checks and feature-doc audit passed: **46 capability docs**. No migration, dependency, check, or module changes.

The final responsive class/CSS correction followed that full web pass. Its focused suites passed (11 tests, then 4 tests after adding card padding/action spacing), types and rebuilt assets passed. The full web suite was not repeated for those class/style changes.

## Runtime regression controls

Initial failing tests were committed before their corresponding implementations. Saved explicit removal overlays reproduce runtime assertion failures, not compilation failures:

| Removed behavior | Runtime failure | Restored receipt |
| --- | --- | --- |
| Separate occurrence grouping | Two firings collapse to one | Focused store and race checks |
| Timeline transition boundary | Later recurrence and late checks enter an old/empty capture | Focused provider and race checks |
| Missing-ending membership boundary | Old occurrence borrows the later recurrence | Honolulu store and race checks |
| Latest change/flapping mapping | Latest flags incorrectly read as firing | Focused provider and race checks |
| Read-only method admission | Bounded occurrence method unavailable | Investigation admission checks |
| Exact occurrence URL support | Source path rejected | Exact URL/context checks |
| New Timeline occurrence view | Both new UI contracts fail against the old collapsed projection | Restored focused/full web |

Overlay logs are in `/private/tmp/nats-timeline-occurrence-controls/`; latest-state removal is `/private/tmp/nats-episode-latest-removed-red.log`. UI removal uses a disposable tracked export, not mutations during the full web run; receipt `/private/tmp/nats-timeline-ui-removal/removed.log` shows two intended failures. Precision, retention-membership, scope, metadata and one-connection RED/GREEN receipts retain the `nats-episode-*`/`nats-timeline-*` names referenced in the implementing commits/session artifacts.

## Browser checks and qualifications

Only the owned fixed synthetic demo on loopback port 18611 was used. No user server, private database or cluster was accessed. T3 opened but both snapshots returned `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` The installed agent-browser fallback was used.

Keyboard activation opened the actual Timeline finding dialog and followed its exact occurrence link. Its URL retained system, fixed interval, first retained identity and transition boundary. Native pointer activation was not independently established; a DOM activation diagnostic also ran. A documented `find ... focus` subaction was unsupported; the CLI doctor reported 47 passes, and direct `focus` plus Enter worked.

Real embedded assets were checked at **360/761/1280**. A first phone screenshot exposed a zero-width value column. Before correction all three `dd` widths were zero. Removing the scoped facts class after correction reproduces zero widths; restoring it gives readable full-width phone values. Final card padding, wrapped action spacing, contained raw-history table and no document overflow are checked by `/private/tmp/nats-occurrence-browser-check.py`; geometry is `/private/tmp/nats-occurrence-browser-geometry.json`, with screenshots `/private/tmp/nats-occurrence-{360,761,1280}.png`.

A controlled clipboard mock captured the complete prompt from the real occurrence button. The real CLI queried the protected read-only path using that context and a generic loopback profile, returning exactly the selected identity. Receipt: `/private/tmp/nats-occurrence-browser-cli.json`. UI link equivalence remains honestly `partial` for the new RPC. This is not OS clipboard/touch certification.

## Limits

No full Go/check-engine or broad store-provider suite ran for this slice. The earlier frozen 87200340 acceptance failure and its separate fixture/audit reconciliations remain documented; this is not a new full-suite pass. SQL engine work and raw native allocations are not hard bounded by application page guards. A transition high-water mark is not a universal snapshot, and latest metadata can change. Retention may invalidate traversals or exact history. Missing evidence is never converted to a healthy interval or a newer occurrence. Demo reference frames are synthetic identities, not native collection IDs.

Remaining Timeline task work includes complete overlapping-event contributors, richer inferred-window evidence, broader historical lane navigation, and universal agent handoff. Fresh read-only review and develop integration remain pending at this receipt's creation.
