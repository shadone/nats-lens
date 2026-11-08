# Stream and consumer list pagination

`ListStreams` and `ListConsumers` return bounded pages for UI and agent clients. This contract applies to these two RPCs; dedicated bucket lists, detail RPCs, and other list APIs retain their own contracts.

## Page limits and compatibility

The existing `limit` field requests at most 100 rows. Zero uses 100, positive values above 100 use 100, and negative values return `InvalidArgument`. Older clients that relied on zero returning every row must traverse `next_page_token`. The response can contain fewer rows because both JSON and protobuf representations must stay below 512 KiB. A summary that cannot fit alone returns `ResourceExhausted`; no fields are removed to make it fit. The handler checks the budget again after adding readable account names. An oversized decorated page also returns `ResourceExhausted`; request a smaller limit.

`total_count` covers every match before the page limit. Stream totals cover every matching stream before the limit, including storage streams only when `include_kv` includes them. Totals on the default stream list therefore exclude hidden bucket consumers. Neither count nor totals describe only the loaded page.

The Overview requests 10 worst streams or 6 busiest consumers. The Streams and Consumers pages request one page at a time. Search and stream filter choices request `summary_only` to omit growth and sparklines, traverse pages sequentially, preserve the first response's absolute range, and pass cancellation to every read. They return complete results or fail, rather than presenting an incomplete traversal as complete. Search retains its existing behavior of showing successful entity kinds when another kind fails.

## Traversal context

A first page captures a process-local context with the ordered summaries, totals, collection number, normalized filters, system selector, and absolute range. Streams keep worst-status order followed by account/stream key. Consumers keep worst-status order followed by account/stream/consumer key, or delivery rate followed by that key when requested. Consumer ranking evaluates delivery-rate aggregates over all matches once.

Subsequent requests carry the continuation token and the same filters and system selector. The size can change. Omitted range bounds reuse the context's bounds; explicit different bounds return `InvalidArgument`. A different selector for the same system also counts as a changed selector. The response includes the pinned range and collection number.

The context freezes the summaries and order captured by the first read. It does not create a transactional database snapshot. The first capture reads summary facts, findings, and inventory separately. Readable account names are looked up for each response and can change between pages. Stream growth and seven-day sparklines are read from retained history only for the selected page; concurrent retention or repair can change that history. New collections do not change the retained order or totals. A collection removed before a continuation read returns `FailedPrecondition`.

Contexts live for 5 minutes, with at most 8 retained per system provider, shared between both RPCs. Starting more traversals evicts the oldest contexts. Restarting the process loses them. Expiry, eviction, or missing retained collection returns `FailedPrecondition`: discard the token and start again. The UI offers Refresh list; filter, range, account, and system changes reset to page 1. Continuation pages stop live polling; Refresh list requests a fresh first page even while the previous first page is fresh in the client cache. It starts a live traversal again. Navigation and repeat refresh are disabled while that request is pending.

A context holds at most 2 MiB of encoded summary data, and observed stream/consumer ranking reads stop after 20,000 rows with `ResourceExhausted`. Narrow the account or consumer stream filter when ranking exceeds its bounds. Delivery-rate ranking limits the SQL result to 20,001 rows and rejects the overflow row before adding it to its 20,000-entry map. The limit also applies when historical consumers no longer occur in the pinned collection. These limits bound retained contexts and application row materialization, not the database engine's work to aggregate matching facts. The existing handler deadline and query cancellation still apply.

## Query scope and remaining limits

A stream page selects keys before either history query. Unless `summary_only` is set, one query reads growth bucket medians, preserving the six-hour eligibility and bounded lookback policy; one reads seven-day sparkline buckets. Both queries receive the selected key array, with at most 100 keys, rather than one query per entity. A byte-constrained page can enrich candidates that fit the row limit but do not fit its byte limit. The continuation resumes at the first unreturned key.

`ListConsumers` returns bounded serialized pages and reuses retained delivery-rate rankings; it does not create per-consumer history series. Dedicated KV/object bucket lists still read growth for all listed buckets and have no pagination. `GetStream` still embeds consumers. Servers, subjects, placement, Capacity, and account list surfaces retain their existing individual bounds or gaps. This change does not establish a global bound for every API response.
