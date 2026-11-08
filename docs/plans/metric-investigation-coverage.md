# Metric investigation coverage

Task `bXMlS0kOLTauMBDTEL280` remains in progress. This inventory preserves the whole cross-metric acceptance; an API foundation alone does not complete chart drill-down.

| Metric family | Current evidence/destination | Remaining or explicit limits |
| --- | --- | --- |
| Server CPU | Native last/mean/peak and server comparisons | Global ranking remains; one-core CPU is not host saturation |
| Resident memory | Native raw observations and server histories | Global ranking remains; resident bytes are not host memory pressure |
| Connections | Count last/min/peak and server histories | Counts do not identify affected client connections |
| Message/byte traffic | Separate directions, raw rate peaks, histories and exact comparable server-counter increases | Complete volumes, resource sequence volumes and global ranking remain |
| Storage | Capacity endpoint-pinned change, signed contributors, resource byte histories | Logical/observed-copy/physical distinctions; no invented replica usage |
| Slow-consumer detections | Bounded server-process/collection evidence | Never infer unique affected clients from counters |
| Stream messages/bytes/ingestion | Typed resource pages, native summaries, bounded comparisons | Incarnation ambiguity, capture gaps; not per-key/object measurements |
| Consumer pending/ack/redelivery/delivery | Typed resource pages and scoped diagnostics | Global rank/volumes where meaningful; delivery is not server slow-consumer detection |
| Server/resource/account counts and replica groups | Aggregate native histories and inventory/diagnostic links | Counts alone cannot identify historical membership or a cause |
| Meta changes | Exact observed total, per-collection peak, events/server evidence | Derived transitions differ from advisories; no complete-election or independent-group-health claim |

## First dependency

Existing Series min/max describe rendered bucket values (or band edges), not uniformly raw observations. The API foundation now adds opt-in, resolution-independent observation summaries through the existing bounded GetTimeSeries/GetTimeSeriesBatch path. Retain the same metric SQL, entity resolution, range semantics, request deadlines and offline handlers. Keep legacy responses and work unchanged when the option is absent. Gauge/rate summaries describe actual metric samples; event sums have their own exact decimal total. Unknown values and raw times are explicit. This does not integrate rates into volumes, reconstruct process identity, or establish complete capture.

Proposed, not yet confirmed: observation summaries are opt-in and limited to the web's 30-day maximum; use the existing 32-query batch bound. Future detail pages must label sample-weighted means and observed peaks separately from bucket means, use actual timestamps, show missing evidence and link findings/events without claiming causes.

## Whole-task acceptance still open

Traffic volumes where supported, global metric contributor ranking and richer historic identity/set attribution remain open. Catalogue-backed titles, native observation pages, loaded-page sorting/filters, fixed evidence and interval/Back paths are implemented in the slices below. Existing mini-chart rows retain resource detail and interval entry points; dedicated Timeline and topology views retain their own interactions. No new profiling or collection is implied.

## Server page slice

CPU, memory, connection counts and both traffic-rate directions now have shared metric routes, pre-bucket summaries, twelve-name comparison pages, fixed-range findings/events, selected server histories and interval navigation. Storage and detections reuse specialist destinations. The inventory read remains separate and unbounded by this page-size policy. Remaining whole-task work includes resource metric pages, complete metric-family title coverage, traffic volumes where supported, global contributor ranking and broader historical/process identity evidence. Do not close the umbrella task on this slice.

## Resource page slice

Stream messages/logical bytes/observed replica bytes/ingestion and consumer pending/ack-pending/redelivered/delivery now have native observation pages. One twelve-resource summary page precedes a maximum twelve-series history batch; returned continuation bounds are authoritative. Explicit historical resources do not depend on current inventory. Shared detail chart titles, selected intervals, copied context and fixed resource evidence retain identities. No global metric ranking, exact traffic volume or individual KV/object measurement is claimed. Remaining inventory includes aggregate server/asset/account counts and meta-change destinations, broader cross-resource ranking and traffic volume support.

## Aggregate destination slice

All37 current API catalogue metric IDs now have a declared meaningful destination, enforced by a catalogue conformance test. Eight aggregate pages cover9 count/meta metric IDs (two asset components share a page). All9 Overview tiles use canonical destinations even without history. Aggregate pages use at most2 observation queries, exact transition totals, native timestamps/gaps and fixed system-wide evidence links; counts do not imply identifiable contributors. Remaining umbrella acceptance includes complete supported volume coverage and global ranking, not an unqualified claim that those comparisons already exist.

## Comparable server traffic evidence

The eight cluster/server incoming/outgoing message/byte rate metrics expose exact observed counter increases with pair/ID/skipped-row evidence through the native observation API, server comparison pages and remote/offline requests. This does not integrate plotted rates or establish complete interval volumes. Resource ingestion/delivery sequence volumes and global contributor rankings remain separate acceptance; synthetic demo rates have no cumulative-counter evidence.
