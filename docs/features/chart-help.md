# Chart help

- **Surfaces:** `ui`, `api`
- **Status:** shipped
- **Related:** [servers.md](servers.md), [overview-page.md](overview-page.md), [streams.md](streams.md), [consumers.md](consumers.md), [applications.md](applications.md), [capacity.md](capacity.md), [events.md](events.md), [data-history.md](data-history.md), [multiple-systems.md](multiple-systems.md), [replay.md](replay.md)

## What it does

A small About info control explains what a chart measures without a permanent paragraph underneath it. Help names the quantity and units, its scope and bucket calculation, and limitations needed to interpret the chart. Help supplements the chart's inspection and evidence links.

## Behavior and rules

- [Chart interval selection](chart-interval-selection.md) adds explicit storage-time selection and actions. Selection alone leaves navigation and the page range unchanged.

- **One interaction.** The shared InfoTip opens on hover, keyboard focus or activation. Clicking pins it; Escape, blur or another click dismisses it. Its button has an About label and an associated description. It is separate from chart navigation, sorting and plot inspection.
- **Viewport overlay.** Descriptions render outside clipping cards and table cells, bounded by the viewport. They reposition on scrolling or resizing and remain inside a native dialog's top-layer subtree when used there. Both themes use the shared tokens.
- **Authoritative metric definitions.** Registered metric charts use ListMetrics descriptions and declared units. The catalogue loads only when a description is requested and is cached across charts; it does not add initial page requests. Missing definitions and catalogue failures are explicit and do not turn into chart-data failures. Chart-specific context, when needed, remains separate from the base metric definition.
- **Non-metric summaries.** Counts, distributions, coverage strips and event histograms explain their own existing calculation; they do not invent a time-series metric ID. Server-detail storage/traffic/slow-consumer boxes retain their specific scope explanations. CPU, memory and connections on server details use the API catalogue like the shared trend charts.
- **Compact charts.** Mini-chart help belongs to the column header or shared legend rather than each plot cell. The Range plot keeps its width. Mini-chart inspection uses the actual label and a generic mean-value note unless its caller supplies a more specific aggregation note; bytes are not called delivery rates.
- **Independent behavior.** Help does not start range selection, sort a table, navigate a card or change the selected evidence. Existing missing-data marks and investigation links remain. Drag interval selection and richer metric drill-down are separate capabilities, not implied by this help control.

### Current surface inventory

| Surface | Help entry and definition |
| --- | --- |
| Overview metric tiles | MetricBox's explicit headline/history description |
| Overview, Servers and Capacity trend charts | TrendChart header; API catalogue descriptions and units |
| Five Servers summary cards | Card header; answering/version/restart/store/detection scope |
| Server detail metric boxes | Shared info control; catalogue for CPU/memory/connections and explicit context for specialized charts |
| Stream, consumer, KV and object-store detail charts | SeriesChart header; API catalogue |
| Streams' seven-day mini-charts | Stream column header; stream.bytes catalogue definition |
| Busiest consumers' delivery mini-charts | Consumer column header; consumer.delivered_rate catalogue definition |
| Compact Capacity resource histories | Resource column header; stream.bytes definition, with expanded logical/replica charts separately labeled |
| Application hourly traffic mini-charts | Traffic column header; complete-hour window, direction, per-row scale and missing-hour semantics |
| Events histogram | Histogram legend; captured event counts and routine-event scaling |
| Fleet status strips | Shared strip legend; worst observed finding state per bucket and coverage limits |
| Replay time/status strip | Time-selector help; A/B observations, state buckets and inferred windows |
| Data history coverage/gap strip | Strip help and existing coverage-card descriptions |
| Timeline lanes, cells and zoom | Lane help, shared legend and inspectable mark-specific explanations |

Replica diagrams, topology maps and server-colour maps retain their own structural/state legends and entity interactions. They are not registered time-series charts. Decorative icons, swatches and sorting arrows are not chart surfaces.

## Scenarios

### Understand a metric before interpreting a spike

- **Given** a chart with a known metric ID
- **When** I focus its About control
- **Then** the API description and declared unit appear in a viewport-bounded overlay, independently of the chart's loading state.

### Inspect a narrow table without shrinking its plots

- **Given** a table with compact resource or consumer histories
- **When** I inspect its header help
- **Then** the definition appears outside the cell, and no per-row help control takes width from the plot.

### Read help without sorting

- **Given** an application traffic column that sorts
- **When** I activate About Application traffic
- **Then** the description opens and the sort order stays unchanged.

## Not supported / out of scope

- Help is not a claim of complete capture, incident causation or continuous observation.
- Catalogue metadata does not supply missing measurements or create a metric when its identity is unknown.
- Help does not implement the separate chart drag-action menu, new metric investigation pages or historical episode APIs.
