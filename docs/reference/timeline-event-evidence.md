# Timeline event evidence

Status: proposed, not yet confirmed.

`GetStatusTimeline` returns its resolved range and an optional `eventReadId`. Presence matters: zero means known empty membership; absence means unsupported or unrequested. The native boundary is the retained maximum event ID captured by the Timeline read transaction. It excludes later IDs, not updates, deletions, configuration changes or linked finding changes. The demo encodes a retained scenario reference frame so the same selected event does not move when its clock advances.

Lane rows retain event IDs, named entities, finding IDs and known transition collection identities. Recorded transition data is distinct from the separately read latest finding row. Missing metadata stays unknown. No exact condition onset, causal claim or universal snapshot pin follows from a mark.

## Exact reads and limits

- `GetEventBatch` accepts 1–50 unique canonical positive decimal int64 `ids`, plus optional `eventReadId` and system. It returns retained matching summaries and the acknowledged identities/boundary. It deliberately has no timestamp predicate: window inputs can precede or follow the selected Timeline interval. Missing IDs are not replaced by nearby records.
- `GetEvent` accepts the optional boundary and returns one explicitly requested payload. Missing or out-of-bound identities fail rather than selecting another row.
- `ListEvents` accepts up to 1,000 `selectedIds` plus an optional boundary while retaining its normal range/filter/paging semantics. Its filtered and unfiltered counts remain within the selected membership.
- Advanced selected/bounded responses fit the existing 512 KiB JSON and protobuf page budget. Oversized payloads fail explicitly. These are response limits, not hard SQL-engine resource isolation.
- UI contributor pages request 50 identities per explicit action; they do not automatically traverse all pages or fetch all payloads. The Timeline still has its existing input limits. Missing identities and truncation remain visible.

The CLI and browser require acknowledgement before trusting exact selections. A prior server may ignore additive protobuf fields; an unacknowledged or broadened result is rejected. IDs remain decimal strings in JSON, including values beyond JavaScript's safe integer range. New RPC/messages/fields are additive; older clients keep their prior feed behavior.

## Inferred windows and links

A window ID hashes the exact ordered derivation inputs and gap rule. It is a reconstruction identity, not a signed artifact, authentication mechanism or incident ID. `supportingEventIds` retains all unique known event rows in derivation order. `evidenceTotal` counts input rows, not Kubernetes aggregated occurrence counts. Missing/duplicate IDs and input limits are explicit. Version/deployment/node grouping explains an observed window; it proves neither uninterrupted capture nor maintenance duration nor causation.

`/timeline/windows/:id?system=…&from=…&to=…&eventReadId=…` reconstructs against the fixed original query and captured membership. If retention or changed input prevents the exact identity from matching, it reports unavailable. It does not replace the window with a current one. Event links preserve the same system and fixed interval; resource links are supporting historical context, not proof that resource metadata is pinned.

Copy as prompt preserves exact timestamps, identities and the machine request. `GetEventBatch` is available through the existing read-only live/offline handler. UI links that cannot reproduce an exact machine selection remain broader context with partial-scope disclosure.

## Selected source-limit warnings

The event input flags are separate: `nats_truncated` covers the NATS/collector lane inputs, `finding_truncated` the finding-transition inputs, and `kubernetes_truncated` the Kubernetes lane inputs. Preview and selected-detail warnings retain that original source-range context through refresh and into event prompt annotations. A source limit does not establish that any particular group or zoom window omitted members. These flags do not classify per-check state rows or the separately bounded inferred-window derivation input set. Unset/false flags are not a complete-capture guarantee.

The notice describes input/read limits on the returned Timeline. Other retained records remain accessible through the bounded event feed and exact-identity reads; a successful single-record read does not establish complete coverage.
