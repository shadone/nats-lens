# Storage comparison API

`GetStorageChange` compares retained collection endpoints for one configured system. The web evidence page, remote agent client and offline source use the same handler and finishing rules. Policies below are **proposed, not yet confirmed**.

## Request and endpoint policy

The request names `system`, optional `account`, required ordered timestamps `a` and `b` at most 30 days apart, optional `epochA` and `epochB`, and `pageSize`/`pageToken`. Optional `order` selects absolute magnitude (default), logical ascending or logical descending. `resourceKind` selects one resource type; `resourcesOnly` excludes server rows from the displayed list. Zero page size means 100; sizes outside 1–100 fail. Missing, reversed, invalid or future timestamps fail. The sole future-bound exception is the captured source's existing disclosed one-microsecond exclusive-end adjustment. Actual replies must still finish by the source clock.

Each unpinned endpoint selects the collection with the latest finish at or before its target, then latest start and ID. The collection window must be valid. A pin selects only that collection and fails if it is no longer retained or its window lies after the allowed target. No interpolation or nearest future sample is used. Requested timestamps remain unchanged in the response.

Each endpoint returns its collection ID, start/finish window, age measured from finish, `inventoryComplete` and reasons. Collection windows bound reply acquisition; they are not exact per-server measurement times. Age above three collection intervals adds `stale_collection`. Deltas and contributions require two distinct, available, fresh collection windows. Otherwise endpoint values remain visible but change is unknown (`no_comparable_endpoints`). Unrelated endpoint failures do not establish incomplete JSZ inventory. Expected names, process ambiguity, successful JSZ coverage and agreement between reported stream counts and retained detailed reports determine that inventory; disabled JetStream is known zero.

## Quantities and identities

All byte measurements and deltas are optional signed int64 fields, encoded as decimal strings in protobuf JSON. Before and after measurements are nonnegative. Differences retain their sign. Unknown presence, identity, configuration, replica coverage and arithmetic overflow cannot become a zero measurement.

| Quantity | Meaning |
| --- | --- |
| `logical` | One copy, from a unique leader report, or agreeing usable copies when no leader reports |
| `observedReplicas` | Sum of all usable configured copy reports, including the leader; not physical allocation |
| `estimatedReplicas` | Logical bytes multiplied by a consistent configured replica count; an estimate |
| `serverReported` | JSZ file or memory usage for the named server, covering all accounts |

Resource IDs include account, underlying stream, reported creation time and storage category. KV and object prefixes determine type and bucket label only. Unknown or conflicting identity/configuration is qualified rather than merged into a proven group. Process IDs remain evidence even when a stable server name continues across restart. Historical configuration comes from runs covering the resolved collection, matched to each reporting process and creation time.

An exact-account `stream.deleted` event between completed windows qualifies a resource's absent B endpoint when creation identity is known. A uniquely observed later incarnation created inside that interval qualifies retirement of the earlier one. Matching events are bounded by their event time and by receipt no later than B's finish. Their IDs remain observations, not claims about the cause of a storage increase. Account-alias or unscoped lifecycle events do not qualify zero.

## Ranking, budgets and replay

Resources sort by absolute logical delta by default, with signed ascending/descending options. Unknowns remain last in every order, with stable IDs breaking ties. Servers follow resources and sort by absolute usage delta, then ID. Type/row filters apply before paging but do not change all-type totals. Off-page contributions include filtered-out resources. Each storage category has full totals when evidence permits, known resource contributions, off-page contributions, unknown-row counts and a qualified residual. The residual is system-wide server-usage delta minus complete observed-copy delta; it is unavailable for account scope or incomplete quantities. Memory never contributes to file totals.

Proposed limits are 20,000 ranked rows, 100 rows and 512 KiB per serialized page (both protobuf and JSON), plus the shared 100,000-row/8 MiB metadata scanner budget across a comparison. Oversized responses fail; reduce scope or page size rather than treating overflow as complete. SQL execution and engine intermediate memory are outside the scanner bounds. The shared 30-second handler budget and caller cancellation still apply.

All provider reads use one database transaction. Pagination tokens bind the normalized system/account/requested bounds, resolved collections, page size, ordering/type/row selection and a hash of retained evidence. Tokens are unsigned transport data, not authorization. They never grant another system or account. Changed evidence rejects continuation. Keep the original request parameters when following `nextPageToken`; no automatic traversal occurs.

The returned `investigationUrl` opens `/capacity/storage-change` with scope, requested moments and known pins. Unavailable endpoints have no fabricated zero pin. The handler names the resolved default system explicitly. Offline responses omit this URL and report `offline_ui_unavailable`; retrieve the same typed evidence with the local source instead.

## Agent commands

The existing client discovers and describes the method:

```sh
natslens investigate describe GetStorageChange
natslens investigate query GetStorageChange -endpoint http://127.0.0.1:8080 \
  -system example-prod -account APP \
  -from 2026-10-17T13:00:00Z -to 2026-10-17T14:00:00Z -page-size 25
```

Use a configured trusted profile for protected remote access. Use the same method with `-bundle DIR` or explicit `-db FILE -system ID -collection-interval 30s` for local captured data. Returned contexts bind the response's explicit system and collection pins for replay; the actual sent request remains separately recorded. A request file can also supply pins and an explicit continuation. Copied evidence-page context retains exact timestamps and pins, including IDs above 2^53. Explicit system/range changes reset inherited pins and continuation; account changes reset continuation. The exact machine request remains separate from lossy UI projection.

Important reasons include `no_comparable_endpoints`, `no_collection`, `stale_collection`, `missing_jsz`, `resource_inventory_incomplete`, `not_observed_at_a`, `not_observed_at_b`, `incarnation_unknown`, `invalid_creation_time`, `replica_coverage_incomplete`, `ambiguous_configuration_or_identity`, `observed_delete_event`, `incarnation_replaced`, `created_after_a`, `configured_replicas_changed`, `server_process_changed`, `server_usage_is_system_wide`, `exclusive_end_inclusion` and `offline_ui_unavailable`. A reason explains evidence availability or an observed boundary; it does not prove causation.

Resource-detail links select the last observed endpoint window. Links retain their whole-second inclusion convention, using the next whole second after the window finish. Shared range URLs also preserve millisecond precision when supplied. Exact collection windows and pins remain on the comparison page; this does not create universal pins for other detail RPCs.

A live preset on the evidence page resolves to fixed bounds using the loaded source clock. Explicit range changes discard old collection pins. Available endpoint pins and the resolved system are written into the URL before continuing a page; unavailable endpoints have no invented pin.

In the synthetic demo, comparison pins also identify their scenario frame, so advancing the live demo clock does not silently move their evidence. Treat these IDs as opaque; other detail RPCs do not gain comparison pinning.

## Capacity contributor selection

Capacity keeps a frozen comparison beside its latest-collection bounds/forecast table. Its `change=asc|desc` and `resource=stream|kv|object` URL selectors request global signed ordering and type selection. Missing/unknown values use magnitude order and all types. The pinned evidence link also carries `contributors=1`. That page and imported agent context map the same selectors to `order`, `resourceKind` and `resourcesOnly`; exact machine requests remain separately recorded.

The contributor table follows one page explicitly. Its bars scale within the current page. Explore reads at most two history series for one selected resource, using existing chart gap/inspection rules. Configuration links use the historical endpoint window; event/consumer links use the requested interval. Those supporting reads do not inherit universal collection pinning or causal semantics.
