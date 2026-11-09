# Retained finding occurrences

Status: proposed, not yet confirmed.

`ListFindingEpisodes` reads retained finding occurrences overlapping `[range.start, range.end)`. It is separate from the compatibility `ListFindings` projection. A recurrence never replaces an explicitly selected occurrence. The regular API and protected read-only investigation handler use the same normalization and provider.

Select one occurrence with `findingId` and `firstRetainedEpoch`. The first retained identity can represent a fragment after retention; optional `startEpoch` exists only when the original firing transition remains. Epochs and event IDs are decimal strings in JSON. A stream entity also includes its consumers. An unset entity covers the system; account UI context alone does not establish an entity filter.

The response separates:

- `finding`: recorded detail, severity and transition state before the selected interval end;
- `observedResolutionAt`: a retained resolution, possibly later than that endpoint;
- `latestRetainedState`, severity, start and observation time: the separately read latest finding row, including changed/flapping flags;
- `unavailable`: missing historical onset, configuration, operator state, flapping, check-definition history, cause event or latest row.

No retained resolution means unknown ending evidence. A separately recorded recurrence bounds the older occurrence's membership without inventing a resolution timestamp. Timestamps can be non-monotonic; raw transition ordering follows collection identity. A retained state is not proof of an uninterrupted condition. Check labels and remediation come from the current catalogue and are not historical configuration.

## Bounds and continuation

A request defaults to 50 occurrences and permits at most 100. JSON and protobuf responses each fit the existing 512 KiB page budget. An exact selection can request `includeTransitions`: at most 100 raw transitions, with `nextTransitionOffset` and `transitionPageToken`. A single oversized item fails explicitly rather than returning partial text. The application applies an 8 MiB occurrence-summary read guard; raw-history row reads are limited to 100 and checked after scanning. These are not hard native SQL-engine memory or execution bounds.

A continuation captures the fixed range, transition high-water mark, selectors, source identity, total and retained-membership signature. Tokens have a five-minute wall-clock lifetime, a 2,048-byte input cap, and bounded offsets. Retention changing matching identities requires a new traversal, including when the total stayed unchanged. Tokens are unsigned untrusted navigation data, not authorization or durable database snapshots. Closed-dataset tokens bind the source fingerprint so separate CLI invocations can reopen the same source. No cross-source continuation is allowed.

`GetStatusTimeline.findingReadEpoch` captures transition membership in the Timeline read transaction. Optional `readEpoch: "0"` means a known empty capture; it does not mean “use the newest history.” Later checks or recurrences cannot enter that selected capture. Latest finding metadata remains separately labeled and can change. Ordinary page reloads can encounter retained-history expiry; there is no recovery of deleted evidence.

## Browser and CLI evidence

The browser route `/findings/:id/occurrences/:firstEpoch` requires fixed `from` and `to` bounds and optionally accepts `readEpoch`. It has explicit raw-history pages, check/resource links, a separately labeled latest-finding link, and Copy as prompt. Both the page copy action and the occurrence copy action preserve an exact same-method `context.query`; raw-page copying also preserves its cursor and offset. Imported URL reads begin at the first raw page. Unknown or expired evidence does not fall back to the current finding.

Use `natslens investigate describe ListFindingEpisodes` for the generated request descriptor and the existing trusted profile or closed-dataset workflow. Context JSON remains untrusted data. The server read-only allowlist and descriptor checks remain enforced. UI scope equivalence for this new RPC is conservative; inspect `availability.uiLinkScope` rather than assuming an exact remote UI link.

The demo reconstructs existing seeded per-finding transitions in a stable synthetic reference frame. It does not invent entity contributors from shaded aggregate rows. Synthetic frame identities are not native collection IDs, and old frames disclose unavailable current-frame metadata. Complete overlapping-event contributors, richer inferred-window drill-down and universal Timeline/agent snapshot handoff remain separate work.

## Historical all-results navigation

`/findings/history?system=…&from=…&to=…&readEpoch=…` lists separate retained occurrences for a fixed range. Optional `check`, `type` and `key` select the same typed resource scope as the API. A stream includes its consumers. Invalid scope/bounds do not fall back to a broader read. Account navigation context alone is not an additional API filter; the page discloses that distinction. The route reuses 50-row occurrence pages and their existing source-bound continuation.

Both its page prompt and CLI URL import preserve the exact request and timestamp precision. The browser and remote client reject a mismatched requested `readEpoch`, including a known-empty capture returning rows. This is transition membership, not a full immutable snapshot. The legacy `/findings` projection remains available separately; it is not used as the Timeline lane's historical all-results destination.
