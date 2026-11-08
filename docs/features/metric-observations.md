# Metric observation summaries

- **Surfaces:** `api`, `cli`
- **Status:** shipped
- **Related:** [overview-page.md](overview-page.md), [servers.md](servers.md), [agent-investigations.md](agent-investigations.md), [offline-datasets.md](offline-datasets.md), [chart-help.md](chart-help.md)

## What it does

Agents can request metric statistics before chart bucketing. The result distinguishes the largest captured per-collection value from a peak of bucket averages and keeps unknown latest values separate from measured zero. The [metric investigation pages](metric-investigations.md) consume these summaries for supported server metric families.

## Behavior and rules

- Set `include_observations` on GetTimeSeries or an individual GetTimeSeriesBatch query. Without it, the response and metric work remain unchanged. The existing 32-query batch, deadlines and per-query validation still apply. Proposed, not yet confirmed: opt-in observation ranges are at most 30 days; ordinary series retain their existing longer range limit.
- The summary uses the same metric and entity definition as the curve, before bucketing. Per-collection values can already be derived rates, counts or aggregates. They are not necessarily individual raw server reports, identified contributors or instantaneous hardware peaks.
- `sample_count` counts rows returned by that metric query; `known_count` counts their non-NULL values. Neither is an expected collection count. Some metric queries omit unavailable rows. Missing epochs, excluded comparisons and incomplete capture cannot be inferred from these counts alone.
- First and last sample times are collection timestamps of returned metric rows. Latest is absent when the last returned row has a NULL value, or rows tied at the latest timestamp are unknown or disagree. Mean is the arithmetic mean of known observations, not time-weighted. Minimum, maximum and the earliest timestamp of that maximum describe known per-collection values. Empty ranges retain no invented values or timestamps.
- Band metrics summarize per-collection medians separately from their raw lower/upper edges. A median peak is not the busiest individual server's peak. Server CPU is percent of one core and can exceed 100; it is not host saturation. Existing metric units and scope still apply.
- Summed event metrics also return `observed_total`, an exact decimal integer string. Rates do not receive an invented volume; gauges do not receive a sum. Observed slow-consumer increases are not unique affected connections, complete event capture or proof of a cause.
- Stored curves and their optional summary share one database read transaction. This is one coherent response, not a reusable collection pin or a universal snapshot across batch siblings or other RPCs. Live writes can proceed. Captured datasets keep their engine-enforced read-only mode and frozen clock.
- The read-only investigation client accepts the same request option and structured response for remote or offline sources. Demo summaries use the scenario's native sample grid and remain synthetic. Changing `max_points` changes chart resolution, not the observation summary.

## Scenarios

### Inspect a spike hidden by a coarse bucket

- **Given** captured CPU values of 10, 100 and 30 followed by an unknown value
- **When** I request an observation summary with one chart bucket
- **Then** its maximum is 100 with the observed collection time, its mean uses the three known values, and latest remains absent rather than repeating the earlier value.

### Request summaries in a bounded batch

- **Given** a batch with one opted-in query and one ordinary query
- **When** the API serves the batch
- **Then** only the opted-in result includes observations; an oversized observation range fails that query while the ordinary sibling can still succeed.

### Read the same evidence through an agent

- **Given** a configured remote profile or a validated offline dataset
- **When** I use `investigate query GetTimeSeries` with `includeObservations: true` in its request JSON
- **Then** the client returns the same observation fields and captured metric semantics through the existing read-only interface.

## Not supported / out of scope

- This foundation does not complete cross-metric drill-down, new comparison tables, traffic-volume attribution or chart-title navigation.
- It does not interpolate missing measurements, reconstruct server process identities or establish complete capture, exact event times or causation.
- Existing point values and numeric gauge/rate statistics use the series' floating-point representation. The exact integer string guarantee applies to `observed_total`, not every byte-valued gauge.
