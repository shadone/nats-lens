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

Context version 1 records a deployment origin, system/account, absolute UTC bounds, source path, URL-backed filters including effective Findings/Events defaults and available typed entity, metric, point and evidence references. Chart contexts also include `chartLabel` and `series`: labels, stable metric/resource IDs and an explicit `unavailableReason` for unknown identities. The flat `metric`/`entity` retain the first available query for single-series consumers. Plotted components from one API Series share its query identity; no new metric is invented for a band or its busiest-server points. The optional annotation is human input, separate from facts. Unknown URL parameters are omitted by the UI and rejected by terminal URL imports. Context files reject unknown fields and unsupported versions. The deployment must exactly match the selected profile; a page URL is parsed as context and never fetched.

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
| `context` | Effective request context after flags and request-file overrides; stale source queries and filters are removed. |
| `originalContext` | Original imported handoff, retained separately from query facts. |
| `links` | Scoped source, coverage and recognized finding/event/entity evidence links; at most 100 links plus a truncation notice. |
| `unappliedContextFilters` | URL filters the requested method cannot express; inspect these before treating the query as page-equivalent. |
| `limits` | RPC/read budgets and explicit pagination/truncation limitations. |
| `availability` | Reasons universal actual-time, snapshot pinning and completeness metadata are unavailable; documented response metadata remains intact. |

For example, the response's series has `metric`, `entity`, `unit`, `step`, and `points`. Integers such as byte counts and collection IDs use decimal JSON strings, including values larger than JavaScript's exact integer range. Series values remain the API's doubles. Do not convert decimal strings through a floating-point number.

Optional fields retain absence. Default-valued nonoptional protobuf fields remain their schema defaults; they are not proof of data coverage. Use response range/epoch/coverage and point timestamps for actual observed times. The client does not guess provenance from a field named `epoch`, invent a collection pin, or promise consistent reads across requests.

## Read every series in a copied chart

Use `query GetTimeSeriesBatch` with the copied context to request its known series in one bounded RPC. The client carries metric/resource identities into the existing batch request, without calculating or combining values. Unknown identities stay explicit in context and are listed under `unappliedContextFilters`; they never become guessed metric IDs. An explicit request file can supply a different `queries` list, and effective context then reflects that list.

## Collection and traversal provenance

ListStreams and ListConsumers supply an exact decimal-string `epoch`, an absolute `range`, and a continuation token. Continuing with that token and unchanged filters preserves the process-local ordered summary traversal for up to five minutes while retained. The CLI preserves these fields and never follows the token automatically. This is supported list pinning, not a transactional snapshot: history and readable account names can change, and expiry, eviction, restart or retention loss requires a new traversal.

Stream mini-chart contexts carry an observed collection evidence ID and use the API-provided end for their documented seven-day history window. That ID records what the UI read; it is not a general read-at-epoch selector. Page headers and other chart families do not copy a browser list cursor or establish a cross-RPC collection pin. A new CLI first page therefore starts its own traversal. Generic summaries, series, finding/event detail and configuration RPCs cannot be pinned to an arbitrary copied epoch through today's request contracts. Missing provenance stays unavailable rather than reconstructed.

## Override a request explicitly

A request file contains one object in the method's protobuf JSON schema. It overlays context-derived fields and defaults. A range-bearing request cannot replace frozen bounds with null. Use `describe METHOD` before writing unfamiliar fields. `-request -` reads stdin; `-context -` also reads stdin, so choose only one stdin source. An explicit system change uses `-system`; a different `system` inside a request file is refused so that connection metadata describes the query's system.

Scope is resolved before explicit request overrides. A selected typed chart or route resource owns its account and stream ahead of retained filters. A Consumers URL's `stream=APP/orders` selects APP even if `account=OTHER` remains in the URL. Events query selectors use consumer, then stream, then server; unrelated selectors on other pages do not invent a selected identity. Retained filters remain visible as imported data. The client projects the resolved resource into supported request fields once, after ordinary filters, so map iteration cannot select another account. Selected evidence IDs likewise outrank stale URL IDs. `-account` replaces imported scope for RPCs with an account field, and request-file fields are applied last. To replace a nested entity on a series/event RPC, supply the explicit typed `entity` request; an account flag does not rewrite nested resource IDs. Effective context and evidence links are rebuilt from the resulting request. None of this authorizes broader access or changes server validation.

The CLI carries common account/entity, metric, finding/event ID, finding-state/severity, event-kind/client/source, bucket/ephemeral/tooling visibility and Replay endpoint context where the method supports them. Returned query links are rebuilt from effective request scope. An `original_source` link is labeled separately. `availability.uiLinkScope` explains request options the destination UI cannot reproduce. Streams retains account context but does not filter list rows by account; its account-specific API requests therefore report partial UI scope, with exact resource links available. Other filters remain visible in `unappliedContextFilters`. A Capacity resource filter, for example, describes client-side rows and does not add an account/type filter to the system-wide Capacity API. Supply the supported request fields explicitly for further narrowing; do not claim an unapplied filter constrained results.

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

## Effective page filter audit

Connections resolves visibility before explicit request overrides: `tooling=1` includes tooling, and every nonempty `name` search includes matching tooling even with absent tooling or `tooling=0`. The raw URL value stays in the original context. Explicit `includeTooling: false` remains authoritative; with a nonempty effective name the returned `availability.uiLinkScope` is partial because the Connections UI cannot encode that exclusion. The link still opens, but its rows can differ. Inspect the exact request and response, not just the link.

The table describes current transport support, not semantic offline parity or an assertion that a page renders exactly one RPC response. Page/point budgets differ from UI pagination; no copied traversal cursor is reused. `unappliedContextFilters` describes filters retained from the original handoff that did not constrain the RPC. A link to effective scope cannot restore such an original filter.

| Page / query | Effective defaults and overrides | Scope limits |
|---|---|---|
| Connections / ListConnections | Tooling only for exact `1`, or any nonempty name; server/account/name projected. Explicit request fields win last. | Name + explicit tooling exclusion reports partial UI scope. |
| Applications / GetApplications | Account scope; tooling only for exact `1`. | `sort`/`dir` are retained but unapplied; ordering is not promised. |
| Findings / ListFindings | Open states; muted included unless `muted=0`; supported state/severity/check filters. | Unrepresentable state combinations and API entity restrictions report partial scope. |
| Events / ListEvents | Hide routine and fold repeats unless `show=all`; sources, kinds, client and selected entity. | Independent hide/fold overrides report partial scope. Facets/histograms are response metadata, not implicit row predicates. |
| Streams / ListStreams | KV/object streams excluded unless `kv=1`. | Account-specific API scope is partial in UI; summary-only/cursor options are not replayed by a link. |
| Consumers / ListConsumers | Ephemeral excluded unless `ephemeral=1`; selected stream owns its account. | API sorting can differ; a link does not replay a page token. |
| Stream, KV, object and consumer detail | Route/chart resource wins retained selectors; explicit request resource fields win afterward. | Typed keys, time and original context remain separate. |
| Subjects / GetSubjects, MatchSubject | Account scope; literal tester subject can be requested. | Imported `filter` remains unapplied; explicit enum/subject options not projected into effective links are disclosed as partial. |
| Micro services / GetMicroServices | Account scope, all services returned by the API. | `service` selection is not imported; a URL containing it is refused, and browser copies omit it. |
| Placement / GetPlacement | Unfiltered API matrix unless request fields supplied explicitly. | Imported filter/comparison/server-display state remains unapplied. Explicit unencoded filter/cell options report partial scope. |
| Replay / GetReplay | Explicit A/B preserved. Without them the client uses frozen range bounds. | The UI's dynamic strip-window defaults are not inferred; returned links carry effective explicit endpoints. |
| Overview, Fleet, Servers, Capacity, Accounts, Topology, Data history, Checks | Existing system/time/resource request fields; unsupported account or page-local filters remain unapplied. | Unencoded request options, including Data history's server selector, report partial scope. Topology `cluster` selection is not imported. |
| Copied chart / GetTimeSeries or batch | Known metric/resource identities and frozen times. | List-page filters such as name/tooling do not become series predicates; they remain unapplied. |

Only literal URL `1` enables KV, ephemeral or tooling toggles; `0`, `true` and omission do not, except Connections' name rule above. Findings uses the page's distinct `muted != 0` policy and Events uses `show != all`. The contract fixtures exercise absent, zero, one, noncanonical boolean strings and explicit false overrides. All read-RPC boolean inputs are inventoried in tests: includeMuted, hideRoutine, foldRepeats, includeFacets, includeOk, includeTooling, includeKv, summaryOnly and includeEphemeral. No current read RPC exposes `includeInternal`; such request fields are rejected. Unknown tags/order parameters are rejected on URL import and omitted from browser copies. No IDs, filters or ordering are inferred from those missing inputs.

## Returned context normalization and round trips

The returned version-1 context adds an optional `query` object with `method` and `request`. This is a canonical protobuf JSON read request, separate from the UI projection and `originalContext`. It binds only to the named method; it neither selects a connection nor dispatches a command. You still choose `investigate query METHOD`. Another method ignores that request binding and uses supported resource/metric context. Older contexts without it remain valid; older clients that reject the additive field need an updated client.

For the same method, reimport preserves every API field before explicit overrides: IDs, false/empty values, enums, query arrays, UTC bounds, buckets, budgets, sort and requested page token. UI links still disclose fields the browser cannot encode. A Consumers result also carries its typed stream identity and `selected_resource` link with a machine ID. An empty request-file account/stream/token clears it for the next request; omitted overrides retain the prior fields. Both protobuf and JSON field names are accepted through descriptors; duplicate spellings or repeated JSON fields are rejected rather than resolved by map order.

Explicit `-system`, `-account`, `-from`/`-to` and `-metric` rebase supported fields; explicit page/point flags replace saved budgets. A request-file field wins last. An explicit empty `-page-token ''` clears continuation. Reimporting a bound context without budget flags preserves its prior budget rather than silently replacing a one-row limit with 100.

`query` remains untrusted data. The read-method allowlist and generated input descriptor reject mutations, SQL, unknown/authentication fields and malformed values. The bound system must match the handshake context; range-bearing reads require matching frozen bounds. The normal input/range/page/point/read/deadline budgets and authoritative handlers still apply. Exported effective contexts exceeding the 64 KiB import budget fail with narrowing guidance instead of producing an unimportable artifact. No credential, endpoint, header or browser authority is stored in the binding.

| Input or path | Normalization / preservation | Verification |
|---|---|---|
| Events `kind` | Comma split, ECMAScript trim, empty entries dropped. FEFF and NBSP trim; U+0085 does not. | Actual UI outgoing requests, imported URLs and copied-context demo event 991. |
| Explicit kinds | Never trimmed or split. Whitespace, empty values or embedded commas can be unrepresentable in UI. | Exact request and reimport preserved; partial UI scope disclosed. |
| Events `src` | Known source membership in the UI’s canonical order; no trimming; blanks/unknowns ignored. | Go and actual UI predicate fixtures. |
| Findings `sev` | Exact known choices retained in input order; no trimming; unknowns ignored. | Go and actual UI predicate fixtures. |
| Subjects tester `subject` | ECMAScript trim for imported UI values; explicit API payload remains exact. | Go and actual UI query fixtures. |
| Account/stream/consumer/server, names/client/check text | No blanket trimming of identity payloads. Direct URL values follow the page model; form submission can normalize them before they enter the URL. | Typed identity precedence, retained whitespace, explicit override and stale-source controls. |
| All admitted read methods | Effective context → JSON → parser → request repeated three times preserves the protobuf request. | Table/property audit over every method, including unencoded options and false/empty values. |
| Consumers, Events, Subjects, Placement, Timeline and series | Result context reimport sends the same actual read-only RPC body. | Synthetic HTTP request capture and domain handlers, not only inverse-helper comparisons. |
| Consumers pagination | Fresh explicit continuation retains selected stream, one-row limit, actual range and reported epoch. Empty token clears it. | Actual bounded handler/CLI probes; no expiry assumption. |

To continue from a returned context without broadening its selection, save the `context` and choose the response token explicitly:

```sh
natslens investigate query ListConsumers -profiles profiles.json -profile demo \
  -url 'http://127.0.0.1:8080/consumers?system=example-prod&stream=APP/order-jobs' \
  -page-size 1 > first.json
python3 - <<'PY'
import json
from pathlib import Path
result = json.loads(Path('first.json').read_text())
Path('returned-context.json').write_text(json.dumps(result['context']))
Path('next-request.json').write_text(json.dumps({'pageToken': result['response']['nextPageToken']}))
PY
natslens investigate query ListConsumers -profiles profiles.json -profile demo \
  -context returned-context.json -request next-request.json
```

Use the documented fixed demo and a matching trusted loopback profile for this example. A returned binding stores the token used by its request, not the next token automatically. First-page requests do not acquire an arbitrary read-at-epoch selector. Actual collection epoch/range remain in typed responses, and only a retained explicit cursor pins the API’s bounded traversal. Browser page/chart copies still do not capture their traversal cursor; universal cross-query pins and semantic offline parity remain unavailable.

## Positive UI-equivalence proof

`availability.uiLinkScope: available` now requires a reviewed per-method projection and positive predicate comparison. The client reconstructs the linked page’s row-predicate projection, independently tested against actual React page requests. Page budgets and response metadata are classified separately rather than assumed byte-identical. A missing policy, including a newly added field at its zero/default value, produces partial scope. Numeric unknown enum values remain numeric in `context.query`; only a disposable UI-decoding view converts them to display text. The machine request is never normalized to make a link appear equivalent.

The proof concerns row predicates and selected identity, not identical page cardinality, response shape, aggregation or a cross-query collection pin. Page/point budgets remain explicit. Reviewed inactive defaults can express no restriction; active unencoded options remain partial. Fields without any UI projection, including exact metric/batch selections, remain partial even when empty. A working navigational link is still provided, and the exact machine context remains reusable.

| Field / request | Proven available scope | Partial / unavailable scope |
|---|---|---|
| Events entity | No entity; empty unspecified/system entities as the API's unfiltered predicate; server, stream and consumer type/key selectors | Account, client, connection and future entity kinds not projected by the link. The Events account toolbar parameter does not construct an entity predicate. |
| Events sources/kinds | Supported source membership; exactly representable split/trim/drop-empty kinds | Unspecified/future sources; kinds changed by the UI parser. Explicit machine values remain exact. |
| Findings severities | Empty means every severity; recognized critical/warning/info combinations | Unspecified zero, unknown numeric enums, or mixed supported/unsupported values. `sev=unspecified` is ignored by the UI and therefore cannot reproduce the explicit API predicate. |
| Findings states | Empty API list means open; supported open/flapping/resolved/ALL combinations | Unspecified zero, future states or unsupported combinations. Comparison uses the documented API default without rewriting the machine list. |
| Connections / Applications | Proven account/name/server/tooling rules, including Connections name-search inclusion | Connections name plus explicit tooling exclusion; unencoded options |
| Consumers / streams / buckets | Proven account/stream/ephemeral/KV rules where the page applies them | Streams API account restrictions, non-UI sort/cursors/summary options; unsupported selectors |
| Entity detail / OnCall | Canonical server/stream/consumer type/key identity and finding/event IDs; OnCall reads the finding route identity | Absent/unknown projection or contradictory selectors |
| Subjects / Placement | Default filter when the linked page applies the same predicate | Explicit enum or selected-cell options absent from the emitted UI projection, including future numbers |
| Timeline / series / catalog | Proven common scope fields where a projection exists | Exact unencoded metric/batch/bucket selection, or a catalog RPC without a dedicated UI projection |
| Other fields / oneofs | Reviewed scalar/message predicates must compare with the page request, including presence | Unclassified fields are partial even when unset; oneof presence differences cannot be silently treated as equal |

Events API entities use type/key as predicate identity. Optional redundant account/server/stream display components do not replace that identity. Returned UI metadata and selected-resource links derive canonical components from the typed key; `context.query` retains the exact original fields. A different read method can follow that projected resource without depending on optional metadata.

Independent fixture checks reproduce both mismatch counts: an account-entity Events request returns 1107 while its UI-link projection returns 1176; an explicit unspecified severity returns zero while the UI-link projection returns eight. Both report partial, and machine-context reimport retains the original predicates and counts. Actual UI request tests verify the account-versus-entity distinction and severity-token filtering; they do not merely call the Go proof helper. Prior source attribution, explicit clears, boolean false, normalization and pagination controls remain active.
