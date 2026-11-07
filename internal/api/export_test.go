package api

import "time"

// SetMetricsTimeout sets the per-system time of a /metrics scrape and
// returns a function that restores it.
func SetMetricsTimeout(d time.Duration) (restore func()) {
	old := metricsTimeout
	metricsTimeout = d
	return func() { metricsTimeout = old }
}

// SetBatchTimeouts sets the time limits of a GetTimeSeriesBatch call and
// of each of its queries, and returns a function that restores them.
func SetBatchTimeouts(batch, query time.Duration) (restore func()) {
	oldB, oldQ := batchTimeout, batchQueryTimeout
	batchTimeout, batchQueryTimeout = batch, query
	return func() { batchTimeout, batchQueryTimeout = oldB, oldQ }
}

// SetBadgeClock sets the clock of the handler's badge expiry and the
// longest wait for a badge with none kept (zero keeps the default).
func SetBadgeClock(h *Handler, now func() time.Time, wait time.Duration) {
	h.badgeCache.now, h.badgeWait = now, wait
}
