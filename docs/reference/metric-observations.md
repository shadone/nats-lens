# Metric observation summaries

This API foundation supports metric investigations; it does not add the planned cross-metric detail pages. See the [coverage inventory](../plans/metric-investigation-coverage.md) for the open UI work.

## Request

Use `includeObservations` on GetTimeSeries, or on individual GetTimeSeriesBatch queries. The normal system/entity/range validation still applies. Observation requests are limited to 30 days and the existing 32-query batch/deadline budgets. The option defaults to false; no observation read transaction or summary aggregate runs for ordinary requests.

A request file for an agent:

```json
{
  "metric": "server.cpu_percent",
  "entity": {"type": "ENTITY_TYPE_SERVER", "key": "nats-0"},
  "range": {"start": "2026-10-17T13:30:00Z", "end": "2026-10-17T14:30:00Z"},
  "maxPoints": 1,
  "includeObservations": true
}
```

```sh
natslens investigate query GetTimeSeries -profiles profiles.json -profile shop \
  -system example-prod -request metric.json
```

The same method and option work with the existing explicit `-db` or `-bundle` source selection. See [agent investigations](agent-investigations.md) for trusted profiles, offline assertions, authentication and read-only boundaries.

## Interpret the response

`Series.observations` is absent unless requested. Its statistics use the metric's per-collection query before chart bucketing. `Series.min/max` keep their existing bucket-value/band-edge meanings; they are not redefined.

| Field | Meaning |
| --- | --- |
| `sampleCount` | Rows returned by that per-collection metric query, including NULL values |
| `knownCount` | Returned rows with a known value; not an expected collection count |
| `firstSampleAt`, `lastSampleAt` | Earliest/latest returned metric-row collection timestamps |
| `latest` | Value at the latest returned timestamp, absent if any tied row is NULL or values disagree |
| `mean` | Arithmetic mean of known values; not time-weighted |
| `minimum`, `maximum` | Known metric-value extrema before chart bucketing |
| `maximumAt` | Earliest returned collection timestamp at that maximum |
| `observedTotal` | Exact nonnegative decimal integer sum for summed event metrics only |
| `minimumLow`, `maximumHigh` | Raw per-collection band edges, separate from median statistics |

Some metric queries exclude unavailable comparisons or incomplete collections. These counts therefore cannot prove complete capture, count all expected collections or reconstruct missing observations. Timestamps identify collection starts, not exact physical-event times. Missing values remain absent; a captured zero is present. Empty summaries have zero counts and absent values/times under protobuf's normal JSON presence rules.

Server CPU is percent of one core and may exceed 100. Band means/peaks summarize the per-collection median; use the band edges and relevant server series for different questions. Server-name metrics retain their existing grouping across process lifetimes; this option does not identify a process or disambiguate simultaneous same-name processes. Rates retain their existing reset/gap rules. They are not integrated into traffic volumes, and no `observedTotal` is fabricated for rates or gauges. Slow-consumer totals remain observed counter increases, not unique connections or proven causes.

Stored curves and summaries share one read transaction, including the busiest-server lookup of a band. This is not a persistent pin and does not establish one snapshot across batch siblings or other RPCs. Both reads count against the existing query deadline. Captured sources use the same implementation under engine-enforced read-only access. The demo uses a fixed native sample grid, explicitly synthetic; it omits uncaptured sample intervals rather than inserting zeros.

Numeric gauge/rate statistics use the existing floating-point metric representation. Only `observedTotal` has the exact decimal-integer guarantee; use the [storage change API](storage-change.md) for exact integer storage comparison evidence. Changing chart resolution does not change the summary, but changing source data or the selected interval can.
