# Metric-route handoff correction

Status: implemented; fresh review and integration pending. Board task `bXMlS0kOLTauMBDTEL280` remains in progress for its broader acceptance. Base: accepted `0962fd1d`. This branch does not include the separately pending Timeline summary-label change.

## Contract

The terminal accepts the twenty-one shipped metric families plus the metric index. Navigation metadata identifies existing metric IDs; calculations remain in authoritative handlers. Selected server routes use server metrics, unselected ones use fleet metrics, and both traffic directions/asset groups remain separate. Stream and consumer selectors retain typed compound identity. For a consumer comparison scoped to a stream, its account outranks unrelated retained account navigation, and `ListMetricContributors` receives the unqualified stream name together with that account. Explicit request overrides and the existing read-only allowlist remain unchanged.

Browser contexts retain URL-backed page/ranking choices. They do not trigger another RPC, choose a new ranking metric automatically, or establish full-page equivalence. Unsupported choices remain in `unappliedContextFilters`; existing conservative disclosures can also list a server selector encoded in series identities. Effective links remain qualified partial projections. Returned exact machine requests survive serialization and reimport. Unselected resource contexts do not invent a resource-series aggregate: use a selected identity or the bounded contributor method. Unknown routes, malformed resource selectors and invalid context metadata fail validation. This navigation policy is proposed, not yet confirmed.

## Executed verification

- Initial pure Go route selection failed at runtime for all twenty-one routes and all three typed-selection examples, with `unsupported source UI path`; fixed selection passed.
- Copied ranking/page choices: runtime RED (one failed/one passed), restored GREEN.
- Consumer stream/account regression: pure Go runtime RED omitted the stream from the contributor request; UI runtime RED retained `OTHER` instead of `APP`. Both pass after correction.
- Four independent removals reproduce runtime failures: route admission, stream request scope, copied options and copied stream/account scope. Go uses disposable overlays; UI production bytes are restored in `finally`. Logs: `/private/tmp/nats-metric-route-proof/*-removed.log`. No compile failure is counted as RED.
- Restored focused Go context/command/adjacent request contracts pass under `TZ=Pacific/Honolulu` (0.688s). A socket-free controlled source exercises the real command dispatcher for URL and JSON imports, both traffic directions, fixed bounds, unapplied options and returned exact-request replay. This is not live-backend/browser acceptance.
- A source conformance test compares the explicit Go route inventory against all three shipped UI family definitions. Unknown families stay rejected rather than widening to arbitrary paths.
- Restored four UI/helper files: **33 passed**. TypeScript passes. Feature audit: **46 capability docs, clean**.
- Focused coverage: `metric_route.go` **46/48 statements, 95.8%**. The selected broader investigation-package profile was 62.5%; no package-wide threshold claim is made.
- Corrected metric-prefix non-UTC race selection and scoped `go vet ./internal/investigation` pass. An earlier anchored race selector matched only adjacent tests; it is not counted as metric race evidence.

The command harness initially assumed exactly two unapplied fields and compared formatted JSON bytes. Those assumptions were corrected to require the relevant disclosures and exact protobuf equality; their failures are not product RED evidence. No full package/repository suite, native browser/clipboard, new build, SQL/check-engine or performance claim is made for this branch. The independently running consolidated acceptance profile is frozen at `0962fd1d` and excludes these changes. Existing dependencies only; no installs, proto/migration/API-method changes, private services, push or restart.

Raw focused receipts: `/private/tmp/nats-metric-{route-red,route-green,context-green,route-command,route-restored,route-race,options-red,options-green,stream-red,stream-ui-red,ui-restored}.log`.
