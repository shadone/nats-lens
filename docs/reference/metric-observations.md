# Metric observation summaries

The API supports the [metric investigation pages](../features/metric-investigations.md) and the same remote/offline agent requests. See the [coverage inventory](../plans/metric-investigation-coverage.md) for supported families and remaining work.

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
| `counterComparisons` | Optional exact traffic-counter evidence for the eight server/cluster message/byte rate metrics below |

Some metric queries exclude unavailable comparisons or incomplete collections. These counts therefore cannot prove complete capture, count all expected collections or reconstruct missing observations. Timestamps identify collection starts, not exact physical-event times. Missing values remain absent; a captured zero is present. Empty summaries have zero counts and absent values/times under protobuf's normal JSON presence rules.

Server CPU is percent of one core and may exceed 100. Band means/peaks summarize the per-collection median; use the band edges and relevant server series for different questions. Server-name metrics retain their existing grouping across process lifetimes; this option does not identify a process or disambiguate simultaneous same-name processes. Rates retain their existing reset/gap rules. They are not integrated into traffic volumes, and no `observedTotal` is fabricated for rates or gauges. Slow-consumer totals remain observed counter increases, not unique connections or proven causes.

Stored curves and summaries share one read transaction, including the busiest-server lookup of a band. This is not a persistent pin and does not establish one snapshot across batch siblings or other RPCs. Both reads count against the existing query deadline. Captured sources use the same implementation under engine-enforced read-only access. The demo uses a fixed native sample grid, explicitly synthetic; it omits uncaptured sample intervals rather than inserting zeros.

Numeric gauge/rate statistics use the existing floating-point metric representation. `observedTotal` and `counterComparisons.observedIncrease` have the exact decimal-integer guarantee; use the [storage change API](storage-change.md) for exact integer storage comparison evidence. Changing chart resolution does not change the summary, but changing source data or the selected interval can.

## Comparable traffic increases

For `server.in_msgs_rate`, `server.out_msgs_rate`, `server.in_bytes_rate`, `server.out_bytes_rate` and their `cluster.*` equivalents, stored sources also return `counterComparisons`. Incoming and outgoing traffic remain separate. Change the example request's metric to one of these names; the same `includeObservations` option works through the read-only CLI and captured datasets.

| Field | Meaning |
| --- | --- |
| `observedIncrease` | Exact nonnegative decimal string summing admitted counter increases; absent when no pair qualifies, present as `"0"` for comparable zero |
| `comparisonCount` | Number of admitted consecutive report pairs, partitioned by stored server ID |
| `serverIdCount` | Distinct stored server IDs contributing admitted pairs; not a reconstructed process-lifetime count |
| `firstComparedAt`, `lastComparedAt` | Earliest preceding and latest current collection timestamps among admitted pairs |
| `excludedReports` | Returned current counter rows inside the range that did not contribute a pair |
| `boundaryReports` | Subset of excluded rows whose otherwise comparable pair begins before the range start |

Proposed, not yet confirmed: volume evidence admits only pairs wholly inside `[start,end)`, with nonnegative known counters, no decrease, positive elapsed time, and a gap no greater than three collection intervals (using at least one second per interval). The exact sum uses integer arithmetic before conversion to a decimal string, including sums above signed 64-bit range. A restart with a new stored server ID starts a new baseline. A counter reset, NULL or negative counter, backwards/equal collection timestamp, or excessive gap cannot contribute traffic.

A plotted rate may use a preceding baseline outside the selected range; that pair is deliberately excluded from the increase. Thus rate rows and volume pairs have different counts. Entirely absent reports cannot be counted as excluded rows; a short missing-report gap can bridge within the elapsed-time rule. The timestamp envelope does not establish uninterrupted capture. These are observed increases from admitted pairs, not complete interval volumes, client attribution, or a sum of sampled rates.

Predecessor selection preserves every intervening report within a contiguous epoch envelope before applying timestamp membership. An out-of-range clock-anomalous report cannot disappear and turn nonconsecutive reports into a pair. The envelope is selected from the existing lookback/range epoch bounds; it does not require a full retained-history window.

Ordinary and opted-in server rates share the same counter-validity predicate. In particular, decreasing counters plus backwards timestamps cannot fabricate a positive rate. Resource ingestion/delivery sequence metrics do not yet expose this counter summary. The demo omits it because its synthetic rate curves have no stored cumulative counters; consumers must show unavailable evidence rather than integrate those curves.

The curve and counter summary retain the same read transaction and two metric reads as other stored observation requests. The response materializes aggregate scalars, not raw report maps. These limits do not bound DuckDB's internal work or pin separate RPCs.

## Resource sequence rates

`stream.msgs_rate` measures reported last-sequence advance per second, not stored or accepted message volume. NATS can advance stream sequences without storing messages (pinned [NATS 2.15.0 implementation](https://github.com/nats-io/nats-server/blob/v2.15.0/server/memstore.go#L433-L485)). `consumer.delivered_rate` measures delivery-sequence advance; redeliveries can advance it, so it does not count unique messages or acknowledgements. Neither exposes an exact traffic-increase total.

Proposed, not yet confirmed: adjacent resource observations must have a known creation timestamp that agrees across responding rows. Sequences must be nonnegative and nondecreasing; elapsed time must be positive and at most three collection intervals, with a one-second minimum interval. Consumer observations additionally need exactly one leader and the same leading server on both sides. An observed identity change, unknown/conflicting creation, missing/multiple consumer leaders, reset or invalid timestamp breaks comparison. A return to an earlier identity cannot skip the intervening observation. Predecessors are chosen in epoch order before timestamp membership, so clock-anomalous intervening reports cannot be filtered away to fabricate a pair.

The consumer list/Overview summary averages sequence advance in the newest comparable run within fifteen minutes before the range end. It needs at least two reports in that run; older runs are not substituted. Histories, observation summaries and global rankings reuse the same comparable-pair definition. Completely absent resource rows can bridge a short elapsed gap. Creation metadata and these sample boundaries do not certify continuous capture or a physical incarnation. Existing entity keys and metric IDs remain unchanged.
