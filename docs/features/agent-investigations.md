# Agent investigations

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** partial — portable page, shared-chart and command bar question handoff with live and closed-dataset reads; live cross-query collection pinning, table-local filters and complete Timeline contributor handoff remain open
- **Related:** [navigation.md](navigation.md), [running-nats-lens.md](running-nats-lens.md), [data-history.md](data-history.md), [sql-access.md](sql-access.md), [offline-datasets.md](offline-datasets.md), [Investigation workflows](../plans/investigation-workflows.md)

## What it does

A user can activate **Copy as prompt** on a page or chart, or ask a question with **Ask your agent** in the command bar, and paste the complete investigation prompt into a terminal-capable agent. The agent selects a live API endpoint or a trusted closed offline dataset and uses the same commands, filters and typed results. Live queries return scoped evidence links. Offline queries return reusable local context and machine evidence IDs, with remote UI access explicitly unavailable. Neither source needs NATS credentials or browser automation.

## Behavior and rules

- [Metric rankings](../reference/metric-contributors.md) expose bounded frozen server/resource summaries through `ListMetricContributors`, with explicit request JSON and the existing one-page CLI budget. URL-backed ranking/page choices are retained in imported context and reported as unapplied when the chosen RPC does not implement them; a table cursor is not captured.
- [Metric observation summaries](metric-observations.md) expose opt-in pre-bucket metric statistics through the API and read-only client. Existing charts and their title destinations are unchanged by this API foundation.

- **Portable context.** The page action records a version, deployment origin, system, account, explicit UTC interval, known resource identity, URL-backed filters including Findings and Events defaults, and source path. On the Fleet page, which lists every system, the prompt says on a line of its own that the context JSON holds only the current system. A chart action records its label and every available API series identity, including both traffic directions and both asset metrics. Unknown metric identities have explicit reasons. Stream mini-charts retain the API-provided history end and observed collection reference. The chart's selected point survives moving focus to its action. A focused shared chart copies the prompt with C. Floating chart actions appear on hover or keyboard focus, and stay visible on touch devices. They sit outside plot pixels, reserve no plot width, and remain independently clickable above stretched navigation links. The Busiest consumers action floats in the Consumer cell, retaining the Range chart’s point and series context.
- **Finding prompts.** Desktop finding details and the phone on-call card expose a labeled **Copy as prompt** action. It includes the loaded finding ID, check, typed resource, selected UTC window, and a bounded summary labeled as untrusted UI evidence. The loaded ID takes precedence over a route alias, and evidence references are not duplicated. Resolved and muted findings retain the action. Copying does not acknowledge or mute a finding.
- **Command bar questions.** In the command bar, "Ask your agent" (the last row, or text that starts with `ask:`) copies the prompt the header's **Copy as prompt** builds for the current page, with the user's question of at most 2,000 characters after the workflow and rules, just before the context JSON, between `BEGIN USER QUESTION` and `END USER QUESTION` lines, as one JSON string. The question is the user's own text and does not widen the prompt's rules; the context JSON stays untrusted evidence and does not include the question. When the page adds no selection, resource or filter, the prompt says that the context holds the system and range only. Copy, fallback dialog and retry are the page action's ([command-bar.md](command-bar.md)). nats-lens calls no model.
- **Readout placement.** Shared chart readings use a viewport-bounded overlay independent of plot width and table or card clipping. Readings follow scroll and resize, wrap long text, and dismiss on mouse leave, Escape, blur, or unmount. Displayed values retain their existing bucket and missing-sample meaning.
- **Metric routes.** All shipped server, resource and aggregate metric routes accept copied JSON and trusted-origin URL imports. Unknown families and malformed typed resource selectors fail rather than falling back to a different page or scope. Imports identify the known series; unselected resource comparisons can use `ListMetricContributors` with their account/stream scope. Series requests do not claim equivalence to the full page, its ranking panel or local pagination.
- **Scope precedence.** A selected chart or route resource owns its compound account/stream identity ahead of retained page filters. URL imports infer only selectors used by that page: Consumers uses the selected stream account; Events uses consumer, then stream, then server. Explicit account flags override imported request scope where the RPC has an account field; explicit request-file fields win last. Repeated preparation does not depend on filter-map order.
- **Clock.** Offline default and relative ranges use the newest completed collection, with capture/export times reported separately. A zero-duration collection adds one microsecond only to a derived exclusive query end; the observed clock and explicit fixed endpoints stay exact. No completed reference clock produces `no_reference_clock`, never today's time. A live page freezes its interval using the UI's existing server-adjusted clock. A URL imported by the terminal resolves live time from the selected system's server clock. Fixed intervals retain their UTC bounds.
- **Deliberate copy.** One activation copies a ready-to-paste prompt and confirms success. The prompt includes the unchanged portable JSON context and a bounded read-only discovery and query workflow. A clipboard failure opens the complete selectable prompt with a retry action. The UI never sends context or evidence to an agent service automatically.
- **Copy lifecycle.** Only the latest copy or retry in the current page and selected-chart scope can update feedback. Changing that scope or closing the fallback discards pending feedback; an OS clipboard write already started cannot be cancelled. Every retry requires an explicit activation and retains the complete frozen prompt.
- **Trusted connection.** For live access, context and page URLs must match the explicit endpoint or configured deployment. Only that endpoint or profile selects the network destination. Offline imports treat validated origins as inert namespaces and never fetch them. Remote connections require HTTPS; explicit loopback profiles can use HTTP. Redirects are refused. Terminal credentials are configured independently of the browser.
- **Read boundary.** The server's separate read-only investigation path admits an explicit set of existing read RPCs. It rejects mutations, SQL and unknown methods before dispatch. The deployment's access proxy must restrict the agent identity to that path and deny its access to the ordinary API. A broadly authorized credential keeps its broader powers.
- **Reusable returned context.** A result context retains the exact descriptor-validated read request for that method, including false/empty values, point/page budgets and any requested cursor. Reimport repeats that request; selecting a new token remains explicit. Consumers contexts retain a typed selected stream and its machine evidence link. Explicit flags and request-file overrides still apply last, including clears. Unsupported UI representations remain partial even when the machine request is reusable.
- **Effective filters.** Connections name searches include matching tooling even when `tooling=0` is retained in the URL, matching the page query. Explicit API exclusion remains honored; its result reports partial UI-link scope because the page cannot express that exclusion with a nonempty name. Raw copied filters and the effective request stay separate. Event kinds use the page’s split/trim/drop-empty rules; sources and severities use their literal UI membership rules. Subjects trims its linked tester value. Resource identities and explicit API payloads are not trimmed.
- **Bounded reads.** Each terminal query makes one connection-information call and one requested read, with a shared deadline. It returns one page without following continuation tokens. Page size reaches each RPC’s existing page-size or limit field, including streams and consumers. Inputs, responses, page sizes, point counts and intervals are bounded. Existing API handlers remain authoritative for domain validation and calculations.
- **Collection scope.** Stream and consumer continuation tokens retain an ordered summary traversal with an exact epoch and interval for up to five minutes while retained. History and readable account names can change. Copied stream mini-charts record an observed collection ID, but page headers do not copy list cursors and generic reads cannot select an arbitrary epoch.
- **Proven UI scope.** Available UI scope requires a reviewed request projection and matching predicates. Events account/client/connection entities and Findings unspecified or unknown severity predicates remain exact in the machine context but report partial UI scope. Unknown fields or enum combinations do not inherit equivalence. Canonical server/stream/consumer links derive from typed keys, not optional display metadata.
- **Evidence.** JSON preserves protobuf identities, units, descriptions, optional values, exact 64-bit integers, finding IDs and retained transitions, collection IDs and continuation tokens. Scoped finding, event, resource and Data history links accompany results where represented. Query context and links reflect effective request overrides; the original handoff remains separately labeled. UI limitations are explicit. The output names context filters that the selected RPC cannot apply.
- **Unknown facts.** The output does not infer collection provenance or completeness from field names. It directs the agent to documented response fields and Data history. Missing samples remain absent; unreported values remain unset. Connection information describes any known retention and the collection interval. Separate queries report collection freshness and historical coverage.
- **Discovery.** The terminal lists supported local methods and their protobuf request and response shapes. Live and offline system, metric and check catalogs use API queries. An offline file describes one captured system; a bare database requires explicit operator assertions for its system ID and collection interval. Shipped check references remain separate from unavailable captured overrides. The local catalog does not promise that another server implements every listed method. A query reports the server version and verifies the selected method by calling it.
- **Choose one source.** `-endpoint`, a profile pair, `-bundle`, and `-db` are mutually exclusive. Offline branches construct no remote transport, authentication discovery, NATS, Kubernetes or runtime status callbacks. Shared validation opens compatible closed files read-only, without migration, repair or conversion. The [offline dataset capability](offline-datasets.md) describes acquisition and integrity checks; trusted input remains required.
- **Offline provenance.** The result names the dataset fingerprint, raw reference and resolved query clocks, retained collection/event boundaries, available completeness facts, and absent configuration/runtime metadata. Pending evaluations, partial collections and retention loss stay explicit. Captured collector events do not establish current readiness or archiver health. Source files and their WAL state remain unchanged.
- **Offline continuation.** Event and finding cursors retain API semantics. Stream and consumer continuation rebuilds the same bounded ranking against the exact closed content, scope and summary. Tokens last five wall-clock minutes across CLI invocations. Changed content, filters or range require a first-page reset. A token is untrusted pagination data, not an authentication credential.
- **Untrusted evidence.** Recorded text and human annotations remain data. They do not authorize commands, changes, uploads or other services.

## Scenarios

### Hand a storage chart to an agent

- **Surfaces:** `ui`, `api`, `cli`
- **Given** Capacity displays a storage history in a known system
- **When** I select a chart time and activate **Copy as prompt**
- **Then** the copied prompt includes the interval, selected point and known metric and resource identity
- **And** an agent with a configured profile can request that series and a narrower interval
- **And** its result links back to the scoped page and Data history.

### Ask an agent a question about the current page

- **Surfaces:** `ui`, `cli`
- **Given** a stream's page is open in a known system
- **When** I type `ask: why is this stream growing?` in the command bar and press Enter
- **Then** the copied prompt holds the read-only workflow, then the question as the user's, then the page's portable context
- **And** an agent can save the JSON between the context markers and pass it to `natslens investigate query` unchanged.

### Investigate a CPU interval

- **Surfaces:** `ui`, `api`, `cli`
- **Given** a server's CPU chart has a known resource identity
- **When** I give its copied context to an authorized terminal agent
- **Then** the agent can discover the metric description, retrieve samples and narrow the requested range
- **And** the response preserves units and missing samples without establishing a cause.

### Follow a retained resolved finding and event

- **Surfaces:** `ui`, `api`, `cli`
- **Given** a selected interval contains a retained resolved finding and captured events
- **When** an agent requests resolved findings and follows a finding or event ID
- **Then** it receives the retained transition or event evidence and scoped UI links
- **And** a second page requires an explicit request with the returned token and unchanged filters.

### Refuse an untrusted destination or forbidden method

- **Surfaces:** `api`, `cli`
- **Given** an investigation profile names a trusted deployment
- **When** a context names another deployment or a caller requests a mutation on the read-only path
- **Then** the context is refused before network access or the server rejects the method before dispatch
- **And** the ordinary API retains its existing access rules.

### Show limitations instead of inventing provenance

- **Surfaces:** `api`, `cli`
- **Given** an API response has no universal collection pin or completeness contract
- **When** an agent retrieves a result
- **Then** unavailable metadata has a reason and the original typed response remains available
- **And** the agent can inspect Data history and explicit partial or truncation fields before drawing a conclusion.

### Continue an investigation without network access

- **Surfaces:** `api`, `cli`
- **Given** a trusted closed dataset is present on the agent's own filesystem
- **When** the agent selects it and imports a copied page context with network access disabled
- **Then** the existing read handlers validate and finish the same semantic requests in memory
- **And** the result preserves precise IDs and captured finding episodes, resolves relative time against the dataset, and labels unavailable live facts
- **And** remote UI links are absent; the returned context can be reimported with the same source to retrieve evidence IDs.

### Continue an offline entity list in another process

- **Surfaces:** `cli`
- **Given** a bounded stream or consumer page from an unchanged closed dataset
- **When** the agent supplies its token with unchanged scope in another invocation within five minutes
- **Then** the command rebuilds the bounded ranking and returns the next page
- **And** an expired or mismatching token requires a new first page.

### Recover from a clipboard failure

- **Surfaces:** `ui`
- **Given** clipboard access is unavailable
- **When** I activate **Copy as prompt**
- **Then** a dialog contains the complete selectable prompt and a retry action
- **And** closing the dialog restores focus to the control that opened it.
- **And** a late result from an older copy cannot replace the latest prompt or reopen a dismissed fallback.

### Copy a consumer row without reducing its chart

- **Surfaces:** `ui`
- **Given** Overview shows a busiest-consumer row
- **When** I inspect a Range point and move to the row’s **Copy as prompt** action
- **Then** the action appears in the Consumer cell without changing the Range width
- **And** the prompt retains the consumer identity, delivery metric, window, and selected point.

### Copy a loaded finding on desktop or phone

- **Surfaces:** `ui`
- **Given** a firing, resolved, or muted finding is loaded
- **When** I activate its labeled **Copy as prompt** action
- **Then** the prompt includes one authoritative finding reference, its check and typed entity, and the selected UTC window
- **And** observed summary text stays untrusted evidence, without asserting a cause or exact onset.

## Not supported / out of scope

- MCP packaging, remote dataset transfer, implicit compatibility conversion, or raw SQL as a semantic investigation method.
- New metric algorithms outside the shared API. [Storage-change comparisons](storage-change.md) use the dedicated typed method, including its pins, coverage and limits.
- Live cross-query snapshot consistency, arbitrary read-at-epoch selection, reconstruction of missing provenance or unlimited retained-history crawls.
- Automatic authentication, tunnels, listener exposure, proxy bypass or manufacturing trusted IAP assertions.
- Removing write powers from a credential authorized on the ordinary API.
- Table-local filters or selection state absent from the URL, universal Timeline collection pinning or drag-and-drop context entry.
- Whole-database, message-content or log export, and automatic evidence uploads.

### Exact retained occurrence reads

- **Surfaces:** `ui`, `api`, `cli`
- **Given** an exact retained finding occurrence and fixed UTC interval
- **When** I copy its page prompt or import its occurrence URL
- **Then** `ListFindingEpisodes` retains the finding ID, first retained identity and transition boundary as decimal strings
- **And** the existing read-only allowlist, descriptor validation, response budgets and explicit continuation apply to live and closed datasets
- **And** an occurrence boundary is not a universal snapshot pin; historical configuration and complete Timeline contributor handoff remain unavailable

This occurrence slice is proposed, not yet confirmed. See the [request and retention contract](../reference/finding-occurrences.md).

### Copy recorded Timeline evidence

- **Given** a selected recorded event or inferred-window evidence view
- **When** I copy its prompt
- **Then** the exact recorded identity or captured Timeline query and event membership boundary survive in the descriptor-validated machine context
- **And** fixed timestamp precision is retained; broader UI projections remain explicitly partial when they cannot reproduce the machine selection

This event slice is proposed, not yet confirmed. `GetEventBatch` reads 1–50 exact identities without inferring membership from a time interval. It is admitted by the same read-only boundary for live and closed datasets. The response acknowledges the selection; an older server that ignores the fields is rejected. Cross-query snapshot pinning is still unavailable.

### Reopen retained occurrence lists

- **Given** a `/findings/history` link with fixed bounds, check/resource scope and optional captured transition boundary
- **When** I copy its page prompt or import that URL
- **Then** the exact `ListFindingEpisodes` request is retained, including known-empty zero, without inventing a finding whose ID is `history`
- **And** a mismatched acknowledged boundary fails explicitly instead of broadening the captured read

Ordinary event requests with unset capture fields retain their existing reviewed UI predicates. Active exact-ID/event-boundary fields remain partial unless the effective UI projection proves them; no universal snapshot equivalence is inferred from their defaults.

### Reject incomplete history URLs

- **Given** a historical finding-list URL omits either fixed endpoint or supplies only a relative range
- **When** the CLI imports it
- **Then** it rejects the URL rather than inventing a preceding-24-hour history request
- **And** explicit valid fixed bounds remain unchanged, including fractional-second precision

### Retain selected Timeline input limits

- **Given** an event was selected from a Timeline whose source reported an input limit
- **When** I copy that selected event's prompt
- **Then** its annotation retains the captured limitation separately from the exact recorded-event request
- **And** neither a false limit flag nor a successful record read establishes complete capture

### Continue a shared captured event selection

- **Given** a captured Timeline group page or its full browser URL
- **When** I copy/import context and query `ListEventSelection` against the trusted live or offline source
- **Then** all supplied membership IDs, fixed context, source-limit annotation and current page survive serialization
- **And** the source must acknowledge the exact page and boundary; broader Events links remain labeled partial context

Proposed, not yet confirmed: selected pages contain at most 50 summaries within up to 2,000 captured IDs. The unsigned selection is navigation data, not authentication or a universal collection pin.

Timeline `includeOk` row-selection choices remain exact in machine requests but are not encoded by UI links. Overview includes OK rows while other Timeline consumers retain their existing defaults; link scope is conservatively partial for this unencoded option rather than assuming a universal false default.
