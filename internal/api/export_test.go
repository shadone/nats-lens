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
