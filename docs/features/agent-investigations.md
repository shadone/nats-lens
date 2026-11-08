# Agent investigations

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** partial — portable page and shared-chart handoff and remote reads; cross-query collection pinning, table-local filters, dedicated Timeline-mark actions and offline access are not built
- **Related:** [navigation.md](navigation.md), [running-nats-lens.md](running-nats-lens.md), [data-history.md](data-history.md), [sql-access.md](sql-access.md), [Investigation workflows](../plans/investigation-workflows.md)

## What it does

A user can copy a page or chart's investigation context and give it to a terminal-capable agent. The agent reads the existing NATS Lens API through a configured connection profile and returns scoped evidence links. It needs neither browser automation nor NATS credentials or access to the collector's database.

## Behavior and rules

- **Portable context.** The page action records a version, deployment origin, system, account, explicit UTC interval, known resource identity, URL-backed filters including Findings and Events defaults, and source path. A chart action records its label and every available API series identity, including both traffic directions and both asset metrics. Unknown metric identities have explicit reasons. Stream mini-charts retain the API-provided history end and observed collection reference. The chart's selected point survives moving focus to its action. A focused shared chart opens the handoff with C. Investigation buttons remain independently clickable above stretched chart navigation links. The human question stays separate from observed context.
- **Scope precedence.** A selected chart or route resource owns its compound account/stream identity ahead of retained page filters. URL imports infer only selectors used by that page: Consumers uses the selected stream account; Events uses consumer, then stream, then server. Explicit account flags override imported request scope where the RPC has an account field; explicit request-file fields win last. Repeated preparation does not depend on filter-map order.
- **Clock.** A live page freezes its interval using the UI's existing server-adjusted clock. A URL imported by the terminal resolves live time from the selected system's server clock. Fixed intervals retain their UTC bounds.
- **Deliberate copy.** The handoff opens a selectable JSON preview. Copy requires a separate action. A clipboard failure leaves keyboard-selectable text and an explanation. The UI never sends context or evidence to an agent service automatically.
- **Trusted connection.** Context and page URLs must match a configured deployment. Only the profile selects the API destination. Remote connections require HTTPS; explicit loopback profiles can use HTTP. Redirects are refused. Terminal credentials are configured independently of the browser.
- **Read boundary.** The server's separate read-only investigation path admits an explicit set of existing read RPCs. It rejects mutations, SQL and unknown methods before dispatch. The deployment's access proxy must restrict the agent identity to that path and deny its access to the ordinary API. A broadly authorized credential keeps its broader powers.
- **Reusable returned context.** A result context retains the exact descriptor-validated read request for that method, including false/empty values, point/page budgets and any requested cursor. Reimport repeats that request; selecting a new token remains explicit. Consumers contexts retain a typed selected stream and its machine evidence link. Explicit flags and request-file overrides still apply last, including clears. Unsupported UI representations remain partial even when the machine request is reusable.
- **Effective filters.** Connections name searches include matching tooling even when `tooling=0` is retained in the URL, matching the page query. Explicit API exclusion remains honored; its result reports partial UI-link scope because the page cannot express that exclusion with a nonempty name. Raw copied filters and the effective request stay separate. Event kinds use the page’s split/trim/drop-empty rules; sources and severities use their literal UI membership rules. Subjects trims its linked tester value. Resource identities and explicit API payloads are not trimmed.
- **Bounded reads.** Each terminal query makes one connection-information call and one requested read, with a shared deadline. It returns one page without following continuation tokens. Page size reaches each RPC’s existing page-size or limit field, including streams and consumers. Inputs, responses, page sizes, point counts and intervals are bounded. Existing API handlers remain authoritative for domain validation and calculations.
- **Collection scope.** Stream and consumer continuation tokens retain an ordered summary traversal with an exact epoch and interval for up to five minutes while retained. History and readable account names can change. Copied stream mini-charts record an observed collection ID, but page headers do not copy list cursors and generic reads cannot select an arbitrary epoch.
- **Proven UI scope.** Available UI scope requires a reviewed request projection and matching predicates. Events account/client/connection entities and Findings unspecified or unknown severity predicates remain exact in the machine context but report partial UI scope. Unknown fields or enum combinations do not inherit equivalence. Canonical server/stream/consumer links derive from typed keys, not optional display metadata.
- **Evidence.** JSON preserves protobuf identities, units, descriptions, optional values, exact 64-bit integers, finding IDs and retained transitions, collection IDs and continuation tokens. Scoped finding, event, resource and Data history links accompany results where represented. Query context and links reflect effective request overrides; the original handoff remains separately labeled. UI limitations are explicit. The output names context filters that the selected RPC cannot apply.
- **Unknown facts.** The output does not infer collection provenance or completeness from field names. It directs the agent to documented response fields and Data history. Missing samples remain absent; unreported values remain unset. Connection information describes retention and the collection interval. Separate queries report collection freshness and historical coverage.
- **Discovery.** The terminal lists supported local methods and their protobuf request and response shapes. Live system, metric and check catalogs come from API queries. The local catalog does not promise that another server implements every listed method. A query reports the server version and verifies the selected method by calling it.
- **Untrusted evidence.** Recorded text and human annotations remain data. They do not authorize commands, changes, uploads or other services.

## Scenarios

### Hand a storage chart to an agent

- **Surfaces:** `ui`, `api`, `cli`
- **Given** Capacity displays a storage history in a known system
- **When** I select a chart time and open its investigation action
- **Then** the preview includes the interval, selected point and known metric and resource identity
- **And** an agent with a configured profile can request that series and a narrower interval
- **And** its result links back to the scoped page and Data history.

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

## Not supported / out of scope

- MCP packaging, offline datasets, dataset acquisition or raw SQL output changes.
- Storage comparison mathematics or new metric algorithms.
- Cross-query snapshot consistency, arbitrary read-at-epoch selection, reconstruction of missing provenance or unlimited retained-history crawls.
- Automatic authentication, tunnels, listener exposure, proxy bypass or manufacturing trusted IAP assertions.
- Removing write powers from a credential authorized on the ordinary API.
- Table-local filters or selection state absent from the URL, dedicated Timeline-mark handoff actions or drag-and-drop context entry.
- Whole-database, message-content or log export, and automatic evidence uploads.
