package api

import "time"

// SetMetricsTimeout sets the per-system time of a /metrics scrape and
// returns a function that restores it.
func SetMetricsTimeout(d time.Duration) (restore func()) {
	old := metricsTimeout
	metricsTimeout = d
	return func() { metricsTimeout = old }
}
