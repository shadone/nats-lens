# Historical Timeline all-results navigation

Status: proposed, not yet confirmed. Based on accepted `81a2c579`. This is a focused continuation of the Timeline umbrella, not whole-task or repository-wide acceptance.

## Change

The Findings lane now opens `/findings/history` using the captured fixed range and transition boundary. The page reuses the authoritative 50-row occurrence reader. Resolved and repeated firings remain separate; exact occurrence links retain first-retained identity, fixed bounds and membership. Per-check interval details offer the same route with typed check/resource scope. Invalid typed scopes or bounds never fall back to a broader read. Account navigation context alone is explicitly not an additional API predicate.

The Events lane destination now includes NATS, finding and collector sources rather than dropping two displayed source families. Both event lane links show all routine records without folding/hiding marked evidence and preserve captured event membership. These feeds provide source/range context; an exact overlap group remains its separate captured-identity list.

General page prompts and URL imports preserve the historical request instead of inventing a finding named `history`. Precise UTC bounds and known-empty zero survive. Browser and remote-client checks reject an ignored/mismatched requested occurrence boundary. This pins transition membership, not every mutable fact or retention.

## Runtime checkpoints and corrections

- `6245ac4d`: three runtime failures for the missing history page, invalid-scope handling and Findings lane destination.
- `d621e552`: direct history URL import fails at runtime on its captured boundary.
- `2986cdbb`: copied history context invents a finding ID and lacks its scoped query.
- `472d05f6`: ignored occurrence boundaries are accepted; three Go adversarial cases and the UI counterpart fail at runtime.
- `330f3eac`: Events links drop finding/collector sources and routine visibility.
- `9149ff17`: scoped historical navigation, exact context and acknowledgement implementation.
- `1bafec37`: adjacent Go proof tests expose a prior accepted-schema integration gap: added event capture fields were still classified as unreviewed even when unset. Existing assertions remain unchanged; new default/active controls extend the table. `adefc47f` reviews only the inactive defaults. Active selections, including optional zero, remain partial without proven projection; arbitrary unknown fields remain fail-closed.
- `90e1ee6b`: touch-sequence/refresh coverage and faithful recurrence fixtures. A jsdom-only keyboard attempt did not synthesize native dialog cancellation; the test now dispatches the native `cancel` event, matching existing dialog tests. No production Escape fix is claimed.

The original API fixture requested boundary zero but returned a nonzero capture with an occurrence. The positive fixture now requests its matching high boundary, with distinct acknowledged-empty and ignored-boundary controls. The two-recurrence page fixture shares one latest state while separating an earlier resolved occurrence from the later firing occurrence. No production query or condition semantics changed to accommodate those fixtures.

## Executed checks

Go environment: `GOCACHE=/private/tmp/nats-storage-go-cache GOPROXY=off GOTOOLCHAIN=local GOMAXPROCS=2`.

- `TZ=Pacific/Honolulu go test -race ./internal/investigation ./internal/offline -run 'Test.*(Finding|Occurrence|Episode|ContextRoundTrip|UIEntityAndEnumProof|OfflineLiveParity)' -count=1 -timeout=5m`: final pass, investigation **1.957s**, offline **5.254s**. Disposable HTTP fixtures required loopback permission. The earlier run's UI-proof failure is retained separately rather than called a pass.
- `go vet ./internal/investigation`: pass.
- Ten affected web test files: **96 tests pass**, including page routing, typed scope, raw timestamp precision, zero boundary, invalid selectors, repeated/resolved episodes, prompt creation/reimport, App composition, shared copy behavior and touch-sequence selection surviving refresh. Existing affected suites are reused. Physical touch hardware is not certified.
- `npx tsc --noEmit`: pass. `npx vite build --configLoader runner`: pass; existing large-chunk warning retained.
- Feature audit `--map-refs require`: **46 docs, clean**; both index sections updated. No proto, migration, dependency, native SQL or check changes.

No full Go/check-engine, broad store-provider or full web suite was repeated for this focused continuation. Prior frozen acceptance failures and the preceding captured-event slice's broad web receipt remain separately documented.

## Removal and restoration

Evidence directory: `/private/tmp/nats-history-navigation-controls/`.

- Go overlay of accepted pre-feature context, acknowledgement and proof-policy files: all three intended test groups fail at runtime. Restoration passes those same groups. Logs `go-removed-red.log`, `go-restored-green.log`.
- Restoring old lane/context/API-adapter wiring and the missing page stub: **8 runtime failures /17 passes**. `finally` restores all production files. The same four test files then pass **25/25**. Logs `ui-removed-red.log`, `ui-restored-green.log`.

No compile failures are used as RED evidence. Temporary UI mutations were restored before subsequent source checks. Go overlays did not mutate checkout files.

## Synthetic browser and CLI

Only owned fixed demo `127.0.0.1:18613`, system `example-prod`; no private port 18900, database, config, cluster or logs were accessed.

T3 preview snapshot twice returned `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` Installed agent-browser fallback verified native keyboard Findings-lane navigation to a fixed history route. It displays **20 occurrences**, **12 recorded resolutions** and **five separate recurrences of one finding**. Real fonts and no horizontal overflow at **360/761/1280**. JSON `/private/tmp/nats-finding-history-browser.json`; screenshots `/private/tmp/nats-finding-history-{360,761,1280}.png`.

The actual general copy action was keyboard-activated with a controlled clipboard sink, not an OS clipboard claim. Its context sends `ListFindingEpisodes`, exact range and membership boundary; no finding named `history` is invented. The real CLI returns the same **20 occurrences**: `/private/tmp/nats-finding-history-copy-context.json`, `nats-finding-history-copy-cli.json`. Existing transport/UI links can still be partial for unprojected API options; the exact machine request remains available.

Fresh read-only review and integration are parent-owned. Remaining umbrella acceptance includes complete aggregate share/all-results behavior and capture-limit disclosure across every representation; this slice does not imply universal snapshot pinning, complete capture or causal attribution.
