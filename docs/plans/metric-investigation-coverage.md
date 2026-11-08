# Metric investigation coverage

Task `bXMlS0kOLTauMBDTEL280` remains in progress. This inventory preserves the whole cross-metric acceptance; an API foundation alone does not complete chart drill-down.

| Metric family | Existing evidence/destination | Remaining detail work |
| --- | --- | --- |
| Server CPU | Server detail, cluster band and server series | Raw-observation last/mean/peak, comparative server view; CPU is percent of one core, not host saturation |
| Resident memory | Server detail and cluster band | Same raw-observation distinction, resident process bytes and historical contributors |
| Connections | Connection list plus server count series | Count-specific last/min/peak and historical server comparison; counts are not identities |
| Message/byte traffic | In/out server and cluster rate series | Separate directions, observed rates and supported counter volumes; resets/gaps must not invent traffic |
| Storage | Capacity, endpoint-pinned storage change and contributors | Canonical metric-title navigation and entity/fleet transitions; reuse logical/observed-copy/physical distinctions |
| Slow-consumer detections | Bounded server-process/collection investigation | Canonical title navigation; reuse original observation/evidence semantics, never infer unique affected connections |
| Stream messages/bytes and ingest | Stream/bucket details and series | Shared title destination, retained entity scope, count/rate/storage-specific summaries |
| Consumer pending/ack/redelivery/delivery | Consumer details and series | Shared title destination and diagnostics; delivery rates differ from server slow-consumer counters |
| Server/asset/account counts, meta changes | Servers, Placement, Accounts and Timeline | Explicit meaningful destinations and observation limits, rather than inventing per-entity attribution |

## First dependency

Existing Series min/max describe rendered bucket values (or band edges), not uniformly raw observations. The API foundation now adds opt-in, resolution-independent observation summaries through the existing bounded GetTimeSeries/GetTimeSeriesBatch path. Retain the same metric SQL, entity resolution, range semantics, request deadlines and offline handlers. Keep legacy responses and work unchanged when the option is absent. Gauge/rate summaries describe actual metric samples; event sums have their own exact decimal total. Unknown values and raw times are explicit. This does not integrate rates into volumes, reconstruct process identity, or establish complete capture.

Proposed, not yet confirmed: observation summaries are opt-in and limited to the web's 30-day maximum; use the existing 32-query batch bound. Future detail pages must label sample-weighted means and observed peaks separately from bucket means, use actual timestamps, show missing evidence and link findings/events without claiming causes.

## Whole-task acceptance still open

Accessible cross-page title links; complete metric detail pages; useful sortable entity comparisons/filters; traffic volumes where supported; scoped findings/events/diagnostic links; historical identity/restart/freshness evidence; chart interval/Back/share support; mobile/theme and end-to-end browser paths. The API foundation is reviewed independently before those pages consume it. No new profiling or collection is implied.

## Server page slice

CPU, memory, connection counts and both traffic-rate directions now have shared metric routes, pre-bucket summaries, twelve-name comparison pages, fixed-range findings/events, selected server histories and interval navigation. Storage and detections reuse specialist destinations. The inventory read remains separate and unbounded by this page-size policy. Remaining whole-task work includes resource metric pages, complete metric-family title coverage, traffic volumes where supported, global contributor ranking and broader historical/process identity evidence. Do not close the umbrella task on this slice.

## Resource page slice

Stream messages/logical bytes/observed replica bytes/ingestion and consumer pending/ack-pending/redelivered/delivery now have native observation pages. One twelve-resource summary page precedes a maximum twelve-series history batch; returned continuation bounds are authoritative. Explicit historical resources do not depend on current inventory. Shared detail chart titles, selected intervals, copied context and fixed resource evidence retain identities. No global metric ranking, exact traffic volume or individual KV/object measurement is claimed. Remaining inventory includes aggregate server/asset/account counts and meta-change destinations, broader cross-resource ranking and traffic volume support.
