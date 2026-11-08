# Metric observation API foundation verification

Board task `bXMlS0kOLTauMBDTEL280` remains **in progress**. Base `e7cf67dc`. The complete cross-metric inventory and remaining UI acceptance are in [the coverage plan](metric-investigation-coverage.md). This deliverable is the reusable API foundation, not completion of metric detail pages.

## Implemented contract

Additive Series.observations (field 10), GetTimeSeriesRequest.include_observations (6), SeriesQuery.include_observations (4), and the new ObservationSummary message. No existing field numbers change. Both single and bounded batch requests, the same offline handler, and dynamic read-only CLI support the option. Ordinary requests do not acquire an observation transaction or execute the extra summary query. Opt-in ranges are proposed, not yet confirmed, at most 30 days.

Stored curves and observations share one read transaction. The narrow read view does not copy Provider caches or acquire Store.mu. It reuses metric SQL and a shared band definition; ordinary band queries retain their known-value filter. Summaries aggregate one SQL result row rather than materializing every sample in application memory. Engine intermediate work and source MVCC are not hard memory/disk budgets; existing request deadlines still apply. Batch siblings are not one shared snapshot.

Values include known/returned metric-row counts, raw collection timestamps, arithmetic mean, extrema, earliest maximum timestamp and latest value with NULL/conflicting latest-time evidence left unknown. Band median statistics and raw edges remain separate. Summed increments return an exact decimal integer; rates and gauges receive no fabricated total. Counts do not certify completeness or reconstruct missing collections/process identities. Existing floating-point metric statistics are not exact integer storage arithmetic.

## Runtime checkpoints

- `9f529bb3`: missing observation summary runtime RED for CPU, memory and connections. A coarse CPU bucket is 46.7 while a captured sample is 100; NULL latest remains unknown.
- `a97cab7a`: native API GREEN; `4fb80961` demo multi-family runtime RED; `061f2972` demo GREEN.
- `4468c50a`: concurrent append runtime RED: curve 10 versus summary maximum 1000. `819497c4` fixes the shared response boundary. The writer commits atomically after the original read boundary; the test asserts it actually executed.
- `39ae98f5` / `05c0e899`: absent demo capture became a known zero summary, RED then GREEN.
- `fbb930aa` / `713f2236`: the corresponding demo event curve also fabricated zero; RED then GREEN. Shared capture presence now governs event buckets as well as observations.
- `7267cd8e` / `6e2c8867`: an opted-in curve created points between native demo sample times while its summary had none; RED then GREEN with the same grid. Standalone chart sampling is retained except the demonstrated event-gap correction.
- `7016e127`: distinct rows tied at the latest timestamp selected an arbitrary latest value; corrected by requiring agreement and known values. Equal known ties remain known; mixed NULL/conflicting ties remain unknown.

The first concurrent-write fixture missed required schema columns and failed before its assertion. That setup error is excluded; only the corrected runtime mismatch is proof. An initial pre-existing band/count control run was sandbox-denied on disposable loopback binding; the approved retry passed. An initial coverage-report command used an inaccessible default cache; rerunning the reporting tool with the private cache succeeded. None are product regression proof.

## Verification scope

- Focused native tests: resolution invariance, NULL/zero distinction, sample mean/peak/times, bands versus medians, exact totals above 2^53, rates without fabricated volumes, empty/all-NULL evidence, half-open bounds, every registered metric, opt-in legacy behavior, cancellation and concurrent writes.
- Batch tests: individual opt-in propagation; a >30-day query fails alone while the ordinary sibling succeeds; direct invalid_argument.
- Demo tests: all primary server families, native bands, coarse/fine invariance, missing capture, exact sample-grid emptiness, validation/cancellation and legacy absence of summary.
- Frozen live/offline parity includes both requested single-series observations and a mixed batch. No listener is created by the offline source; the live test uses a disposable loopback server.
- Focused Pacific/Honolulu race runs and existing band/count controls pass. The complete demo package passed after the capture-gap correction; final grid-option controls are run afterward.
- Core focused coverage before the final grid/timestamp controls: native observationSummary **91.3%**, demo observationSummary **90.9%**, shared lookback/band definition **100%**, native opt-in wrapper **84.2%**. These are function coverage, not broad package coverage (focused package totals are deliberately low).
- `make -o web/node_modules/.package-lock.json proto` runs lint, format and generation without installation; borrowed existing dependencies only. TypeScript and scoped vet pass. No frontend logic or migration/dependency changes; no broad web/Go/check-engine suite claim.

Private logs are `/private/tmp/nats-series-*`. Removal overlays do not mutate source. API/CLI receipts and fresh review are recorded below after completion.
