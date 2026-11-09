# Request-local stream finding evidence

Status: implemented; fresh review and integration pending. Second slice of board task `pZ0PaSRralS1vMaPUagPQ`, following the unmerged operator-refresh slice. Benefit: remove the duplicate full firing-findings composition read from `GetStream`, and prevent stream summary status and its returned finding detail from straddling two finding updates.

## Behavior

The existing private `streamStatus` decorator returns the firing-finding set it already read. `GetStream` uses that set for its response rather than reading it again. Its checks/inventory behavior is unchanged. The benchmark-only caller discards the new return value; its fixture, assertions and timing policy are unchanged. The public API, SQL bodies, matching rules, migrations, dependencies and wire fields are unchanged.

This guarantee applies to the API stream summary and returned firing-findings list, including backing-stream diagnostics. Consumer summaries, server/config reads and independent page requests still have their existing read boundaries; this is not a whole-response or cross-RPC snapshot. The separately scoped minimal consumer-status read remains unchanged.

## Verification

- Initial tests compile and fail at runtime: actual cause-event lookup **two queries instead of one**, and summary Warning versus detail Critical after an intervening finding update. RED checkpoint `718cd0df`; GREEN `8a757c7b`.
- The final consistency fixture publishes an actual later `ApplyCheckRuns` transaction and transition after the first finding set is read. Old code returns Warning with Critical/Changed detail. Fixed code returns the original Warning finding set while the store independently confirms the later Critical/Changed record exists.
- Native current/historical, empty-findings and cancellation controls pass. Cancellation returns no partial response. Final Honolulu race run: **5.609s**, scoped selectors only.
- Complete production removal overlays also restore the benchmark caller so compilation succeeds: three runtime failures (current count, historical count, consistency); restored controls pass. The final atomic-publication probe independently fails with the same overlay, then passes restored.
- The measured query reduction is the cause-event lookup in this controlled fixture, **2 → 1**. It witnesses reuse of the duplicate full finding composition; it is not a measurement of all SQL statements, complete response latency, throughput or every detail-page request.
- Scoped `go vet ./internal/api/storeprov`, feature-doc audit (46 capabilities) and whitespace checks pass. One intermediate signature-adaptation compile failure was corrected before runtime GREEN and is not counted as RED evidence.

Receipts: `/private/tmp/nats-stream-reuse-{red,green,controls}.log` and `/private/tmp/nats-stream-reuse-proof/{removed.log,published-run-removed.log,restored.log,coverage.out}`. No full Go/check-engine/storeprov suite, frontend build/browser, network/private data, install, push or restart claim. This request-local reuse policy is proposed, not yet confirmed.

## Remaining original acceptance

Detail chart annotations still fetch full Overview; a focused cached read remains to implement. Representative real detail-load/polling network counts remain open. The existing operator-refresh slice is separately documented in [operator refresh verification](operator-refresh-verification.md). Neither slice closes the whole task.
