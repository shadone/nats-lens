# Agent investigation delivery verification

Status: proposed, not yet confirmed. This branch is an implementation candidate for fresh root-owned review, not an owner-approved integration. It starts at `d8ebff1e`; accepted pagination integration and the root's slow-counter work are intentionally absent.

## Requirement coverage

| Requirement | Implemented evidence and limits |
|---|---|
| Portable browser handoff | Version 1 context on page headers and shared chart inspection. Server-clock UTC bounds, system/account, known metric/entity IDs, selected point, URL-backed filters including Findings/Events defaults, source path and separate annotation. Clipboard preview and selectable fallback. |
| Collection provenance | Explicitly unavailable for a cross-query pin. Existing typed response collection/evidence IDs remain intact; Data history is linked. No provenance inferred from field names. |
| Immediately usable terminal path | `investigate methods`, `describe`, `context`, `query`. One `Source` contract and Connect/protobuf descriptors; no CLI SQL, metric or storage algorithms. Only remote access ships. |
| Discovery and facts | Live system/metric/check catalogs and existing summaries, series, finding/history, event, configuration, Replay and collection-coverage RPCs. Request files expose the generated contract. |
| Structured results | Protobuf JSON preserves 64-bit decimal strings, optional presence, units, bucket widths, identities, transitions, page tokens and response metadata. Universal actual-time, completeness, aggregation and provenance metadata have unavailable reasons and discovery next steps. |
| Scoped evidence links | Source and Data history, typed entity, finding and event links. Encoded slashes inside a finding ID remain one route segment. Narrowing drops a selected point outside the new interval. Links have an explicit count cap. |
| Trusted connection/auth | Operator profiles pin browser deployment and API origin. Context/page URLs never select a network destination. HTTPS remotely, explicit loopback HTTP, redirects refused, no ambient proxy/cookies. Protected environment credential reference only. No IAP assertion fabrication or automated login/tunnel/listener exposure. |
| Enforced read-only boundary | `/api/read-only/` dispatches an explicit server-side read allowlist. Mutation/SQL/unknown methods are rejected before dispatch. Existing handlers retain validation and redaction. The proxy must authenticate the agent and deny ordinary API/direct-backend access; a broad credential keeps broader powers. |
| Budgets and compatibility | 64 KiB input/request cap, 1 MiB RPC read cap, shared RPC deadline at most 45 s, explicit pages/points/ranges, no automatic continuation. Missing old clock contract and unsupported route/method fail explicitly; newer unknown fields are unavailable. |
| Honest limits | No collection pin, universal consistency promise, table-local state capture, dedicated Timeline-mark or drag/drop action, offline parity/acquisition/export, MCP packaging, structured raw SQL or new storage comparison mathematics. |

## Automated verification

- `TZ=Europe/Oslo go test -race ./internal/investigation -count=1 -timeout 90s -coverprofile=…`: passes, 83.9% statement coverage. Only synthetic HTTP fixtures and the built-in fixed demo are used.
- Focused `internal/api` read-boundary, range and batch-limit tests pass with the race detector. No broad API, full Go, checks or store-provider suite is run for this delivery.
- `go vet ./internal/investigation`: passes.
- Full web suite with two workers: 166 files, 1,669 tests passed, 24 skipped.
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

Root owns the fresh review, board status and integration. No reviewer, top-level thread, push, deployment, external evidence upload or private deployment access is performed. No slow-counter, store-provider, migration or generated protobuf file is changed.

The shared descriptor client will expose additive generated fields after root reconciles accepted pagination. Its discovery catalog is explicitly local, and the current branch does not claim sibling list-context/cursor behavior. Check the resulting cursor request contract against `describe` and the pagination reference during integration.
