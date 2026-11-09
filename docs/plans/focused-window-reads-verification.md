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

Logs: `/private/tmp/nats-detail-windows-{red,green,removed,restored,types}.log`, `/private/tmp/nats-focused-timewindows-{native,race-parity}.log`. The first accidentally root-run web invocation lacked the web environment and is excluded; only the installed web runner's results are counted.

No full web/Go suite, production browser or private-data access is claimed. Combined fresh review remains required before integration.
