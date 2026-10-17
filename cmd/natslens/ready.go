package main

import (
	"sync/atomic"
	"time"
)

// readiness answers /readyz: ready once the store is open (any rebuild
// finished) and the API serves. It does not follow collection: a stopped
// collector shows in the API's Freshness.collecting and through
// nats_lens_last_epoch_age_seconds, and the UI must stay reachable then.
type readiness struct {
	now   func() time.Time
	since atomic.Int64 // UnixNano when the API started serving; 0 before
}

func newReadiness(now func() time.Time) *readiness {
	if now == nil {
		now = time.Now
	}
	return &readiness{now: now}
}

// serving records that the store is open and the API serves.
func (r *readiness) serving() { r.since.Store(r.now().UnixNano()) }

func (r *readiness) ready() bool { return r.since.Load() != 0 }
