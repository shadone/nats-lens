# Performance baseline

This page records how the API, the store and the background jobs behave on synthetic stores of a large system, measured on 2026-11-11 (branch `perf/measure`). The `history` runs used the harness of revision `07ec253b`; the `entities` load ran on an earlier working state of the same harness, which lacked the column of failed calls' latency; the queries and scenarios were the same. It gives the numbers that admission limits, indexes or caching must improve on, and the ranked changes the numbers point to. Every recommendation and budget here is **proposed, not yet confirmed**.

## Rerun it

```sh
make perf
```

`make perf` runs `scripts/perf.sh`. For each profile it builds `natslens-perf`, generates a store, records the heaviest queries under `EXPLAIN ANALYZE`, runs the load scenarios, writes the results to `.perf/<UTC time>/` and deletes the store. It takes about 30 minutes and needs 7 GiB of free disk; the script refuses to start with less. `PERF_PROFILES=smoke scripts/perf.sh` checks the harness in two minutes. The script's header lists the other settings (`PERF_DURATION`, `PERF_SCENARIOS`, `PERF_KEEP`).

The steps on their own:

```sh
go build -o natslens-perf ./cmd/natslens-perf
./natslens-perf generate -profile entities -out /path/entities.duckdb
./natslens-perf explain  -profile entities -db /path/entities.duckdb -out /path/explain
./natslens-perf load     -profile entities -db /path/entities.duckdb -duration 60s -json /path/load.json [-cpuprofile /path/cpu.out] [-query-budget N] [-threads N] [-memory-limit 4GiB] [-max-temp-size 2GiB] [-interval 30s]
```

`load` changes the store: the scenarios that write add epochs, findings and archive records. Generate a new store to repeat a run exactly. `-interval` sets how often `writes-only` and `mixed` write an epoch, and each write's limit, as the collector's interval sets both for `natslens run`. Its default is 10 s, a third of the collector's default of 30 s; every result on this page used it unless it says otherwise. Nothing here runs in the test tiers except `TestSmokeProfileRuns`, which checks that the smoke profile generates, explains and loads without an error, and `TestIntervalFlagReachesTheWriter`, which checks that `-interval` sets the writer's interval. Both skip in `-short` mode.

## Environment

| Item | Value |
| --- | --- |
| Machine | Apple M2 Pro, 12 cores, 32 GB memory, macOS 15 (Darwin 24.6), APFS on the internal SSD |
| Software | Go 1.27.3, DuckDB 2.0.0 (`duckdb-go` v2.20000.0), DuckDB `threads` 12, `memory_limit` 25.5 GiB (DuckDB's default, 80% of memory) |
| Background load | A desktop session: load average about 5 with nothing of ours running (`sysmond`, the window server, a browser). Each scenario records the load average before and after it in the raw results; the heavier scenarios themselves raise it to 15–58 |
| Process | The harness serves the store and runs the clients in one process, over loopback HTTP with the Connect client; the server is `api.NewMux` over `storeprov.Provider`, as `natslens run` builds it, with one system |

The machine was not otherwise idle, and each scenario ran once. Treat differences under about 30% as noise; several scenarios show a factor of 10 or more.

## Synthetic stores

`natslens-perf generate` writes the first two epochs through `Store.WriteSnapshot`, the collector's own path, from synthetic VARZ, CONNZ, ROUTEZ, ACCSTATZ, JSZ and HEALTHZ replies. It copies the second epoch's rows to every later epoch in SQL, in epoch order, with traffic counters and sequences grown in proportion to the epoch and activity times moved with it. It then adds events over the whole history and runs the built-in checks on the newest epoch. The newest epoch always starts at 2026-10-17 14:00 UTC, so runs compare. Stream `s` lives on servers `s` to `s+2` and is led by the first; its consumers are placed the same way.

| Profile | Servers | Streams (R3) | Consumers (R3) | Connections | History | Epochs | Rows | Size | Generated in |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `entities` | 5 | 2,000 | 10,000 | 50,000 (20,480 listed: 4,096 per server, the default `connz_limit`) | 24 h | 2,880 | 370 million | 4.59 GB | 73 s |
| `history` | 5 | 200 | 1,000 | 5,000 | 7 days | 20,160 | 321 million | 4.78 GB | 128 s |

`entities` has 130,000 rows per epoch: 6,000 stream reports, 12,000 stream replica peers, 30,000 consumer reports, 60,000 consumer replica peers and 20,480 connections. One epoch takes 1.6 MB of the database file, so a week of such a system takes 31 GB and the default 14-day retention 63 GB. The two profiles stand in for that week on one disk: `entities` has the full asset count over the UI's default 24-hour range, `history` a tenth of it over seven days. Writing one `entities` epoch through `WriteSnapshot` into a nearly empty store took 1.20 to 1.22 s.

The generator leaves out RAFTZ, SUBSZ, leaf nodes, gateways, micro services, Kubernetes and account lookups, so pages built on those read empty tables.

## Scenarios

| Scenario | What runs |
| --- | --- |
| `cold-overview` | The Overview's calls once, all at once, first thing after the store opened: DuckDB's buffers are empty. The OS file cache is not dropped (that needs `sudo purge`), so this is cold for DuckDB only |
| `warm-overview` | The Overview's calls at once, ten times in a row, 24-hour range |
| `overview-7d` | The same, five times, 7-day range |
| `tabs-1`, `tabs-4`, `tabs-16` | 1, 4 or 16 clients, each loading the Overview and then nine other pages in turn (streams, consumers, servers, connections, subjects, timeline with events, findings, capacity, topology), for 60 s |
| `agents-1`, `agents-4`, `agents-8` | 1, 4 or 8 agents, each alternating a 32-series `GetTimeSeriesBatch` (`stream.msgs_rate` per stream, 24 h) and `ListMetricContributors` (`stream.msgs_rate`, mean, top 50), for 60 s |
| `batch-vs-serial` | One client: the 32-series batch, then the same 32 series as `GetTimeSeries` calls one after another, five rounds |
| `writes-only` | Epochs written every 10 s through `WriteSnapshotWithin` with a 10 s limit, each followed by the checks, and the archive exporting two hours at a time, with no API calls, for 60 s |
| `mixed` | `tabs-4` and two agents while epochs are written and the archive exports as in `writes-only`, for 60 s |

The Overview's calls are those `web/src/pages/Overview.tsx` and the app shell start on load: `GetLensInfo`, `ListSystems`, `GetOverview`, `ListFindings`, `GetStatusTimeline` (with the subject lanes and capture gaps), `GetDataHistory`, the eleven-series trend `GetTimeSeriesBatch`, `ListConsumers` by delivered rate and `ListStreams`. The subjects page's `GetSubjects` carries the delivered rates. A page's time is from its first call to its last answer. The writer runs three times as often as the default 30 s interval, so its limit is a third of production's.

Per procedure, the client measures each call; the server's histograms give the queries per call and their summed seconds; the lock, job and query histograms give the server side of the background work. The raw tables, with every procedure and the load average of each scenario, are the `load` command's Markdown output; the summary below keeps the rows that matter.

## Results

### Overview page load

| Store | Scenario | Page p50 | Page p95 | Calls that failed |
| --- | --- | --- | --- | --- |
| `entities` | `cold-overview` | 48.7 s (one load) | | `GetOverview` and the trend batch, `deadline_exceeded` |
| `entities` | `warm-overview` | 12.1 s | 53.0 s | 5 of 10 `GetOverview`, 5 of 10 trend batches, 1 of 10 `ListConsumers` |
| `entities` | `overview-7d` | 8.9 s | 49.8 s | 2 of 5 `GetOverview`, 2 of 5 trend batches |
| `history` | `cold-overview` | 0.82 s (one load) | | none |
| `history` | `warm-overview` | 0.83 s | 0.84 s | none |
| `history` | `overview-7d` | 4.3 s | 4.5 s | none |

At `entities` size the Overview does not load within its limits: `GetOverview` has 30 s (`api.QueryTimeout`) and the batch 40 s (`api.BatchTimeout`), and both run out. In the cold load `ListConsumers` took 22.6 s and `ListStreams` 6.1 s. At `history` size a 24-hour Overview takes under a second and a 7-day one 4.3 s, led by `GetOverview` (38 queries) and the trend batch.

### Concurrent pages

| Store | Clients | Overview p50 / p95 | Slowest other page p95 | Failed calls |
| --- | --- | --- | --- | --- |
| `entities` | 1 | 53.5 s (one load, timed out) | capacity 2.6 s | `GetOverview`, batch |
| `entities` | 4 | 53.3 s (one load) | capacity 30.3 s, consumers 30.0 s, streams 19.5 s | 5 |
| `entities` | 16 | 30.8 s / 31.5 s | capacity 35.9 s (11 of 11 failed), consumers 30.1 s | 18 |
| `history` | 1 | 0.78 s / 0.84 s | capacity 0.35 s | none |
| `history` | 4 | 2.2 s / 43.3 s | streams 45.6 s, capacity 14.5 s | 2 |
| `history` | 16 | 7.6 s / 9.2 s | capacity 3.8 s | none |

Pages that read only the newest epoch or one bounded list (findings, connections, servers, events, lens info) stayed under 1.5 s at p95 in these runs, mostly under 300 ms; with epoch writes beside them (`mixed`, below) the connections page reached 9.7 s at p95. The pages that slow down read every epoch of the range for every asset: the Overview's tiles and trends, capacity, the consumers' delivered rates and the streams' sparklines. With `history` the 4-client run had a worse p95 than the 16-client one; one run each cannot say whether that is noise.

### Agent traffic

| Store | Agents | Batch p50 / p95 | Contributors p50 / p95 | Calls per second, both kinds | Failed |
| --- | --- | --- | --- | --- | --- |
| `entities` | 1 | 0.36 s / 0.41 s | 2.4 s / 2.7 s | 0.68 | none |
| `entities` | 4 | 1.2 s / 10.5 s | all 4 timed out at 30 s | 0.13 | 4 contributors |
| `entities` | 8 | 4.0 s / 6.9 s | 22.4 s / 29.8 s | 0.40 | 1 contributors |
| `history` | 1 | 0.19 s / 0.20 s | 0.29 s / 0.31 s | 4.2 | none |
| `history` | 4 | 0.35 s / 0.48 s | 0.75 s / 1.0 s | 7.2 | none |
| `history` | 8 | 0.48 s / 0.68 s | 1.7 s / 2.0 s | 7.4 | 1 batch |

A 32-series batch runs 64 or 65 queries: each series also checks that its stream exists. Throughput stops growing at about four agents: from four to eight agents the `history` store's calls per second rose by 3% while the contributors' p50 more than doubled (0.75 s to 1.7 s) and the batch's rose by 40% (0.35 s to 0.48 s).

### Per-request parallelism

`GetTimeSeriesBatch` reads four series at a time (`batchParallelism`). One client, five rounds, the same 32 series:

| Store | Batch p50 | Same series one call at a time, p50 |
| --- | --- | --- |
| `entities` | 0.35 s | 0.88 s |
| `history` | 0.20 s | 0.65 s |

On an idle store the parallelism makes a batch 2.5 to 3.3 times faster. Under concurrency it only adds to the queue: with four agents the `entities` batch's p50 was 1.2 s and the batch's queries ran 18 s summed per call, against 17 s with one agent. The parallelism within a request pays while the store is idle; across requests nothing bounds the total, so it should come out of a shared budget rather than be added to it (see the follow-up below).

### Epoch writes, checks and archive

| Store | Scenario | Epochs on time | Write p50 / max | Checks p50 / max | Tick to checks done, max | Lock wait | Archive |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `entities` | `writes-only` | 6 of 6 | 1.28 s / 1.52 s | 5.1 s / 5.3 s | 6.7 s | under 1 ms, except `archive_record` up to 500 ms | 24 hours, 1.1 s mean per hour |
| `entities` | `mixed` | 1 of 7 | 8.5 s / 14.1 s | 5.4 s / 6.6 s | 17.0 s | under 1 ms | none exported in 60 s |
| `history` | `writes-only` | 7 of 7 | 0.40 s / 0.47 s | 3.1 s / 3.3 s | 3.7 s | under 1 ms at p95; `archive_record` 9 ms mean | 126 hours, 0.47 s mean per hour |
| `history` | `mixed` | 1 of 6 | 10.05 s / 10.6 s | 5.1 s / 11.0 s | 22.7 s | `epoch` 7 ms mean, p95 in the 50 ms bucket; `archive_record` 1.08 s mean, p95 in the 10 s bucket | 8 hours, one tick 43 s |

Under API load three of seven `entities` epochs and three of six `history` epochs were not stored: their writes ran out of the 10 s limit (`context deadline exceeded` in `INSERT INTO consumer_replicas`, `UPDATE consumer_config`, the stream configuration staging). The write lock is not where the epochs wait: `nats_lens_store_lock_wait_seconds{op="epoch"}` stayed under 50 ms while the hold grew from 1.3 s to 8.5 s. The epoch write competes with the API's queries for DuckDB's twelve threads. The lock does delay the archive: in the `history` `mixed` run, recording an exported hour (`op="archive_record"`) waited 1.08 s on average, with its p95 in the 10 s bucket, behind the slowed epoch writes. At production's 30 s interval the limit is three times longer, but the `entities` write's maximum of 14 s already exceeds a third of it.

### Memory

The process's peak resident memory was 19.1 GB for the `entities` run and 15.3 GB for the `history` run; the Go heap never exceeded 412 MB. The rest is DuckDB's buffer pool and the hash tables of concurrent aggregations, bounded only by the default `memory_limit` of 25.5 GiB. The `entities` generator itself peaked at 7.8 GB.

### Collection freshness

Freshness is the time from an epoch's tick until its checks finish: 6.7 s (`entities`) and 3.7 s (`history`) without API load, 17.0 s and 22.7 s with it. `nats_lens_last_epoch_age_seconds` already reports the age of the newest epoch; `nats_lens_job_duration_seconds{job="checks"}` and the lock hold of `op="epoch"` now show where the time goes.

### Where the time goes

A Go CPU profile of the whole `entities` run attributes 99% of the samples to `runtime.cgocall` and native code: the Go side (Connect, protobuf, scanning rows) is under 1%. The cost is in DuckDB, so the plans below come from DuckDB's own profiler.

## Top costs

`natslens-perf explain` recorded every query of three warm Overviews, each other page once and two agent rounds on the `entities` store (24-hour range), then replayed the heaviest alone under `EXPLAIN ANALYZE`. Operator times are CPU time summed over DuckDB's threads.

| # | Procedure and query | Slowest in the page load | Alone | What the plan spends it on |
| --- | --- | --- | --- | --- |
| 1 | `GetTimeSeriesBatch`, `cluster.assets_replicated` and `cluster.assets_single` (the Overview's asset trend), `assetCount` in `storeprov/series.go` | 30.3 s | 2.0 s | Joins all 86.4 million consumer reports of the day to `consumer_config` on a range condition (4.3 s), groups them into 28.8 million (epoch, consumer) groups (8.5 s), then joins the stream groups (5.1 s) |
| 2 | `GetOverview`, `cluster.streams`, `cluster.kv_buckets` and `cluster.object_stores` tiles, `streamCount` | 48.3 s | 0.33 s each | `count(DISTINCT (account, stream))` per epoch over 17.3 million stream reports: a 5.8-million-group hash aggregation (2.6 s), three times, once per tile |
| 3 | `GetOverview`, `cluster.durable_consumers` tile | 2.1 s | 1.65 s | `count(DISTINCT ...)` per epoch over 86.4 million consumer reports joined to `consumer_config`: 28.8 million groups (10.6 s) |
| 4 | `ListMetricContributors`, `stream.msgs_rate` | 2.2 s alone; timed out with 4 agents | 2.3 s | A window over all 5.76 million (stream, epoch) rows (6.6 s), then a 5.8-million-group aggregation (3.7 s), to rank 2,000 streams |
| 5 | `ListConsumers`, `delivered_rate_summary` | 7.2 s | not replayed | Delivered rates of 10,000 consumers from every epoch of the range |

`GetOverview` computes its ten tile series one after another (`storeprov/tiles.go`), so items 2 and 3 add up within one call before its 30 s limit. Items 1 to 3 run once per Overview load and again on every refresh of a live range. Under the Overview's own burst of nine calls (the trend batch alone reads four series at a time) the same queries take 15 to 150 times longer than alone, which is how a 0.33 s query reaches 48 s.

On the `history` store (200 streams, 24-hour range) the same queries rank first, at a tenth of the cost: the asset trend 0.52 s, the stream-count tiles 0.21 to 0.25 s.

## Findings

1. **The Overview's counts scan every report of the range.** The tile and trend series that count streams, KV buckets, object stores, durable consumers and replicated or single assets aggregate every stream and consumer report of every epoch in the range, to produce one number per epoch. Their cost grows with assets times epochs; at 2,000 streams and 10,000 consumers over 24 hours they exceed the API's limits, so the Overview never loads.
2. **API reads starve epoch writes.** The write lock serialises writes only; reads run beside them, so an epoch write waits for DuckDB's threads, not for the lock. (The archive's records do wait for the lock, behind those slowed epoch writes.) Under four tab clients and two agents, epoch writes took six times longer and half failed at a 10 s limit. A failed epoch write is a lost collection.
3. **Nothing bounds the API's total work.** Each request runs its queries as soon as it arrives; the batch adds four at a time; `database/sql` opens a DuckDB connection for each concurrent query (no `SetMaxOpenConns`). The work shows up as latency and memory: 19 GB peak resident with 16 clients.
4. **Per-request parallelism pays only when idle.** The batch's four-at-a-time is 2.5 to 3.3 times faster alone and saturates at about four concurrent agents.
5. **Timeouts are the only limit, and they are reached.** At `entities` size every page that reads all epochs of the range for every asset reached its 30 s or 40 s limit at least once.
6. **Check evaluation takes 3 to 5 s per epoch** without API load and up to 11 s with it, within the default budget of half an interval (15 s) but not far from it.
7. **The store grows by 1.6 MB per epoch** at `entities` size: 63 GB for the default 14-day retention.

No bug in the sense of a wrong answer was found. The instrumentation shows two defects of scale (findings 1 and 2) that need more than a small fix; they are recommendations below.

## Recommendations

Ranked by measured benefit. Each is **proposed, not yet confirmed**.

1. **Store per-epoch asset counts when the epoch is written, and read the Overview's counts from them.** Implemented; see [After: asset counts per epoch](#after-asset-counts-per-epoch). A new table (one row per epoch: streams, KV buckets, object stores, durable consumers, replicated and single assets) written in the epoch's transaction, and a backfill for stored epochs, by a new migration. `cluster.streams`, `kv_buckets`, `object_stores`, `durable_consumers`, `assets_replicated` and `assets_single` then read 2,880 rows for a day instead of 17 to 86 million. Evidence: these are the top three costs (2.0 s, 3 × 0.33 s and 1.65 s alone at `entities`; 30 to 48 s in the page load) and the reason `GetOverview` and the trend batch time out. The remaining Overview calls took under 1 s at p50 in the cold load except `ListConsumers` and `ListStreams`. The write adds a few aggregates over rows already in the transaction.
2. **Bound the API's concurrent DuckDB work, and keep a share for epoch writes and checks.** See the follow-up below. Evidence: finding 2 (epoch write hold 1.3 s alone, 8.5 s p50 and 14 s max under load, 3 of 7 lost) and the throughput plateau at about four agents.
3. **Do the same for the per-stream and per-consumer rates over a range.** `ListMetricContributors` (`stream.msgs_rate`), `ListConsumers`' delivered rates, the capacity page's stream points and the streams' sparklines aggregate every epoch of the range for every asset: 2.3 s alone and timeouts under load at `entities`. Options to measure: hourly rollups of per-asset counters, as `client_app_hours` and `micro_endpoint_hours` already do, or reading only the epochs at the range's bucket edges for counters, whose rate needs only the end points.
4. **Set DuckDB's `memory_limit` explicitly** below the container's limit, with room for the Go heap. Evidence: 19 GB peak resident on a 32 GB machine with DuckDB's default of 80% of memory; in a container whose limit is below the host's memory, the default follows the host and invites an out-of-memory kill.
5. **Drop the per-series stream existence check, or do it once per batch.** A 32-series batch runs 64 queries where 32 compute series. The check takes under 1 ms on the smoke store and is not among the 25 costliest queries at either baseline size, so the benefit is fewer queries per call, not time.
6. **Profile check evaluation per check.** `check_runs.duration_ms` already holds each check's time; ranking it at `entities` size would show whether a few checks take most of the 5 s.

### Budgets

Proposed from these numbers, to be tracked with the new histograms, not with wall-clock unit tests:

| Measure | Budget | Now at `entities` | Now at `history` |
| --- | --- | --- | --- |
| Overview page load, 24 h, warm, one client (`nats_lens_api_request_duration_seconds`, slowest of the page's calls) | 2 s | times out | 0.84 s |
| API calls ending in `deadline_exceeded` with four tab clients | none | 5 in 60 s | 2 in 60 s |
| Epoch write under API load: the hold's p95 (`op="epoch"`) | a quarter of the interval (7.5 s at 30 s) | 30 s bucket; slowest write 14.1 s | 30 s bucket; slowest write 10.6 s |
| Epochs lost to the write limit | none | 3 of 7 under load | 3 of 6 under load |
| Write lock wait, p95, any op | 100 ms | 500 ms bucket (`archive_record`, no API load) | 10 s bucket (`archive_record` under API load); `epoch` 50 ms bucket |
| Check evaluation, `job="check_evaluation"`: mean, and p95 bucket, without and with API load | half the interval (15 s at 30 s) | 5.1 s and 5.2 s mean; 10 s bucket both | 3.0 s and 6.3 s mean; 5 s and 30 s buckets |

## After: asset counts per epoch

Recommendation 1 is implemented (branch `perf/asset-counts`, migration 0041): each epoch's write stores the six counts in `epoch_asset_counts`, and the series read one row per epoch. [Design](../design.md) describes the table, the fill when the collector opens the store, and the rebuild. Measured on 2026-11-11 on the same machine, which was shared with other work: the load average before each run is given, and it was well above the baseline's 5. One `entities` store was generated with the code before the change (revision `a75af7f2`), measured, then opened by the new code, which migrated it and filled its counts, and measured again. The scenarios ran once each. They predate the per-store query budget (recommendation 2), so every run here had no budget.

| Measure | Before | After |
| --- | --- | --- |
| `cold-overview`, page | 10.2 s (load 8.3) | 0.45 s (load 19.2) |
| `cold-overview`, `GetOverview` | 10.2 s, 38 queries | 0.33 s, 42 queries |
| `cold-overview`, trend `GetTimeSeriesBatch` | 8.9 s, 16 queries | 0.06 s, 18 queries |
| `warm-overview`, page p50 / p95 | 9.0 s / 69.1 s (load 10.4) | 0.38 s / 0.41 s (load 19.2) |
| `warm-overview`, `GetOverview` p50 / p95, failed | 8.4 s / 10.4 s, 3 of 10 `deadline_exceeded` | 0.28 s / 0.35 s, none |
| `warm-overview`, trend batch p50 / p95, failed | 6.5 s / 8.7 s, 3 of 10 `deadline_exceeded` | 0.05 s / 0.06 s, none |
| `warm-overview`, `ListConsumers` p50, failed | 1.2 s, 2 of 10 | 0.38 s, none |
| Peak resident memory, `warm-overview` | 14.0 GB | 1.3 GB |
| `writes-only`, epoch write p50 / max | 1.29 s / 1.96 s, 6 epochs (load 11.8) | 1.21 s / 1.55 s, 7 epochs (load 20.1) |

A second run of the old code at a load average of 19 timed out in its cold load (49.5 s) and failed with internal errors in its warm one while the disk filled; it is left out of the table.

- **The new reads.** `EXPLAIN ANALYZE` of the 24-hour `cluster.assets_replicated` series over the counts: 13 ms, reading 2,882 count rows and 2,880 epochs with the complete-JSZ subquery over `epoch_endpoints`; `cluster.durable_consumers` 28 ms. The check for epochs without counts that each count series runs first took 1.4 ms. The asset trend took 2.0 s alone before. `natslens-perf explain` no longer lists any of the six series among the nine costliest queries; `GetOverview`'s costliest is now `cluster.accounts` at 0.27 s summed over the recorded calls.
- **The write.** Computing one epoch's counts took 16 to 20 ms (ten epochs, load 12 to 13), about 1.3% of the 1.2 s epoch write; the `writes-only` write times above do not show it apart from noise.
- **The fill.** Filling the migrated store took 2.9 s for 2,886 epochs (measured when the fill still ran inside the open; it now runs after the store's limits are set); filling all 2,893 again took 3.0 s and 4.1 s. That is about 1 ms per epoch at this size, so a 14-day store of this size fills in about 40 to 60 s once, at its first start after the upgrade. Each statement covers 120 epochs.
- **Queries per call.** `GetOverview` runs four more queries and the trend batch two more: the check for missing counts per count series.

What still costs at `entities` size after this change: `ListConsumers`' delivered rates and `ListStreams` (recommendation 3), and the concurrency limits of recommendation 2. The Overview's 24-hour warm load is now under the 2 s budget above.

## For the follow-up: bound aggregate API work and verify cancellation

The follow-up task "Bound aggregate API work and verify cancellation under load" owns admission and queue limits. From these numbers it should:

- Put one budget of concurrent DuckDB queries on each store, shared by every API request and by `/metrics`, with the epoch write and the checks outside it or ahead of it. The plateau at about four concurrent agents on 12 threads suggests starting near four API queries per store and measuring up and down from there with `make perf`'s `mixed` scenario. With several systems in one process, `SetThreads` already divides the threads; the budget should follow the same share.
- Draw the batch's four-at-a-time from that budget, so one batch runs four series when the store is idle and one when it is busy, instead of adding four to the load.
- Queue a request that finds the budget used, bounded by its own deadline, and answer `resource_exhausted` or `unavailable` once the queue is full, rather than letting all of them run slower. Count queued requests in a gauge beside `nats_lens_api_requests_in_flight`.
- Bound the connection pool (`SetMaxOpenConns`) to the budget plus the writers, so the limit holds even for code that skips the admission.
- Verify cancellation under load: when a client goes away or a deadline passes, the query must stop in DuckDB, not only in Go. With the new metrics: after a burst of cancelled calls, `nats_lens_api_requests_in_flight` returns to 0 and `nats_lens_db_query_duration_seconds{source="api"}` stops counting within a second. In these runs a batch that timed out at 40 s had run 60 s of queries summed over its four workers, which is consistent with stopping at the deadline but does not prove it.
- Keep collection progress: the acceptance check is `mixed` at `entities` size with every epoch stored and the write hold's p95 within a quarter of the interval, and the fatal-error path unchanged (a fatal DuckDB error still stops only that system).

## After the query budget

Measured on 2026-11-11 on the same machine with the branch `perf/query-budget`: `natslens-perf load -profile entities -scenarios mixed -duration 60s` on APFS clones of one generated `entities` store, each clone deleted after its run. The writer runs every 10 s with a 10 s limit, as in the baseline (production's default is 30 s), so the target is a write hold p95 within 2.5 s and no lost epoch. "Before" is the same binary with the budget off (these runs passed `-query-budget 0`, which then meant off; the flag now follows `query_budget`, so off is `-query-budget -1`), which leaves the API unbounded and the pool at two idle connections, as on `develop`. `load` takes `-query-budget` as `query_budget` does: `-1` off, `0` (the default) derived from the threads as natslens derives it, or a number of queries; and `-threads` and `-memory-limit` to match a deployment. Another agent ran its own `natslens-perf` load on the machine during the first runs; the runs below waited until no other `natslens-perf` process ran and checked again halfway and at the end. The load average before each run is in the table; the machine was never below 8.6, and each setting ran one to three times, so treat differences under a factor of two as noise. With about six epochs a run, the p95 of a run is its slowest write.

| Setting | Runs | Load before | Epochs lost | Write p50 | Write p95 | API queries run | Most connections open | Overload answers |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| No API load (`writes-only`) | 1 | 14.3 | 0 of 7 | 1.28 s | 1.42 s | 0 | 2 | 0 |
| Before: no budget | 3 | 10.6, 8.6, 12.1 | 7 of 19 (3, 3, 1) | 10.2, 5.7, 5.1 s | 16.3, 17.0, 18.8 s | 871, 893, 1,019 | 28, 31, 33 | 0 |
| Default: budget 4, one slot during the epoch write | 3 | 10.1, 15.9, 10.2 | 8 of 18 (3, 3, 2) | 4.0, 2.7, 10.2 s | 13.0, 24.2, 12.4 s | 681, 371, 624 | 24, 17, 20 | 6, 5, 9 |
| Budget 2, one slot during the write | 1 | 17.1 | 3 of 6 | 2.0 s | 12.7 s | 470 | 19 | 7 |
| Budget 1 | 1 | 11.0 | 3 of 5 | 10.5 s | 17.8 s | 399 | 19 | 10 |
| DuckDB `threads` 8, budget 2 | 1 | 13.2 | 4 of 7 | 10.1 s | 19.2 s | 599 | 23 | 19 |
| DuckDB `threads` 6, budget 2 | 1 | 9.9 | 4 of 8 | 1.9 s | 11.0 s | 785 | 21 | 12 |
| DuckDB `threads` 6, budget 1 | 1 | 10.4 | 3 of 6 | 1.6 s | 10.3 s | 762 | 18 | 13 |
| Default with `memory_limit` 4 GiB | 1 | 11.8 | 5 of 6 | 2.5 s | 10.3 s | 498 | 24 | 8 |

"Overload answers" counts the queries whose call's deadline passed in the queue (`nats_lens_db_admission_wait_seconds_count{outcome="deadline"}`); no queue filled. With the budget, at most 4 queries ran and at most 16 waited; the pool never made a query wait for a connection.

**The acceptance target is not met.** No setting stored every epoch: the default lost 8 of 18, against 7 of 19 without a budget, and its slowest write per run stayed between 12 and 24 s. The budget does help the median write: with the epoch write's yield to one slot the p50 fell from 5 to 10 s to 2 to 4 s in five of the seven runs where the yield narrowed a budget above one (10 s in the other two), but a single heavy API query still holds the engine through some writes. Budget 1, which serialises every read, and fewer DuckDB threads lost as many epochs, so neither the number of concurrent queries nor the thread count is what loses them. Two causes remain:

- **The heaviest reads.** The Overview's asset counts and the per-asset rates over a day (the baseline's top costs) take 2 s alone on 12 threads and keep the engine busy for seconds whatever else runs. Recommendations 1 and 3 above remove them; they, not admission, are what can bring the write back to its 1.3 s.
- **A constraint error under concurrent reads.** Five epochs in the budget runs (and one in an earlier run) failed at once, not at the deadline, with `Constraint Error: Duplicate key "server_id: ..." violates primary key constraint` in the epoch write's `INSERT INTO servers ... ON CONFLICT (server_id) DO UPDATE`. It appeared only with API reads running beside the write, and most often with a 4 GiB memory limit (three of six epochs). This matches DuckDB's handling of a primary key whose old row versions a concurrent read transaction still sees. It is a separate defect of the epoch write, not of admission, and needs its own task.

**Memory.** The process peaked at 14.0 GB with no API load at all, and at 14.5 to 18.6 GB in the mixed runs, with or without a budget: DuckDB's buffer pool, not the API's concurrency, sets the peak. A `memory_limit` of 4 GiB held the peak to 6.8 GB. `natslens run` already sets `memory_limit` (`duckdb_memory_limit`, by default 60% of the container's limit, 1 GiB outside one); only `natslens-perf` ran with DuckDB's default of 25.5 GiB, and now takes `-memory-limit` to match a deployment.

**API.** With the budget the API ran fewer queries in the same minute (371 to 681 against 871 to 1,019) and answered 5 to 9 calls `resource_exhausted` in a run instead of letting them run on into `deadline_exceeded`; failed calls per run stayed at 6 to 7 either way. Connections open fell from about 30 to about 20.

**Cancellation.** `TestCancelReachesDuckDB` (in `internal/store`) cancels two queries running in DuckDB, sixteen queued for the budget (its queue's bound at a budget of 2) and twenty mutes waiting behind the write lock. All 38 ended within milliseconds of the cancel; no slot, waiter or goroutine was left; only the two interrupted queries were recorded; and the process's CPU fell from 3 s per second to none in the following second, so the engine stopped running them. Its control, `TestEndlessQueryRunsLong`, shows the same query still running after 6 s without a cancel.

**Settings, proposed, not yet confirmed.** The default stays one slot per three threads (4 on 12) with the yield to one slot during the epoch write: the smaller budgets lost as many epochs and ran fewer API queries. The pool keeps the budget plus 8 connections idle and has no `SetMaxOpenConns`, because the epoch write shares the pool and would queue behind API reads for a connection. `threads` stays at the per-system share: fewer threads did not keep epochs.

**Measure again.** The yield's limit of one slot during the epoch write (`admit.YieldLimit`) is the first setting to measure again, once the per-epoch asset counts replace the Overview's heaviest reads and the duplicate-key error in the servers upsert is fixed. With those two causes gone, the runs above cannot say whether one slot, more, or none keeps the write within its quarter of the interval.

## Acceptance after asset counts, query budget and the servers change

Measured on 2026-11-12 on the same machine with `develop` at `c5c8892a`, which has the per-epoch asset counts (migration 0041), the query budget with its yield to one slot during the epoch write, and the servers upsert that updates the row in place. One `entities` store was generated by this revision (2,880 epochs, 4.59 GB, 76 s), and each run used an APFS clone of it, deleted after the run:

```sh
natslens-perf load -profile entities -db <clone> -scenarios mixed -duration 60s -json <run>/load.json [-query-budget -1]
```

The writer takes an epoch every 10 s with a 10 s limit, as in the baseline (production's default interval is 30 s). The target is therefore no lost epoch and a write p95 within 2.5 s. The default runs pass no setting, so they ran as the earlier runs did: a query budget of 4 (derived from 12 DuckDB threads), DuckDB's default `memory_limit` of 25.5 GiB and 12 threads. The "off" runs pass `-query-budget -1`. A `natslens run` collector unrelated to these runs ran on the machine throughout; no other `natslens-perf` ran.

**Engine logging.** The 20 runs named `default-*` and `off-*` ran with DuckDB's logging on, so that a lost epoch or a duplicate key would leave its engine log behind. They used a measurement-only build of `natslens-perf` that runs `CALL enable_logging(level => 'debug', storage => 'file', ...)` after the open, with `PhysicalOperator`, `AdaptiveFilter`, `FileSystem` and `Metrics` disabled. That left the query, transaction and checkpoint entries, 3 to 13 MB a run. This build is not committed. The three `nolog-*` runs used the committed binary without logging, as a control for the logging's own cost.

### Every run

With 5 to 9 epochs a run, a run's write p95 is its slowest write. A write's time runs from the call to its return, so it includes the wait for the store's lock; that wait's p95 was under 1 ms in 15 runs, and in the 5 s or 10 s bucket in three (`default-01`, `default-07`, `off-02`). "Missed" counts epochs stored late or not at all, and "lost" those whose write failed. "Overload answers" counts queries whose call's deadline passed in the budget's queue (`outcome="deadline"`); no queue filled. "Checkpoints" counts the `Start Checkpoint` entries in the engine log.

| Run | Budget | Load before / after | Epochs | Missed | Lost | Write p50 | Write p95 | Write max | Overload answers | Failed calls | Hours archived | Checkpoints | Duplicate key |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| default-01 | 4 | 7.3 / 10.0 | 5 | 4 | 3 | 10.2 s | 23.8 s | 23.8 s | 9 | 9 | 2 | 3 | none |
| default-02 | 4 | 9.3 / 10.7 | 8 | 4 | 3 | 2.2 s | 15.9 s | 15.9 s | 3 | 0 | 12 | 5 | none |
| default-03 | 4 | 10.4 / 21.6 | 6 | 1 | 1 | 2.2 s | 10.1 s | 10.1 s | 3 | 0 | 16 | 4 | none |
| default-04 | 4 | 19.9 / 12.3 | 5 | 5 | 4 | 10.9 s | 18.3 s | 18.3 s | 8 | 5 | 2 | 1 | none |
| default-05 | 4 | 11.6 / 19.4 | 7 | 7 | 4 | 10.2 s | 15.7 s | 15.7 s | 10 | 9 | 2 | 4 | none |
| default-06 | 4 | 18.2 / 13.0 | 5 | 4 | 4 | 10.3 s | 25.8 s | 25.8 s | 8 | 4 | 2 | 2 | none |
| default-07 | 4 | 13.1 / 8.8 | 6 | 6 | 4 | 10.5 s | 13.1 s | 13.1 s | 6 | 4 | 2 | 3 | none |
| default-08 | 4 | 8.8 / 9.8 | 7 | 5 | 5 | 10.4 s | 21.0 s | 21.0 s | 9 | 5 | 6 | 3 | none |
| default-09 | 4 | 9.8 / 16.2 | 6 | 1 | 1 | 2.3 s | 10.2 s | 10.2 s | 5 | 0 | 14 | 3 | none |
| default-10 | 4 | 16.2 / 15.2 | 6 | 2 | 2 | 1.8 s | 10.1 s | 10.1 s | 4 | 0 | 12 | 3 | none |
| default-11 | 4 | 9.3 / 16.7 | 6 | 1 | 1 | 2.1 s | 10.3 s | 10.3 s | 4 | 0 | 14 | 3 | none |
| default-12 | 4 | 14.8 / 13.8 | 7 | 4 | 4 | 10.2 s | 15.8 s | 15.8 s | 4 | 5 | 6 | 4 | none |
| default-13 | 4 | 13.1 / 15.2 | 6 | 3 | 1 | 2.2 s | 10.4 s | 10.4 s | 9 | 4 | 10 | 4 | none |
| default-14 | 4 | 14.3 / 16.2 | 6 | 3 | 3 | 2.6 s | 10.3 s | 10.3 s | 8 | 4 | 8 | 2 | none |
| default-15 | 4 | 12.4 / 14.1 | 6 | 3 | 2 | 2.4 s | 10.2 s | 10.2 s | 14 | 4 | 8 | 3 | none |
| off-01 | off | 13.5 / 12.7 | 8 | 7 | 4 | 9.7 s | 11.4 s | 11.4 s | 0 | 5 | 2 | 4 | none |
| off-02 | off | 12.7 / 11.1 | 7 | 7 | 3 | 10.3 s | 19.2 s | 19.2 s | 0 | 6 | 2 | 5 | none |
| off-03 | off | 11.1 / 16.7 | 5 | 4 | 2 | 7.4 s | 10.3 s | 10.3 s | 0 | 5 | 2 | 2 | none |
| off-04 | off | 16.0 / 12.2 | 6 | 5 | 4 | 10.1 s | 12.5 s | 12.5 s | 0 | 4 | 0 | 2 | none |
| off-05 | off | 11.5 / 12.4 | 5 | 4 | 2 | 10.2 s | 10.7 s | 10.7 s | 0 | 3 | 2 | 2 | none |
| nolog-01 | 4 | 5.5 / 8.8 | 7 | 4 | 3 | 2.3 s | 35.3 s | 35.3 s | 9 | 7 | 6 | not logged | none |
| nolog-02 | 4 | 8.8 / 20.0 | 6 | 0 | 0 | 1.8 s | 2.5 s | 2.5 s | 2 | 0 | 12 | not logged | none |
| nolog-03 | 4 | 18.8 / 18.2 | 9 | 6 | 4 | 3.8 s | 17.5 s | 17.5 s | 8 | 6 | 10 | not logged | none |

Every lost epoch ran out of its 10 s limit (`context deadline exceeded` in the epoch write's statements: `consumer_config`, `stream_config`, `consumer_replicas`, the consumer staging table, `connections`, `account_stats` and others). None failed at once with a constraint error.

API latency per page, p50 / p95 in seconds. A page's time is from its first call to its last answer, including calls that failed, so 30 and 40 are the API's limits:

| Run | Overview | Streams | Consumers | Servers | Connections | Subjects | Timeline | Findings | Capacity | Topology | Agent batch | Agent contributors |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| default-01 | 3.2 / 3.2 | 2.1 / 17.1 | 1.7 / 30 | 0.5 / 9.7 | 0.2 / 0.5 | 0.5 / 30 | 0.3 / 0.4 | 0.1 / 0.1 | 30.3 / 56.1 | 0.9 / 26.4 | 40 / 40 | none finished |
| default-02 | 3.0 / 4.8 | 1.1 / 3.8 | 1.3 / 1.8 | 0.2 / 2.3 | 0.2 / 0.4 | 0.4 / 3.4 | 0.5 / 2.7 | 0.1 / 0.3 | 6.6 / 12.5 | 1.1 / 2.6 | 5.2 / 5.5 | 7.7 / 10.4 |
| default-03 | 2.6 / 6.0 | 1.2 / 2.2 | 1.3 / 2.2 | 0.4 / 1.7 | 0.1 / 0.4 | 0.4 / 1.6 | 0.4 / 1.3 | 0.2 / 0.6 | 6.8 / 9.2 | 1.5 / 3.6 | 5.5 / 6.0 | 8.8 / 11.1 |
| default-04 | 3.0 / 3.0 | 1.3 / 2.4 | 1.7 / 1.8 | 0.3 / 1.8 | 0.2 / 0.6 | 0.4 / 0.8 | 0.4 / 30 | 0.1 / 0.2 | 30 / 38.6 | none finished | 6.1 / 6.3 | 30.3 / 30.3 |
| default-05 | 3.2 / 3.2 | 2.4 / 30 | 0.7 / 1.9 | 0.4 / 1.9 | 0.1 / 0.5 | 0.4 / 3.9 | 0.4 / 30 | 0.1 / 22.7 | 30 / 54.9 | 1.7 / 26.9 | 40 / 40 | none finished |
| default-06 | 3.0 / 3.0 | 1.2 / 2.3 | 1.2 / 1.7 | 0.4 / 1.7 | 0.1 / 0.4 | 0.3 / 3.7 | 0.3 / 30 | 0.1 / 0.1 | 30 / 30 | none finished | 5.2 / 5.6 | 30 / 30.1 |
| default-07 | 3.5 / 3.5 | 2.1 / 2.6 | 1.2 / 2.0 | 0.6 / 1.9 | 0.1 / 0.7 | 0.4 / 30 | 0.4 / 0.5 | 0.0 / 0.1 | 30 / 30 | none finished | 6.2 / 6.6 | 30.1 / 30.1 |
| default-08 | 2.7 / 3.1 | 1.4 / 2.4 | 1.0 / 1.6 | 0.3 / 1.6 | 0.4 / 30 | 0.3 / 0.4 | 0.5 / 30 | 0.1 / 0.1 | 8.1 / 30 | 0.8 / 3.0 | 4.9 / 5.7 | 9.5 / 31.5 |
| default-09 | 3.1 / 4.6 | 1.4 / 6.6 | 1.5 / 2.0 | 0.5 / 1.7 | 0.1 / 0.6 | 0.4 / 1.4 | 0.4 / 1.1 | 0.1 / 0.2 | 8.5 / 9.3 | 0.9 / 1.9 | 5.2 / 6.8 | 8.6 / 10.3 |
| default-10 | 3.0 / 5.8 | 1.3 / 4.1 | 1.5 / 2.9 | 0.5 / 3.0 | 0.2 / 3.0 | 0.3 / 1.0 | 0.3 / 1.0 | 0.1 / 0.6 | 9.6 / 12.1 | 1.0 / 2.3 | 4.0 / 8.5 | 9.3 / 10.2 |
| default-11 | 3.1 / 6.5 | 1.4 / 2.4 | 0.9 / 5.3 | 0.4 / 2.0 | 0.3 / 8.3 | 0.5 / 0.9 | 0.4 / 0.9 | 0.1 / 4.4 | 7.3 / 17.7 | 0.6 / 1.4 | 5.0 / 8.1 | 10.3 / 17.8 |
| default-12 | 3.2 / 4.0 | 1.2 / 2.3 | 1.0 / 1.8 | 0.2 / 1.8 | 0.2 / 0.6 | 0.4 / 1.0 | 0.5 / 1.5 | 0.1 / 0.6 | 9.1 / 30.2 | 1.4 / 2.5 | 5.6 / 7.1 | 9.9 / 32.3 |
| default-13 | 2.2 / 3.4 | 1.1 / 2.9 | 1.0 / 3.5 | 0.4 / 1.9 | 0.2 / 0.7 | 0.3 / 0.6 | 0.4 / 0.5 | 0.0 / 0.3 | 9.2 / 30.8 | 1.2 / 5.9 | 6.0 / 6.7 | 9.4 / 36.1 |
| default-14 | 2.4 / 3.3 | 2.1 / 6.0 | 1.7 / 3.0 | 0.4 / 1.8 | 0.2 / 0.4 | 0.5 / 30 | 0.5 / 0.6 | 0.1 / 0.2 | 9.4 / 30 | 0.7 / 1.4 | 5.7 / 7.0 | 9.5 / 30.1 |
| default-15 | 2.7 / 3.0 | 1.2 / 2.4 | 1.0 / 3.1 | 0.3 / 2.3 | 0.2 / 0.7 | 0.3 / 0.6 | 0.4 / 0.7 | 0.1 / 0.2 | 9.3 / 32.4 | 0.7 / 30 | 5.3 / 5.4 | 9.0 / 31.1 |
| off-01 | 2.2 / 36.3 | 1.8 / 2.1 | 2.5 / 28.5 | 0.1 / 0.4 | 0.0 / 0.2 | 0.3 / 1.9 | 0.4 / 16.8 | 0.0 / 0.1 | 30.4 / 51.4 | 5.8 / 19.5 | 1.9 / 18.9 | 30.3 / 31.1 |
| off-02 | 2.1 / 37.2 | 1.8 / 2.0 | 2.5 / 30.6 | 0.1 / 0.3 | 0.0 / 0.1 | 0.2 / 1.2 | 0.4 / 20.2 | 0.0 / 0.0 | 31.5 / 46.2 | 6.2 / 10.0 | 1.7 / 24.2 | 30.4 / 33.0 |
| off-03 | 22.5 / 23.2 | 1.8 / 2.3 | 2.3 / 23.6 | 0.1 / 0.4 | 0.0 / 0.2 | 0.3 / 0.7 | 0.4 / 0.6 | 0.0 / 0.0 | 30 / 52.9 | 0.5 / 0.7 | 2.1 / 2.4 | 30.3 / 30.3 |
| off-04 | 2.3 / 11.1 | 1.6 / 12.3 | 2.5 / 20.4 | 0.1 / 0.4 | 0.0 / 1.1 | 0.4 / 0.4 | 0.3 / 0.4 | 0.0 / 0.1 | 30.4 / 30.8 | 2.0 / 10.8 | 1.7 / 2.8 | 30.2 / 31.8 |
| off-05 | 11.5 / 14.7 | 2.0 / 8.6 | 4.8 / 19.2 | 0.1 / 1.8 | 0.1 / 7.4 | 0.4 / 13.4 | 0.6 / 2.1 | 0.0 / 0.1 | 11.2 / 51.6 | 4.7 / 12.7 | 1.7 / 4.9 | 30.1 / 30.6 |

The `nolog-*` runs' pages, as medians of the three runs' p50 and p95, are in line with the default runs': Overview 3.1 s / 5.8 s, capacity 9.3 s / 30 s, agent contributors 9.2 s / 14 s, agent batch 5.5 s / 11.1 s, and every other page at most 1.5 s / 4.6 s.

### Totals

| Setting | Runs | Stalled (missed > 0) | Epochs | Missed | Lost | Runs with a write p50 under 3 s | Write p95 per run | Overload answers | Failed calls | Hours archived | Duplicate key |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Default, logging | 15 | 15 | 92 | 53 | 42 (46%) | 8 | 10.1 to 25.8 s | 104 | 53 | 116 | none |
| Off, logging | 5 | 5 | 31 | 27 | 15 (48%) | 0 | 10.3 to 19.2 s | 0 | 23 | 8 | none |
| Default, no logging | 3 | 2 | 22 | 10 | 7 (32%) | 2 | 2.5 to 35.3 s | 19 | 13 | 28 | none |

For comparison, the runs of [After the query budget](#after-the-query-budget), before the asset counts and the servers change, lost 8 of 18 epochs with the default budget and 7 of 19 without one.

### Target

**The target is not met.** 22 of 23 runs lost at least one epoch: 49 of 114 epochs in the default runs (with and without logging) and 15 of 31 with the budget off. Only one run, `nolog-02`, kept every epoch with a write p95 within 2.5 s. Every other run's write p95 was 10.1 s or more, because a lost write takes at least its 10 s limit. The logging does not explain the losses: the three runs without it lost 7 of 22 epochs. They are too few to say whether it adds to them.

### The servers change

All 22 stalled runs count as evidence about the servers change, and none showed the duplicate key. The 20 logged runs checkpointed 62 times, 1 to 5 times a run, with the API load running. Each of the 48 checkpoints that wrote `servers` found it as one segment of 5 rows. In the two failing runs before the change, `servers` held two segments of 5 rows, the deleted copies beside the live rows. Before the change, the duplicate key appeared in 2 of 8 stalled runs. At that rate, 22 stalled runs without one would happen about 0.2% of the time (0.75 to the power 22). The conditions differ, though: the earlier failures ran at budget 2, and the Overview's heaviest reads are gone now. These runs support the change, and the change stays proposed, not yet confirmed. The engine logs are not kept in the repository.

### Budget on and off

| Measure | Budget 4 (15 runs) | Off (5 runs) |
| --- | --- | --- |
| Epochs lost | 42 of 92 | 15 of 31 |
| Write p50, median of runs | 2.6 s (8 runs under 3 s) | 10.1 s (none under 3 s) |
| Hours archived per run | 2 to 16 | 0 to 2 |
| Overview loads per run | 4.8 | 2.6 |
| Overview p50 / p95, median of runs | 3.0 s / 3.4 s | 2.3 s / 23.2 s |
| Capacity p50 / p95, median of runs | 9.3 s / 30 s | 30.4 s / 51.4 s |
| Agent contributors p50 / p95, median of runs | 9.5 s / 30.1 s | 30.3 s / 31.1 s |
| Agent batch p50 / p95, median of runs | 5.5 s / 6.7 s | 1.7 s / 4.9 s |
| Consumers p95, median of runs | 2.0 s | 23.6 s |
| Failed calls per run | 3.5 | 4.6 |
| Most connections open | 23 | 26 |
| Peak resident memory | 8.4 to 14.5 GB | 12.2 to 13.6 GB |

The budget does not keep epochs: both settings lose about half. It does help everything else. With the budget, the median write is about 2 s in about half the runs, against 10 s in every run without it. The archive keeps up, and the API completes more page loads. The heavy pages have a lower p95 (Overview, consumers, timeline, capacity, topology, agent contributors). The light pages (servers, connections, findings, streams, subjects) are a little slower at p95, by 0.1 to 1.5 s. The agents' batch now waits in the queue: 5.5 s at p50 against 1.7 s.

The default runs fall into two groups:

- **Eight runs** (02, 03, 09, 10, 11, 13, 14, 15) had a write p50 of 1.8 to 2.6 s, 0 to 4 failed calls and 8 to 16 hours archived. They lost 14 of 50 epochs.
- **Seven runs** (01, 04 to 08, 12) had a write p50 of 10.2 to 10.9 s, 4 to 9 failed calls and 2 to 6 hours archived. They lost 28 of 42 epochs.

The load average before a run does not separate the two groups. What does was not isolated.

### What remains

- **The per-asset reads over a range.** Over the 15 default runs, `GetCapacity` ran 770 s of queries in 87 calls (22 failed) and `ListMetricContributors` 754 s in 50 calls (11 failed): 66% of the API's 2,299 query-seconds. `GetTimeSeriesBatch` (220 s), `ListConsumers` (194 s) and `ListStreams` (137 s) follow. These are recommendation 3's reads, and they keep the engine busy for seconds while an epoch is written.
- **A write that runs out of its limit returns late.** In 12 of the 23 runs a write returned 12.5 to 35.3 s after it was called, against its 10 s limit, and the lock wait does not account for it in nine of them. The epoch's hold of the store's lock (`op="epoch"`) had its p95 in the 30 s or 60 s bucket in 22 runs (2.5 s in `nolog-02`). The next tick's epoch then starts late, so one slow write costs more than one epoch. Whether the time after the limit goes to the interrupt or to the rollback of the epoch's transaction was not measured.
- **The yield's limit (`admit.YieldLimit`).** It still cannot be measured apart: the reads above hold the engine whatever the yield admits.

Proposed, not yet confirmed:

1. Move the per-asset rates over a range to rollups or edge reads (recommendation 3), then run this acceptance again.
2. Measure how long the epoch write takes to return after its limit, and what it spends that time on. Until it returns promptly, a lost write also delays the next epoch.
3. Keep the default budget. It does not save epochs, but it brings the median write to about 2 s in about half the runs, lets the archive keep up, and keeps page p95s below the API's limits more often.

## Spill

Measured on 2026-11-12 on the same machine with the branch `perf/spill-cap`. On the `entities` profile DuckDB's spill folder (`<store>.tmp`) had grown to 3.3 to 4 GB within the first 30 to 35 s of `mixed`, with DuckDB's default memory limit and with 4 GiB. To leave room on the disk for the spill, these runs use `entities-6h`, the `entities` assets over six hours (1.13 GB, 720 epochs), each on an APFS clone deleted after it:

```sh
natslens-perf load -profile entities-6h -db <clone> -scenarios mixed -duration 60s -memory-limit 1GiB -max-temp-size 4GiB [-threads 2]
```

`load` samples `duckdb_temporary_files()` every 250 ms and reports the peak as "peak spill"; it logs each 256 MB of growth with its time. The sample is a query on the store's pool, outside the query budget, so it adds one connection at most to the connection counts of the same report.

| Scenario | Threads | `memory_limit` | Bound | Peak spill | Failed calls | Epochs lost |
| --- | --- | --- | --- | --- | --- | --- |
| `mixed` | 12 | 25.5 GiB (default) | 2 GiB | 0 MB | 0 | 0 of 7 |
| `mixed` | 12 | 1 GiB | 2 GiB | 2,047 MB (the bound) | 35, answered `internal` | 0 of 7 |
| `mixed` | 12 | 1 GiB | 4 GiB | 2,994 MB | 0 | 0 of 6 (1 late) |
| `mixed` | 2 | 1 GiB | 4 GiB | 1,094 MB | 0 | 0 of 7 |
| `writes-only`, 40 s | 12 | 1 GiB | 2 GiB | 223 MB | 0 | 0 of 4 |
| `tabs-4`, 40 s | 12 | 1 GiB | 2 GiB | 2,043 MB (the bound) | 15 | none written |
| `agents-4`, 40 s | 12 | 1 GiB | 2 GiB | 1,412 MB | 0 | none written |

- **The API's reads spill, the writer hardly.** With no API load the writer, its checks and the archive spilled 223 MB. The pages alone reached the bound, the agents alone 1.4 GB. Each call of the pages and agents run alone spilled nothing; the heaviest held 850 to 920 MB of DuckDB working memory (`ListMetricContributors`, 6 hours), `ListConsumers` and `GetCapacity` 150 to 280 MB. The spill is their sum over the memory limit, so it grows with the reads run at once: with 2 threads the query budget admits 1 read and the spill peaked at 1.07 times the memory limit, with 12 threads it admits 4 and the spill reached about 3 times. It falls to zero between waves of reads.
- **At the bound, whatever needs memory next fails.** In the second run 35 API calls failed, and the same load under a 4 GiB bound failed none. Two checks (`consumer-009`, `consumer-013`) failed with DuckDB's Out of Memory error, the commit of the epoch's loopback decision failed, and one archive export failed and was exported at a later tick. No epoch write failed, and the store was never marked failed. Calls like these now answer `resource_exhausted`.
- **`preserve_insertion_order`.** Off, the spill peaked at 1,069 MB with 2 threads and 3,571 MB with 12, against 1,094 MB and 2,994 MB on: no gain, so it stays on.

The default bound is the memory limit times the reads the query budget admits plus one: twice the limit at the budget of 1 of 2 threads, above the 1.07 times measured there, and five times at the budget of 4 of 12 threads, above the 2,994 MB measured there; it is lowered to 90% of the free disk. Proposed, not yet confirmed.

## Instrumentation

The `/metrics` families these numbers come from are listed in [Metrics](../metrics.md). Recording costs about 40 ns and one allocation per observation, and about 111 ns and one allocation per query on the query path (`go test -bench . ./internal/timing`), against queries of a millisecond and more. `natslens` serves no profiling endpoint; `natslens-perf explain` sets a query hook, off by default, that the `natslens` command never sets.

## Not measured

- The OS file cache was never dropped, so "cold" is cold for DuckDB only.
- Each scenario ran once, on a workstation with a desktop session; no repeats, no confidence intervals.
- The browser's own cost: these are API times. [Measure web loading](measure-web-loading.md) covers the browser.
- More than one system in a process, Kubernetes, micro services, subscriptions and Raft samples; the generator does not produce them.
- A full week at `entities` size (31 GB), which did not fit the disk.
