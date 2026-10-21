package api

import (
	"math"
	"sort"
	"time"
)

// AcceptGrowthWindow is the span before now whose growth an acceptance
// records, and before each epoch whose growth the review compares with it.
const AcceptGrowthWindow = 7 * 24 * time.Hour

// AcceptGrowthBucket is the width of the buckets the acceptance growth is
// read in: a whole fraction of a day (30 a day), so that the bucket grid
// falls on the same times of every day.
const AcceptGrowthBucket = 48 * time.Minute

// AcceptGrowthWidth is the bucket width both the acceptance and the review
// read growth points in: AcceptGrowthBucket, or the collect interval when
// that is longer.
func AcceptGrowthWidth(interval time.Duration) time.Duration {
	return max(AcceptGrowthBucket, interval)
}

// AcceptGrowthFloor is the least rate, bytes per day, the review measures a
// change against: an acceptance recorded at about +0 does not end on a few
// KiB of change.
const AcceptGrowthFloor = 1 << 20

// AcceptEndedBy is the unmuted_by of an acceptance nats-lens ended.
const AcceptEndedBy = "nats-lens"

const day = 24 * time.Hour

// DailyPerDay is the growth per day of time-ordered samples measured over
// whole days: for every sample and every k of 1 or more whole days for
// which the samples reach k days further, the change to the value k days
// later (interpolated between the two samples around that time) divided
// by k, and the median of those. A pattern that repeats every day (growth
// by day, none by night) adds the same amount to every such change, so
// the rate does not depend on the time of day it is read at; one step (a
// purge, a restore) moves only the changes that span it. Nil under one
// day of samples, or when no sample has a partner a whole number of days
// later. A partner is interpolated only between samples at most three
// times the median spacing apart, so a gap in collection is not bridged.
func DailyPerDay(s []Sample) *float64 {
	if len(s) < 2 || s[len(s)-1].T.Sub(s[0].T) < day {
		return nil
	}
	gaps := make([]float64, 0, len(s)-1)
	for i := 1; i < len(s); i++ {
		gaps = append(gaps, float64(s[i].T.Sub(s[i-1].T)))
	}
	maxGap := time.Duration(3 * median(gaps))
	last := s[len(s)-1].T
	var rates []float64
	for _, a := range s {
		for k := 1; !a.T.Add(time.Duration(k) * day).After(last); k++ {
			at := a.T.Add(time.Duration(k) * day)
			// The first sample at or after at; one exists, as at <= last.
			j := sort.Search(len(s), func(i int) bool { return !s[i].T.Before(at) })
			var v float64
			switch {
			case s[j].T.Equal(at):
				v = s[j].V
			case j > 0 && s[j].T.Sub(s[j-1].T) <= maxGap:
				p, q := s[j-1], s[j]
				f := float64(at.Sub(p.T)) / float64(q.T.Sub(p.T))
				v = p.V + f*(q.V-p.V)
			default:
				continue
			}
			rates = append(rates, (v-a.V)/float64(k))
		}
	}
	if len(rates) == 0 {
		return nil
	}
	m := median(rates)
	return &m
}

// GrowthChanged reports whether growth now differs from the recorded growth
// by more than half of the larger of the recorded rate's size and
// AcceptGrowthFloor.
func GrowthChanged(recorded, now float64) bool {
	return math.Abs(now-recorded) > 0.5*math.Max(math.Abs(recorded), AcceptGrowthFloor)
}

// GrowthEndReason is the end_reason of an acceptance ended because its
// growth changed; it names what was measured.
func GrowthEndReason(recorded, now float64) string {
	return "growth changed from " + FmtGrowth(recorded) + " to " + FmtGrowth(now) +
		" (day over day, over the last 7 days)"
}
