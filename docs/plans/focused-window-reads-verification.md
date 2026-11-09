# Focused detail chart window reads

The redundant-read task adds `GetTimeWindows`, a read-only system/range RPC, and switches Stream, KV, Object Store and Consumer detail annotations to it. Native and demo providers reuse their existing window derivation. There is no new detector, migration, dependency or capture-completeness claim.

The proposed, not yet confirmed cache policy shares an exact system/range answer, retains fixed-range cache freshness, and polls live ranges every 30 seconds. There is no cross-range placeholder. One consumer-page observer supplies all three charts. Previously three hook observers already shared one network request; this is not a three-to-one network claim.

## Evidence

- Initial detail/invalidation regression selection: five runtime failures, seven passes. All four detail variants called Overview; raw annotations were incorrectly considered operator-dependent.
- Final focused selection: 23 passes across detail integration, annotation cache, generic query hooks and operator invalidation. Types pass.
- Removing both detail-page corrections reproduces four runtime failures; restored selection passes.
- Synthetic native provider fixture with one stream and a positive Kubernetes drain window: full Overview makes 28 counted provider reads; focused windows make 2. Window protobuf evidence is identical, including range and supporting identities. Cancellation performs zero reads and returns no partial response.
- The two focused reads are the existing event high-water mark and bounded event-input query. SQL engine work is not measured by this count.
- Read-only generated-handler HTTP fixture passes with nonempty demo windows equal to Overview. Existing Connect size/range/system controls and explicit read allowlist remain in force.
- Live/bare-source and canonical bundle parity fixtures now include this same RPC; no alternate parser or offline implementation exists. Focused non-UTC race selections pass in storeprov, API, offline and investigation packages (2.353s / 2.051s / 6.520s / 1.675s). Modified-package vet, command compilation and final types pass. The command compile check intentionally runs no tests.

The browser request contract changes one annotation RPC per detail load or live poll from `GetOverview` to `GetTimeWindows`. Primary detail and series reads remain separate. This is a source and controlled-hook/query-count statement, not a deployed network trace, benchmark or universal latency improvement.

The run logs were written to the temporary folder of the machine that ran them, outside the repository; they are not kept with this record. The first accidentally root-run web invocation lacked the web environment and is excluded; only the installed web runner's results are counted.

No full web/Go suite, production browser or private-data access is claimed. Combined fresh review remains required before integration.

## Polling and wire-contract follow-up

Four focused tests pass across annotation cache and Connect wire-adapter fixtures. Two live observers share one initial read and three subsequent reads over three simulated 30-second intervals; the fixed-range observer makes one read total. This is virtual-time request-function evidence, not elapsed production timing. Removing polling makes the new count assertion fail at runtime (one live read instead of four); exact restoration passes.

The real generated Connect client is exercised with a controlled no-network fetch implementation. It sends only the selected system/range to GetTimeWindows, and the returned view model retains event IDs above 2^53, derivation, supporting IDs, capture range and input-limit notice. The transport fixture initially assumed fetch received a Request object; correcting it to the actual URL/init signature was a fixture correction, not a production defect.

Their logs, like the ones above, stayed in the temporary folder of the machine that ran them.

## Measured detail-page requests

The reviewer of the merged branch counted the `/api` requests of the Stream and Consumer detail pages before and after the change. Each page sends the same number as before: 11 on load and 29 after 75 seconds with two polls. Each `GetOverview` is replaced one-for-one by `GetTimeWindows`. The saving is server work per call, not request count: in the provider fixture above, Overview makes 28 provider reads and `GetTimeWindows` makes 2.

## Review

The combined review ran on the merged branch `merge/page-reads`. A later round fixed its findings: the web test mocks and keys, generated-code drift, the consumer detail read (see [stream finding reuse](stream-findings-reuse-verification.md#consumer-detail)), a guard on query-key shape, and these records. No approval or merge is claimed.
