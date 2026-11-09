# Scoped operator refresh verification

Status: implemented first slice of board task `pZ0PaSRralS1vMaPUagPQ`; fresh review and integration pending. Base: accepted `5f5002a1`. Benefit: fewer unrelated query refetches and no waiting on an unrelated blocked series request after a successful operator write. This does not claim measured production latency.

## Implemented contract

Mute/unmute and acknowledgement completion use the system passed with the mutation, not the current route. Shared fleet summaries remain dependencies even though their key is not system-specific. Known raw observation/metadata and immutable captured-selection families are excluded; unknown same-system families are conservatively invalidated so future status views do not silently retain stale operator state. Fixed historical operator-dependent families are included. The existing frozen-list snapshot contract is unchanged: Refresh list starts a new capture.

Source audit retained bucket/stream/consumer/server summaries, findings, mutes, occurrence metadata, overview/shell, applications, Capacity and analogous status-bearing reads. Stopped-collector `lastData` calls ShellStatus and carries server state; Replay's strip calls StatusTimeline. Neither is a raw independent series. Native acknowledgement creation writes `finding.acknowledged` into the captured event table atomically, so that action additionally invalidates event feeds, server-event feeds and event-history coverage. Mute and release do not write that new event. Existing exact-event/selection membership does not acquire newly created IDs. Replay's story uses its existing explicit event-kind list, rather than current mute state.

Default mute/unmute completion still waits for affected active refetches. The existing acceptance `awaitRefetch:false` behavior and nonwaiting acknowledgement behavior are preserved. All options still wait for the write itself. Existing dialog-captured scope, pending/duplicate-action protection, error handling and revision/episode guards for optimistic acknowledgement updates are unchanged. This cache policy is proposed, not yet confirmed.

## Executed proof

- Initial runtime RED: two failures and one passing control in the new real-QueryClient fixture. The old global invalidation refetched unrelated queries and left a successful save waiting on a deliberately blocked series refresh.
- Minimal correction: the same tests pass; adjacent query-hook tests and TypeScript pass.
- Additional source audit found stopped-status/Replay-strip and acknowledgement-event dependencies. New runtime RED: two failures/six passing controls, followed by corrected GREEN.
- Final affected selection: **56 tests across six files** pass (operator invalidation, queries, MuteDialog, AcceptDialog, AckDialog, Acknowledgement). TypeScript and 46-capability feature audit pass. These use controlled request functions, not deployed services.
- Complete fix removal to `5f5002a1`: **five runtime failures / three passes**. Restored: **eight passes**. Dependency-only removal to `6bb0fa39`: **two runtime failures / six passes**. Exact production bytes are restored after each run. No compile error is counted as RED.
- Query-count evidence: the synthetic fixture mounts **73 active queries**. The old mute refresh makes **73 additional query invocations** (146 including initial reads); the correction makes **30** (103 including initial reads). This is a controlled cache-invocation measurement across seeded families, not a real-page network count, SQL count, throughput benchmark or universal percentage improvement.
- Controls cover inactive historical operator reads, another system's cache, raw observations, shared fleet summaries, unknown future same-system families, acknowledgement events, failed writes, and the explicitly nonwaiting acceptance option while its actual write is held.

Logs: `/private/tmp/nats-operator-{cache-red,cache-green,dependencies-red,dependencies-green,all-removed,dependencies-removed,final-restored}.log`; final types `/private/tmp/nats-operator-cache-types.log`. No full web/Go suite, browser, new build, install, private data, push or restart is claimed for this slice.

## Remaining original task acceptance

Subsequent commits implement request-local firing-findings reuse and a focused annotation endpoint; see [stream finding reuse](stream-findings-reuse-verification.md) and [focused window reads](focused-window-reads-verification.md). The 73-query receipt above remains the original fixture; the later fixture adds an independent annotation family. Fresh combined review and representative load/polling acceptance remain separate.
