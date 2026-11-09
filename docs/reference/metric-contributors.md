# Global metric rankings

`ListMetricContributors` ranks all matching observed server names, streams or consumers in one selected interval, then returns a bounded page. It reuses the [native observation summary](metric-observations.md) formulas. The existing history tables still sort only their loaded rows; use **Rank matching server names/resources** for global ordering.

## Read a ranking

Save this request as `ranking.json`:

```json
{
  "metric": "server.cpu_percent",
  "range": {"start": "2026-10-17T13:30:00Z", "end": "2026-10-17T14:30:00Z"},
  "ranking": "METRIC_RANKING_MAXIMUM",
  "descending": true,
  "query": "nats",
  "limit": 12
}
```

```sh
natslens investigate query ListMetricContributors -profiles profiles.json \
  -profile shop -system example-prod -request ranking.json -page-size 12
```

The same method works with validated `-bundle` or qualified closed `-db` sources through the existing network-free handler. Local method discovery does not establish that an older server implements the RPC. See [agent access](agent-investigations.md) for trusted profiles, credentials, source selection and exact returned-context replay.

The response includes `contributors[].entity`, full native `observations`, `range`, `readAt`, `totalCount`, metric/unit/ranking/order, and an explicit `nextPageToken`. To continue, repeat the same metric, filters and order with that token. Omitted range bounds are taken from the token. Do not automatically fetch all pages; the CLI's page size applies to `limit`. Immutable datasets can rehydrate the traversal after reopening, with the same source fingerprint and summaries.

## Scope and ordering

- All eighteen current server/stream/consumer metric IDs are supported. Cluster aggregate metrics are rejected: the API does not invent entity attribution for an aggregate. A record must have retained reports inside the half-open interval to enter the candidate set. This is not configured inventory or proof of complete capture.
- `account` and `stream` restrict resource keys. `server` selects one reported server name. Account/stream filters are invalid for server measurements; server filters are invalid for resources. `query` is a literal case-insensitive entity-key substring, not SQL or a regular expression. The resource panel exposes it as **Filter ranking names** (`rankQuery` in the URL); server panels inherit the page’s server-name filter.
- `ranking` accepts `NAME` (the default), `LATEST`, `MEAN`, `MAXIMUM`, `MINIMUM`, `OBSERVED_TOTAL` or `COUNTER_INCREASE`, with the `METRIC_RANKING_` prefix. `descending` defaults to false. Event totals apply only to summed metrics; comparable counter increases apply only to supported traffic counters. Unsupported combinations fail.
- Unknown/nonfinite values sort last in either direction. Ties use ascending entity key; name ranking itself can be reversed. Exact decimal totals use integer comparison, including values above JavaScript's safe-integer and signed 64-bit ranges. A comparable zero is a known value; absent comparisons remain unknown.
- Mean is sample-weighted, not time-weighted. Peaks are pre-bucket metric observations, not extrema of chart averages. Incoming/outgoing traffic rank separately. Replica/logical storage, counter/rate and incarnation caveats remain those of the metric.
- Server rows also report distinct stored IDs observed in the interval and IDs with known recorded process-start timestamps inside it. Initial starts count; unknown start times do not. These fields do not reconstruct all lifetimes or separate simultaneous processes sharing one name.

## Capture and limits

Proposed, not yet confirmed: at most **20,000 observed identities**, **2 MiB** of serialized ranking summaries, **100 rows / 512 KiB** per response and **30 days** per interval. Zero limit means the bounded 100-row default; negatives are invalid. A narrow filter can recover after overflow. The server never silently turns overflow into a partial global ranking.

Identity selection and native summary reads share a read transaction. One identity query precedes one set-based native summary query; the API does not issue a series RPC per candidate or build chart histories for the full ranking. The SQL uses the same observation query/scanner, with trusted correlated entity identifiers and bound time/filter values. Database execution work can depend on retained data; row/byte limits are application/context/wire limits, not a hard CPU, query-memory, latency or aggregate in-flight memory guarantee. Cancellation/deadlines retain native cooperative limits.

The existing cache holds eight contexts per provider, shared with stream/consumer lists, for five minutes. It freezes order, values and range. Expiry, eviction, source restart or changed scope requires a first-page refresh. Tokens are untrusted traversal data, not credentials or persistent database pins. Immutable-source continuation re-runs the same bounded loader and validates its fingerprint/scope/summary digest.

Subsequent histories/findings/events use fixed interval links but can observe later backfills; they are not the ranking's database snapshot. `readAt` is the provider's source clock at capture, distinct from per-row collection times. The first live UI ranking page refreshes; continuation is fixed until **Refresh list**. Share/Back preserves ranking choices and requested bounds; browser cursor position is session-local. Explicit machine requests and returned exact contexts are supported; copied ranking URL controls are not automatically translated into a ranking query.

Demo rankings use synthetic scenario observations/lifecycles and never fabricate cumulative traffic evidence from rate curves. A ranking of a count does not prove causal contribution, missing capture is not zero, and no message/key/object content is read.
