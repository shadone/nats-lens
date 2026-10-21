package api

import (
	"math"
	"time"
)

// AcceptGrowthWindow is the span before now whose growth an acceptance
// records, and before each epoch whose growth the review compares with it.
const AcceptGrowthWindow = 24 * time.Hour

// AcceptGrowthFloor is the least rate, bytes per day, the review measures a
// change against: an acceptance recorded at about +0 does not end on a few
// KiB of change.
const AcceptGrowthFloor = 1 << 20

// AcceptEndedBy is the unmuted_by of an acceptance nats-lens ended.
const AcceptEndedBy = "nats-lens"

// GrowthChanged reports whether growth now differs from the recorded growth
// by more than half of the larger of the recorded rate's size and
// AcceptGrowthFloor.
func GrowthChanged(recorded, now float64) bool {
	return math.Abs(now-recorded) > 0.5*math.Max(math.Abs(recorded), AcceptGrowthFloor)
}

// GrowthEndReason is the end_reason of an acceptance ended because its
// growth changed.
func GrowthEndReason(recorded, now float64) string {
	return "growth changed from " + FmtGrowth(recorded) + " to " + FmtGrowth(now)
}
