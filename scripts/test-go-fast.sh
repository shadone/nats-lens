#!/bin/sh
# Curated policy/decoding tier: no NATS/HTTP listeners or DuckDB stores.
# Keep selectors anchored; docs/operations/go-test-tiers.md owns the inventory.
set -eu
cd "$(dirname "$0")/.."
go test -short -count=1 -timeout 2m "$@" ./internal/config ./internal/routehealth
go test -short -count=1 -timeout 2m "$@" ./internal/collect -run '^(TestAccountsToLookUp|TestAccountLookupOrderAndCap|TestMissingAndPartial|TestCompleteEpochIsNotPartial|TestExpectedIncludesPeers|TestTrackerForgets|TestApply.*|TestAccountRaftzMergesWithSystemRaftz|TestPeerNamesSkipNullEntries|TestMicroDecodeStats|TestMicroSubjectFromInfo|TestSubszDueAfterClockStepsBack)$'
go test -short -count=1 -timeout 2m "$@" ./cmd/natslens -run '^(TestMicroRefusalEvidence|TestRedialBackoffPolicy|TestMetricsTimeoutOptions|TestReadinessIgnoresCollection|TestCollectionStatusTransitions|TestStateDeadline|TestThreadsPerStore|TestWithin|TestArchiveGrace)$'
