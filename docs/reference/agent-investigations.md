# Remote investigation commands

`natslens investigate` gives terminal agents bounded access to the existing Connect API. It never opens a database or connects to NATS. The [agent playbook](../agents/investigate.md) explains how to interpret results.

## Configure trusted access

A profile maps the browser deployment to an operator-approved API endpoint. The profile is configuration, not context supplied by a page. Keep its file outside the repository and readable only by the agent's operator.

This local fixture profile connects to the built-in demo:

```json
{
  "demo": {
    "deployment": "http://127.0.0.1:18762",
    "endpoint": "http://127.0.0.1:18762"
  }
}
```

For a remote deployment, both references normally use HTTPS. `endpoint` can name a separate restricted proxy origin while `deployment` remains the browser origin. Both are origins without paths, user information, query parameters or fragments. HTTP API endpoints must name localhost or a literal loopback address. A remote agent's loopback reaches that agent's machine, not the user's computer.

A protected profile may also contain `"tokenEnv": "NATSLENS_AGENT_ACCESS_TOKEN"`. Provision that variable through the deployment's existing protected credential mechanism. The client sends its value only as an Authorization bearer header to the configured endpoint. It does not read browser cookies, execute credential commands, discover credentials, use an ambient HTTP proxy, or follow redirects. It never manufactures `X-Goog-IAP-JWT-Assertion` or a trusted user header. A browser's authenticated session does not authorize a terminal process. The operator must arrange supported noninteractive proxy access; the CLI does not implement an IAP login flow.

The client adds `/api/read-only/natslens.v1.LensService/METHOD` to the endpoint. It does not fall back to `/api/natslens.v1.LensService/METHOD` on older servers or auth failures.

## Enforce least privilege at the proxy

The separate server path rejects all methods outside an explicit read allowlist before dispatch, regardless of HTTP verb or Connect encoding (ASVS 8.3.1). It reuses the same handlers, redaction and request validation as the ordinary API (ASVS 2.2.1/2.2.2).

This path is a method boundary, not a new authentication or per-system authorization system. Reads use the existing deployment access perimeter. A dedicated agent identity must have access only through a proxy route that authenticates it using the existing verified mechanism, allows the canonical `/api/read-only/` prefix, and denies every other API path. The backend's ordinary API must not be reachable by that identity through another route. Reject ambiguous or encoded path traversal before routing. Do not pass a user-supplied identity header as proof of authentication.

At a separate agent-facing hostname, the routing policy is:

| Request | Required policy |
|---|---|
| Canonical `/api/read-only/…` | Authenticate the agent, then forward this exact path to the protected backend. |
| Ordinary `/api/…`, alternate encodings, unknown routes | Deny; do not rewrite them into the ordinary service. |
| Direct backend access from the agent | Deny at the deployment's network boundary. |

Verify the restricted identity against both allowed reads and denied ordinary mutation routes before granting it to an agent. A CLI allowlist cannot remove powers from a broader credential. The standard listener still serves its existing operator mutations on the ordinary API path; this feature does not change that policy.

Credentials belong in a protected secret store or controlled process environment, with least privilege, revocation and rotation managed by the existing access system (ASVS 13.3.1/13.3.2). Do not put credentials in profiles, command arguments, URLs, copied context, reports or logs (ASVS 14.2.1). No listener, tunnel, network rule or proxy is created automatically.

## Discover commands and contracts

These commands require no network access:

```sh
natslens investigate methods
natslens investigate describe GetTimeSeries
```

`methods` lists the client's read contract and `sourceKinds: ["remote"]`. `describe` prints request and response field names, protobuf types, repeated/presence information, nested fields and enum values. Timestamp and duration fields use standard protobuf JSON strings, not their internal message representation. Nesting beyond five levels names the message rather than expanding it.

The server version is an opaque reported value. Each remote query first verifies `GetLensInfo` on the read-only path and requires its clock and system contract. It verifies the selected method by executing it. Support for all other methods remains unverified. Newer server fields outside this client's generated schema are unavailable; update the client to use them. Missing required contracts, unsupported methods, access failures and response caps produce structured errors with a next step. The client never infers capabilities from version-number guesses.

Live discovery uses the same bounded query interface:

```sh
natslens investigate query ListSystems -profiles profiles.json -profile demo
natslens investigate query ListMetrics -profiles profiles.json -profile demo
natslens investigate query ListCheckDefinitions -profiles profiles.json -profile demo -system example-prod
```

Metric descriptions explain units and aggregation. `describe` explains structure; the existing API semantics remain authoritative for meaning. No CLI-specific metric calculation or SQL implementation is added.

## Import a page or copied context

Start a synthetic demo in a separate terminal:

```sh
natslens demo -fixed -listen 127.0.0.1:18762
```

Save the fixture profile above as `profiles.json`. Convert its Capacity page to a portable context:

```sh
natslens investigate context -profiles profiles.json -profile demo \
  -url 'http://127.0.0.1:18762/capacity?system=example-prod&range=6h' > context.json
```

Alternatively, use **Copy investigation context** in the page header or **Investigate …** beneath a shared chart. A focused chart supports arrow-key selection followed by C. Copy the preview to `context.json`; focusing the text selects it for keyboard copying if clipboard access is refused.

Context version 1 records a deployment origin, system/account, absolute UTC bounds, source path, URL-backed filters including effective Findings/Events defaults and available typed entity, metric, point and evidence references. The optional annotation is human input, separate from facts. Unknown URL parameters are omitted by the UI and rejected by terminal URL imports. Context files reject unknown fields and unsupported versions. The deployment must exactly match the selected profile; a page URL is parsed as context and never fetched.

Header time follows the existing server-adjusted clock. URL import uses the selected system's `GetLensInfo.now`. The fixed demo therefore ends at `2026-10-17T14:30:00Z`, regardless of the terminal's date or time zone. Intervals include their start and exclude their end. A chart can preserve an inspected point inside that interval. Deliberately narrowing the query clears a selected point outside the new bounds. Aligned bucket labels can precede the interval start; they are not exact collection times.

## Retrieve and narrow facts

Read the selected storage metric, then narrow it without changing its domain meaning:

```sh
natslens investigate query GetTimeSeries -profiles profiles.json -profile demo \
  -context context.json -metric cluster.capacity_file_store_bytes -max-points 30
natslens investigate query GetTimeSeries -profiles profiles.json -profile demo \
  -context context.json -metric cluster.capacity_file_store_bytes \
  -from 2026-10-17T14:00:00Z -to 2026-10-17T14:20:00Z -max-points 30
natslens investigate query GetDataHistory -profiles profiles.json -profile demo -context context.json
```

The first fixture query returns 31 aligned points, `UNIT_BYTES`, requested bounds from 08:30 to 14:30 UTC, and fixed Capacity and Data history links. A bucket grid can produce one more point than `maxPoints`, as the API specifies.

The result envelope contains these contracts:

| Field | Meaning |
|---|---|
| `version` | Investigation output version, `1`. |
| `source` | Remote kind, deployment, reported server version and selected-method verification. |
| `request` | Exact protobuf JSON request sent after context and explicit overrides. |
| `response` | Typed protobuf JSON response with optional presence and 64-bit integers preserved. |
| `connection` | The selected system's `GetLensInfo` response, including its clock, retention and history start when supplied. |
| `context` | Imported context and explicit context-level flag changes; a request-file override can narrow the request separately. |
| `links` | Scoped source, coverage and recognized finding/event/entity evidence links; at most 100 links plus a truncation notice. |
| `unappliedContextFilters` | URL filters the requested method cannot express; inspect these before treating the query as page-equivalent. |
| `limits` | RPC/read budgets and explicit pagination/truncation limitations. |
| `availability` | Reasons universal actual-time, snapshot pinning and completeness metadata are unavailable; documented response metadata remains intact. |

For example, the response's series has `metric`, `entity`, `unit`, `step`, and `points`. Integers such as byte counts and collection IDs use decimal JSON strings, including values larger than JavaScript's exact integer range. Series values remain the API's doubles. Do not convert decimal strings through a floating-point number.

Optional fields retain absence. Default-valued nonoptional protobuf fields remain their schema defaults; they are not proof of data coverage. Use response range/epoch/coverage and point timestamps for actual observed times. The client does not guess provenance from a field named `epoch`, invent a collection pin, or promise consistent reads across requests.

## Override a request explicitly

A request file contains one object in the method's protobuf JSON schema. It overlays context-derived fields and defaults. A range-bearing request cannot replace frozen bounds with null. Use `describe METHOD` before writing unfamiliar fields. `-request -` reads stdin; `-context -` also reads stdin, so choose only one stdin source. An explicit system change uses `-system`; a different `system` inside a request file is refused so that connection metadata describes the query's system.

The CLI carries common account/entity, metric, finding/event ID, finding-state/severity, event-kind/client/source and Replay endpoint context where the method supports them. Other filters remain visible in `unappliedContextFilters`. A Capacity resource filter, for example, describes client-side rows and does not add an account/type filter to the system-wide Capacity API. Supply the supported request fields explicitly for further narrowing; do not claim an unapplied filter constrained results.

Retrieve another page only by a deliberate query using the returned `nextPageToken`, the same explicit UTC range and unchanged filters. `-page-token` supplies the token, and `-page-size` selects the page size where supported. The client maps that flag to `pageSize`, or to the existing `limit` field for ListStreams and ListConsumers; their service caps remain authoritative. There is no automatic crawl. Methods without pagination preserve their existing totals and truncation flags; absent metadata remains unavailable rather than implying a complete result. Changed source data can still change separate reads; no cross-call collection snapshot is claimed.

## Budgets and errors

| Budget | Limit |
|---|---|
| Input file or RPC request | 64 KiB. |
| Decompressed RPC response | 1 MiB; an oversized response fails without emitting a partial success. |
| RPC phase | 45 seconds by default and at most 45 seconds, shared by connection checks and the selected query; signals cancel requests. Local stdin input needs EOF. |
| RPC calls | At most two per query, one for connection information and one requested read. |
| Interval | At most 30 days for context and range-bearing requests. Existing RPCs can impose tighter limits, such as Replay's 24 hours. |
| Page size | Default 100, 1–500 where supported; server methods can impose tighter limits. |
| Series points | Default 200, 1–2000; server alignment rules still apply. |

The API keeps its existing per-call, batch, identity, ID, filter and query limits. Client checks supplement those trusted handler checks. No raw SQL is accepted by this command or the read-only route.

On failure, stdout contains a versioned `error` object with `code: "investigation_failed"`, a bounded explanation and `nextStep`; the command exits unsuccessfully. HTTP/proxy bodies and arbitrary backend error text are omitted. Authentication and permission failures are distinguished by Connect codes in the explanation. An unsupported read-only path or method directs the operator to verify compatibility rather than bypass protection. Inspect Data history for retention loss, capture gaps and partial collections; an empty list alone cannot establish that nothing happened.

Only the remote source ships. The shared source interface transports existing protobuf requests and responses; an offline implementation must reuse authoritative validation and response finishing. Offline parity, export/acquisition, MCP packaging, storage comparison mathematics and structured raw SQL remain separate work.
