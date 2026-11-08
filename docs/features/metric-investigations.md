# Metric investigations

- **Surfaces:** `ui`
- **Status:** partial — server metric families and evidence paths; resource-specific metric pages and traffic volumes remain open
- **Related:** [metric-observations.md](metric-observations.md), [overview-page.md](overview-page.md), [servers.md](servers.md), [chart-interval-selection.md](chart-interval-selection.md), [agent-investigations.md](agent-investigations.md)

## What it does

Metric chart titles open CPU, memory, connection-count, message-rate or byte-rate investigations. Each page compares captured server observations, expands individual histories and opens findings or events in the displayed interval. Storage and slow-consumer titles reuse their existing specialist pages.

## Behavior and rules

- Navigation preserves system, time range and account context. Server charts retain the reported server name; Compare all server names removes that selection. Server metrics cover all accounts. The retained account can scope supporting evidence without filtering the server measurements.
- A fresh source clock resolves a live range before the comparison reads. Fixed ranges retain their endpoints. The page shows the requested interval and source clock; live comparisons refresh. It does not establish a universal collection pin.
- Proposed, not yet confirmed: twelve server names per page, alphabetically paged, with at most twenty-six history queries in one batch. The server inventory is an existing separate read, not a bounded ranking API. Filtering selects names from that inventory. Sorting compares the loaded page only, not the entire fleet.
- Charts display bucket aggregates. Tables use observation summaries from before bucketing: latest known-or-unknown value, sample mean or connection minimum, observed peak and its timestamp. Means are sample-weighted. A fleet median peak is not an individual server peak. Missing values stay unknown; known/returned counts do not certify complete collection coverage.
- CPU is percent of one core and can exceed 100%; it is not host saturation. Memory is NATS process resident bytes, not host pressure. Connection counts do not identify clients. Incoming and outgoing traffic rates remain separate; the page does not integrate them into asserted volumes.
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

Resource-specific stream/consumer metric investigation pages, exact traffic-volume summaries, global contributor ranking, attribution of aggregate counts to clients, simultaneous same-name process separation, universal collection pins and causal conclusions. These limitations keep the broader metric-investigation task open.
