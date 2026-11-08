# Metric investigations

- **Surfaces:** `ui`, `api`, `cli`
- **Status:** partial — server, resource and aggregate metric pages; complete volumes and richer historical attribution remain limited
- **Related:** [metric-observations.md](metric-observations.md), [overview-page.md](overview-page.md), [servers.md](servers.md), [chart-interval-selection.md](chart-interval-selection.md), [agent-investigations.md](agent-investigations.md)

## What it does

Metric chart titles open CPU, memory, connection-count, message-rate or byte-rate investigations. Each page compares captured server observations, expands individual histories and opens findings or events in the displayed interval. Stream and consumer chart titles open resource histories and comparisons. Observed server/resource/account counts, replica-configuration groups and derived meta-leader changes have aggregate histories with supporting evidence links. Storage-change and slow-consumer investigations retain their existing specialist paths.

## Behavior and rules

- Server and unselected resource comparison pages offer **Rank matching server names/resources**. This separate panel ranks all matching observed entities before paging, instead of reordering only the existing twelve loaded histories. It supports the fifteen metric IDs represented by those pages; storage/detection specialists retain their own views.
- Ranking choices (metric/direction, latest, sample mean, observed peak/minimum, name, and supported exact counter increase) live in the URL. Unknown/nonfinite values sort last in either direction; ties use the entity key. Incoming and outgoing traffic rank separately. This does not identify causal contributors or integrate rates.
- The native `ListMetricContributors` API supports all eighteen server/stream/consumer catalogue metrics. It reuses the exact observation SQL and returns historical entities with retained reports in the selected interval, even after they leave the latest collection. Resource account/stream and literal name-substring filters apply before ranking; server measurements remain system-wide across accounts.
- Proposed, not yet confirmed: ranking captures allow at most 20,000 identities and 2 MiB of serialized summaries; overflow fails and asks for narrower filters, never silently returns a partial global rank. UI pages contain twelve rows; the API caps at 100 rows and 512 KiB per page. Native database work and aggregate concurrent-request memory are not hard-bounded by these application limits.
- Native identity selection and summary reads share one read transaction. Ranking order and values are then frozen in the existing bounded traversal cache (five minutes, eight contexts per provider, shared with other entity lists). This is not a database pin for subsequent histories or findings. Validated immutable sources rehydrate the same bounded capture for continuation after reopening.
- Ranking tables keep complete timestamp labels on one line inside their horizontally scrolling region at phone widths.
- The first live ranking page refreshes automatically; continuation retains its fixed range/order until Refresh list. Changing sort or scope starts a new traversal. Shared URLs/Back retain ranking choices and range; cursor position stays local. Each row opens an explicitly selected metric history or canonical entity diagnostics with the capture's fixed bounds. Individual reads can observe later backfilled data.
- Agents can request the same ranking through the existing read-only methods with explicit request JSON and one-page budgets. Ranking URL controls are not automatically translated from a copied page into an agent request; use the documented method/request or the returned exact machine context. See the [ranking reference](../reference/metric-contributors.md).

- Aggregate pages cover answering servers, ordinary streams, KV buckets, object stores, active accounts, durable consumers, configured replica groups and observed meta-leader changes. They read at most two native observation series, show actual sample times and unknown values, and refresh live ranges from a fresh source clock.
- Count pages show latest/minimum/peak observations. Meta changes show an exact decimal-integer observed total and the largest per-collection change count, distinct from bucket sums. A leaderless collection is not a recorded change, and derived transitions are not captured election-advisory counts.
- Aggregate counts cover the whole system; retained account context does not filter them. Inventory/evidence links use explicit system-wide displayed bounds and explain scope differences. Observed accounts differ from configured account inventory; replica-setting counts do not prove currentness or quorum. Missing R1 reports can reduce resource counts without a chart gap.
- An aggregate change does not identify a contributor or prove a lifecycle action. The page links to server/resource/placement/account diagnostics, captured events, retained findings and coverage, with capture and cross-query limits stated. No keys, objects or message contents are read.
- Every accepted API metric has a declared investigation destination when its required entity identity is available. Overview tiles use known metric identities even before history loads; combined asset components stay together. Mini-chart rows retain their entity-detail paths and shared interval actions. This destination coverage does not imply new profiling, complete historic set membership or exact event capture.


- Navigation preserves system, time range and account context. Server charts retain the reported server name; Compare all server names removes that selection. Server metrics cover all accounts. The retained account can scope supporting evidence without filtering the server measurements.
- Resource pages cover stored messages, logical bytes, observed replica bytes, stream ingestion, pending, ack pending, redelivered and delivery rate. They read one existing twelve-resource summary page, then only those histories in a batch. Account and consumer-stream filters apply before paging. An explicit resource selection reads its history directly, including when it has left the current list.
- Live selected-resource views and first comparison pages refresh automatically. Continuation pages stay fixed to their traversal until Refresh list; the page explains this distinction. Changing consumer metric families retains an active account/stream filter.
- Resource page continuation keeps the traversal’s returned bounds, collection ID and ordering. Refresh list starts a new traversal. Collection IDs remain decimal strings; a list pin does not pin independent histories or findings. The history table sorts its loaded page; the separate global panel ranks the full bounded capture. Local cursor position is not encoded into a shared URL.
- Resource count summaries show the latest value, observed minimum and peak. Rates and byte gauges show sample mean instead of minimum. Charts retain gaps and show bucket aggregates; summaries use pre-bucket observations. Stream logical bytes exclude replica multiplication; observed replica bytes omit unreported copies and are not physical file usage. KV/object backing-stream messages and bytes do not measure keys, objects or their contents.
- Resource chart links retain requested identities during loading or failure. Each history title uses that chart’s own fixed bounds, including a seven-day chart inside a shorter page range. Changing metric or resource does not show another resource’s previous history as a placeholder. Copied prompts retain metric and typed resource identity. Findings links use the selected resource account rather than an unrelated retained account selector.
- A fresh source clock resolves a live range before the comparison reads. Fixed ranges retain their endpoints. The page shows the requested interval and source clock; live comparisons refresh. It does not establish a universal collection pin.
- Proposed, not yet confirmed: twelve server names per page, alphabetically paged, with at most twenty-six history queries in one batch. The server inventory is an existing separate read, not a bounded ranking API. Filtering selects names from that inventory. The history table sorts its loaded page only; the separate global panel ranks the full bounded capture.
- Charts display bucket aggregates. Tables use observation summaries from before bucketing: latest known-or-unknown value, sample mean or connection minimum, observed peak and its timestamp. Means are sample-weighted. A fleet median peak is not an individual server peak. Missing values stay unknown; known/returned counts do not certify complete collection coverage.
- CPU is percent of one core and can exceed 100%; it is not host saturation. Memory is NATS process resident bytes, not host pressure. Connection counts do not identify clients. Incoming and outgoing traffic rates remain separate; the page does not integrate them into asserted volumes. Message/byte comparisons show exact observed counter increases separately per direction, admitted-pair counts, contributing stored server IDs and skipped-report details. Collection timestamps describe an envelope, not continuous coverage. No comparable evidence stays unknown; a comparable zero stays zero. The demo states that counter evidence is unavailable.
- Histories group by reported server name across process lifetimes. Process-start counts include initial starts. Simultaneous same-name processes cannot be separated here. Evidence links lead to server diagnostics and retained coverage.
- Selecting a supported chart interval offers Open metric details, Set time range and Copy as prompt. Actions preserve explicit endpoints; Back returns to the prior page or range. Other metric families retain their existing interval policies.
- Findings open in a full-screen, fixed-interval view, including muted, resolved and unknown states. Observation refresh does not close it. Server-scoped API evidence remains separate from the broader Findings page link. Events retain supported server/account filters. Related evidence does not establish causation.
- A storage title opened from a server discloses that its comparison is system-wide rather than claiming a server filter. Slow-consumer investigation retains its existing process/collection semantics.

## Scenarios

### Compare a CPU spike

**Surface:** `ui`

- **Given** an Overview CPU chart,
- **When** the reader opens its title or selects an interval and opens metric details,
- **Then** the page preserves that interval and system. It distinguishes captured peaks from bucket means and can expand a server history.

### Inspect historical evidence

**Surface:** `ui`

- **Given** a server row in a fixed interval,
- **When** the reader opens findings or events,
- **Then** supporting reads retain the displayed bounds. Missing observations stay unknown, and findings remain open during metric refresh.

### Share an entity comparison

**Surface:** `ui`

- **Given** a selected server and metric family,
- **When** the reader shares the URL or copies a prompt,
- **Then** metric identities, server selection and fixed bounds remain available without claiming cross-query snapshot identity.

## Not supported

Complete interval traffic volumes, resource sequence-volume summaries, attribution of aggregate counts to clients, simultaneous same-name process separation, universal collection pins and causal conclusions. These limitations keep the broader metric-investigation task open.

### Follow a consumer backlog

**Surface:** `ui`

- **Given** a consumer pending chart,
- **When** the reader opens its title and narrows the interval,
- **Then** the investigation retains account, stream and consumer identity, shows native observations and opens fixed-range evidence. A missing leader remains unknown rather than an empty backlog.

### Compare resource observations

**Surface:** `ui`

- **Given** an account-filtered resource metric page,
- **When** the reader advances the list,
- **Then** only the returned resources receive history reads, using the traversal’s fixed bounds. Sorting and comparisons describe that loaded page, not a global ranking.

### Follow an aggregate change

**Surface:** `ui`

- **Given** a count or meta-leader chart on Overview,
- **When** the reader opens its title or selects an interval,
- **Then** its investigation shows native observations and fixed-range evidence links, without claiming that the count identifies the affected resource.

### Find an entity outside the loaded history page

**Surface:** `ui`, `api`, `cli`

- **Given** a server outside the first twelve alphabetical names has the highest captured CPU peak
- **When** I open global ranking by observed peak
- **Then** it appears first across all matching observed names, with its actual metric timestamp and a fixed-range link to its history. Continuing the ranking retains the captured ordering and values.
