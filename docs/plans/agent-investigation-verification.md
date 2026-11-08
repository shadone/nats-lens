# Agent investigation delivery verification

Status: proposed, not yet confirmed. This branch is an implementation candidate for fresh root-owned review, not an owner-approved integration. It starts at `d8ebff1e` and integrates the root-approved `b0c21dbb` boundary in `6a40e905` before R1 verification. Root-owned pagination, storage and slow-counter changes are preserved.

## Requirement coverage

| Requirement | Implemented evidence and limits |
|---|---|
| Portable browser handoff | Version 1 context on page headers and shared chart inspection. Server-clock UTC bounds, system/account, known metric/entity IDs, selected point, URL-backed filters including Findings/Events defaults, source path and separate annotation. Clipboard preview and selectable fallback. |
| Collection provenance | Accepted entity-list cursors pin bounded summary traversals with explicit epoch/range; stream mini-charts retain observed collection evidence. Cross-query pins remain unavailable. Data history is linked; no provenance is inferred from field names. |
| Immediately usable terminal path | `investigate methods`, `describe`, `context`, `query`. One `Source` contract and Connect/protobuf descriptors; no CLI SQL, metric or storage algorithms. Only remote access ships. |
| Discovery and facts | Live system/metric/check catalogs and existing summaries, series, finding/history, event, configuration, Replay and collection-coverage RPCs. Request files expose the generated contract. |
| Structured results | Protobuf JSON preserves 64-bit decimal strings, optional presence, units, bucket widths, identities, transitions, page tokens and response metadata. Universal actual-time, completeness, aggregation and provenance metadata have unavailable reasons and discovery next steps. |
| Scoped evidence links | Source and Data history, typed entity, finding and event links. Encoded slashes inside a finding ID remain one route segment. Narrowing drops a selected point outside the new interval. Links have an explicit count cap. |
| Trusted connection/auth | Operator profiles pin browser deployment and API origin. Context/page URLs never select a network destination. HTTPS remotely, explicit loopback HTTP, redirects refused, no ambient proxy/cookies. Protected environment credential reference only. No IAP assertion fabrication or automated login/tunnel/listener exposure. |
| Enforced read-only boundary | `/api/read-only/` dispatches an explicit server-side read allowlist. Mutation/SQL/unknown methods are rejected before dispatch. Existing handlers retain validation and redaction. The proxy must authenticate the agent and deny ordinary API/direct-backend access; a broad credential keeps broader powers. |
| Budgets and compatibility | 64 KiB input/request cap, 1 MiB RPC read cap, shared RPC deadline at most 45 s, explicit pages/points/ranges, no automatic continuation. Missing old clock contract and unsupported route/method fail explicitly; newer unknown fields are unavailable. |
| Honest limits | No universal collection pin, consistency promise, table-local state capture, dedicated Timeline-mark or drag/drop action, offline parity/acquisition/export, MCP packaging, structured raw SQL or new storage comparison mathematics. |

## Automated verification

- `TZ=Europe/Oslo go test -race ./internal/investigation -count=1 -timeout 90s -coverprofile=…`: passes, 85.4% statement coverage. Only synthetic HTTP fixtures and the built-in fixed demo are used.
- Focused `internal/api` read-boundary, range and batch-limit tests pass with the race detector. No broad API, full Go, checks or store-provider suite is run for this delivery.
- `go vet ./internal/investigation`: passes.
- Full web suite with two workers: 171 files, 1,689 tests passed, 24 skipped.
- TypeScript and production Vite build: pass. Vite reports the existing large-chunk warning; no bundle-size claim is made.
- Feature-docs audit with `--map-refs require`: clean, 40 capability docs. `git diff --check`: clean.

Runtime RED/GREEN checkpoints cover the read boundary, context contract and command/client behavior. Focused regressions fail with fixes removed and pass with them restored: selected-system connection metadata, finding source enum, null-range refusal, page-default filters, metric-box IDs, point narrowing and encoded finding links. Temporary source changes are restored and checked before delivery.

## Browser-to-client-to-browser verification

T3 preview is attempted first. Two consecutive snapshots fail with `Preview snapshot failed: Preview automation snapshot timed out after 15000ms.` The installed agent-browser workflow is then used in an isolated session.

The production binary serves only its fixed synthetic demo on an owned loopback port. IBM Plex Sans loads from local assets. Capacity's page width equals the viewport at 360, 761 and 1280 CSS pixels. Dialog bounds are 16–344 at 360, 120.5–640.5 at 761 and 380–900 at 1280. Screenshots are visually inspected.

- Storage: a real browser context is imported by the terminal client, queried over its six-hour interval, narrowed to 14:00–14:20 UTC and opened through the returned Capacity link. The result retains `UNIT_BYTES` and aligned points. This verifies access and handoff, not storage comparison mathematics.
- CPU: keyboard Home/ArrowRight/C opens a context containing `server.cpu_percent`, server `nats-0`, account context and a UTC point. The terminal retrieves and narrows the series. Its returned scoped server link opens the expected heading without an alert.
- Resolved finding/event: the copied resolved-state context includes effective mute visibility. The terminal retrieves `consumer-001:APP/order-jobs/job-cache-projector`, its two retained transitions, then captured finding event `991` (`finding.resolved`). Returned encoded finding and event links open their corresponding UI evidence without alerts.
- Copy: focusing portable JSON selects its full 559-character storage context; keyboard copy is exercised and the explicit action reports “Context copied”. The denied-clipboard fallback and pointer/keyboard selection persistence are covered in UI tests.

Generic screenshots and captured JSON remain under `/tmp/natslens-agent-evidence` on the implementation machine. They contain only the built-in demo. Owned demo/browser processes are stopped at handoff; those loopback links require restarting the documented demo to reopen later.

## Integration notes

Root owns the fresh review, board status and integration. No reviewer, top-level thread, push, deployment, external evidence upload or private deployment access is performed. R1 makes no independent slow-counter, store-provider, migration or generated protobuf edits beyond integrating the accepted baseline.

The shared descriptor client uses the accepted generated pagination fields. Discovery is explicitly local; method support remains checked against the selected server. Both bounded entity lists are verified against the accepted synthetic demo, including explicit continuation.

## R1 corrections on the accepted combined baseline

The isolated branch integrates accepted `b0c21dbb` before R1 corrections. The original reviewer probe is retained unchanged; its SHA-256 is `64c887f0e8b55637324f29f029b3a5171aff01130429df13c2a3c1ce49f42e64`.

- Pointer controls: a real trusted pointer hit the stretched anchor and navigated to Connections before the fix. Investigation buttons now share the protected control layer. Removing only that selector from the rendered stylesheet reproduces the anchor hit/navigation; reloading the unchanged fixed assets restores button hits and handoff. DOM event-handler tests alone are not used as proof of hit testing.
- Page budgets: the unchanged reviewer probe and real accepted demo handlers reproduce missing `limit`. Both stream and consumer requests now send the requested page size through their descriptor-checked existing limit field and return exactly one row for a one-row request, without following continuation. Reverting the mapping reproduces RED.
- Effective scope: canonical account/system fields cannot be overwritten by retained source queries. Effective context and links are rebuilt from request scope; original context/source are labeled separately. Request options without an equivalent UI control are disclosed under `availability.uiLinkScope`. Tests cover resource/entity changes, visibility, event/finding filters, observation IDs, global discovery and preservation of original context. The unchanged account-link probe is RED with the fix removed and GREEN after restoration.
- Chart identity: copied contexts include `chartLabel` and bounded `series` identities with labels, metric IDs, resources and unavailable reasons. Traffic directions and replicated/single assets retain both API query identities; band components share the one API query that supplies them. Known MiniLine identities are passed from their query owners. Stream seven-day mini-charts use the API-provided end and retain the observed collection ID. Batch queries reuse the existing RPC; unknown IDs are not invented.

Collection provenance is partial and method-specific. Accepted stream/consumer continuations pin bounded ordered summary traversals and report exact epoch/range. Generic RPCs have no arbitrary read-at-epoch input. An observed browser collection ID is evidence, not authorization or a reusable universal snapshot selector; copied page headers do not carry a browser traversal cursor. Offline parity/export, MCP, dedicated Timeline actions and table-local state gaps remain disclosed.

Final R1 browser checks use real IBM Plex Sans at 360, 761 and 1280 pixels. At each width all 13 linked chart handoff buttons (including five MetricBox controls) pass `elementFromPoint` hit testing. Trusted pointer and native Enter open Slow consumers context without navigation; viewport and document widths match. Multi-series Assets/Messages/Bytes contexts are copied from actual dialogs and imported by the CLI. The resulting batch requests retain both identities. Stream MiniLine keyboard selection uses ArrowRight/C and retains its resource, seven-day bounds and collection evidence.

Final R1 demo CLI probes request one stream and one consumer per page, assert outgoing `limit: 1`, one response row and an explicit token, then separately request a second page with the same epoch/range. An OTHER-to-APP account override produces APP request/context/evidence links and separately labels the original OTHER source. Streams UI currently retains account context without filtering its rows; `availability.uiLinkScope` states that limitation and exact resource links are supplied. No silent traversal or offline parity is claimed.

The four corrections have runtime RED → GREEN and removed-fix RED → restored GREEN evidence. Additional global-discovery and selected-resource account precedence regressions are also exercised before and after their fixes. The unchanged reviewer probe remains checked in byte-for-byte. Final evidence is under `/tmp/natslens-r1-evidence`, including width screenshots and copied contexts. These are synthetic local artifacts, not uploaded reports.
