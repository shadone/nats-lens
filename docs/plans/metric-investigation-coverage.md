# Metric investigation coverage

Task `bXMlS0kOLTauMBDTEL280` remains in progress. This inventory describes the current cross-metric implementation and preserves the remaining acceptance.

| Metric family | Current evidence/destination | Explicit limits or remaining work |
| --- | --- | --- |
| Server CPU | Native last/mean/peak, server histories and global ranking | Percent of one core, not host saturation; name grouping can span lifetimes |
| Resident memory | Native observations, server histories and global ranking | Resident bytes are not host memory pressure |
| Connections | Native count last/min/peak, histories and global ranking | Counts do not identify client connections |
| Message/byte traffic | Separate directions, raw rate peaks, histories, exact comparable server-counter increases and ranking | Complete volumes and resource sequence-volume semantics remain limited |
| Storage | Capacity endpoint-pinned changes, signed contributors, resource byte histories and native observation ranking | Logical/observed-copy/physical distinctions; no invented replica usage |
| Slow-consumer detections | Bounded process/collection evidence and native observed-total ranking API | Never infer unique affected clients or a cause from counters |
| Stream messages/bytes/ingestion | Typed resource histories, summaries, global ranking and fixed evidence links | Incarnation ambiguity; no per-key/object measurements |
| Consumer pending/ack/redelivery/delivery | Typed histories, native summaries, global ranking and scoped diagnostics | Sequence-volume semantics remain; delivery differs from server slow-consumer detection |
| Server/resource/account counts and replica groups | Aggregate native histories, inventory/placement/coverage links | Counts alone cannot identify historical membership or a cause |
| Meta changes | Exact observed total, per-collection peak and supporting events/server evidence | Derived transitions differ from advisories; no complete-election or independent-group-health claim |

## Shared evidence and navigation

All 37 current API catalogue metric IDs have a declared meaningful investigation destination when their required identity is available. Nine Overview tiles keep canonical destinations even without loaded history. Native pre-bucket summaries distinguish observed extrema and actual timestamps from bucket values; means are sample-weighted and unknown is not zero. Supported title/interval navigation, fixed findings/events, Back/share and copied metric identities retain scope. Aggregate pages use at most two observation series and disclose system-wide versus account scope. Existing storage and detection specialists retain their metric-specific semantics.

Server comparison histories use twelve names and at most twenty-six series per batch. Resource histories use one twelve-resource page followed by at most twelve series; explicit historical selections bypass current inventory. Each chart uses its own fixed bounds. The older loaded-history table sorting remains local and clearly labeled; the server inventory read remains separate and is not bounded by that page size.

## Comparable traffic evidence

The eight server/cluster incoming/outgoing message/byte rate metrics expose exact observed counter increases, admitted-pair counts, contributing stored IDs, skipped/boundary rows and collection envelopes. Report adjacency is preserved before timestamp filtering. No rate integration, continuous capture or complete interval volume is claimed. Synthetic demo curves do not fabricate cumulative-counter evidence.

## Native global rankings

All eighteen server/stream/consumer catalogue metrics support `ListMetricContributors`, reusing the same authoritative observation SQL. Separate shared panels expose global ordering for the fifteen metric IDs on server/resource comparison pages. Users can rank names, latest/mean/extrema and supported exact counter increases, including entities outside the currently loaded history page. Resource candidates come from retained reports across the full selected interval, not only the latest inventory.

Proposed, not yet confirmed: captures allow 20,000 identities and 2 MiB of summaries, with explicit overflow errors. The existing five-minute/eight-context traversal freezes values and ordering, supports immutable-source reopening and retains row/wire budgets. Detailed histories are not built for every candidate. Native engine work and aggregate concurrent-request memory remain outside those application bounds. Histories/evidence are separate reads, not a persistent pin.

Agents have the same explicit request through the read-only API/CLI. UI ranking choices and range are shared in the URL; cursor position is session-local. Copied browser ranking controls are not automatically a ranking request. See [ranking fields and requests](../reference/metric-contributors.md).

## Whole-task acceptance still open

Complete supported volume coverage, resource sequence/incarnation evidence and richer historical set attribution remain limited. Aggregate changes cannot always identify a resource from stored evidence; those pages disclose the limit and offer supporting diagnostics. No new profiling, client causation, physical resource measurements or universal collection pin is implied. Do not close the umbrella task merely because its API/navigation and ranking slices are implemented.
