package api

import (
	"math"
	"sort"
	"time"
)

// AcceptGrowthBucket is the width of the buckets the acceptance growth is
// read in: a whole fraction of a day (30 a day), so that the bucket grid
// falls on the same times of every day and week.
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

// AcceptGrowthWindow is the history WeeklyGrowth needs: 8 days.
const AcceptGrowthWindow = week + day

const (
	day  = 24 * time.Hour
	week = 7 * day
	// spanMargin widens each span read, so that the points at a span's
	// edge have neighbours to interpolate between.
	spanMargin = time.Hour
)

// Span is a time span (From, To].
type Span struct{ From, To time.Time }

// AcceptGrowthSpans are the spans WeeklyGrowth reads at t: the day before
// t and the day a week before that, each an hour wider. Nothing between
// them is read, so the review reads two days of data, not eight.
func AcceptGrowthSpans(t time.Time) []Span {
	return []Span{
		{From: t.Add(-week - day - spanMargin), To: t.Add(-week + spanMargin)},
		{From: t.Add(-day - spanMargin), To: t},
	}
}

// Growth is an entity's growth as an acceptance records and the review
// compares it.
type Growth struct {
	// PerDay is the week's growth per day: the median, over the points of
	// the day before t, of (v(p) - v(p - 7 days)) / 7.
	PerDay float64
	// LastDay is the change over the day before t, v(t) - v(t - 1 day);
	// nil when either end has no point to read it from.
	LastDay *float64
}

// WeeklyGrowth is the growth at t of time-ordered samples read over
// AcceptGrowthSpans(t) in buckets width wide, one sample a bucket. For
// every sample p in the day before t, the value a week earlier is
// interpolated between the two samples around it, never across a gap of
// more than three widths; the growth per day is the median of
// (v(p) - v(p - 7 days)) / 7. A pattern that repeats every day or every
// week (business hours, weekdays, a weekly cleanup) adds the same to each
// difference and cancels; noise is damped by the median over a day of
// points. A purge or a restore within the week changes the week's growth,
// as it changes the week's net bytes. Nil unless pairs cover at least two
// thirds of the buckets a full day has (day / width): an entity under
// about 8 days old, or a day mostly lost to a collector outage, has no
// rate rather than one that a few pairs decide.
func WeeklyGrowth(s []Sample, t time.Time, width time.Duration) *Growth {
	if len(s) < 2 || width <= 0 {
		return nil
	}
	maxGap := 3 * width
	at := func(x time.Time) (float64, bool) {
		j := sort.Search(len(s), func(i int) bool { return !s[i].T.Before(x) })
		switch {
		case j == len(s):
			return 0, false
		case s[j].T.Equal(x):
			return s[j].V, true
		case j > 0 && s[j].T.Sub(s[j-1].T) <= maxGap:
			p, q := s[j-1], s[j]
			f := float64(x.Sub(p.T)) / float64(q.T.Sub(p.T))
			return p.V + f*(q.V-p.V), true
		}
		return 0, false
	}
	var rates []float64
	for _, p := range s {
		if !p.T.After(t.Add(-day)) || p.T.After(t) {
			continue
		}
		if v, ok := at(p.T.Add(-week)); ok {
			rates = append(rates, (p.V-v)/7)
		}
	}
	// Pairs for two thirds of a full day's buckets, counted against the
	// buckets a day has, not against the samples that are there.
	if len(rates) == 0 || 3*float64(len(rates)) < 2*float64(day)/float64(width) {
		return nil
	}
	g := &Growth{PerDay: median(rates)}
	last := s[len(s)-1]
	if v, ok := at(last.T.Add(-day)); ok && !last.T.Before(t.Add(-maxGap)) {
		d := last.V - v
		g.LastDay = &d
	}
	return g
}

// GrowthChanged reports whether growth now differs from the recorded growth
// by more than half of the larger of the recorded rate's size and
// AcceptGrowthFloor.
func GrowthChanged(recorded, now float64) bool {
	return math.Abs(now-recorded) > 0.5*math.Max(math.Abs(recorded), AcceptGrowthFloor)
}

// GrowthEndReason is the end_reason of an acceptance ended because the
// week's growth per day changed from recorded to now.
func GrowthEndReason(recorded, now float64) string {
	return "growth changed from " + FmtGrowth(recorded) + " to " + FmtGrowth(now) + " over the last 7 days"
}

// EndReason is GrowthEndReason for g, with the last day's change when it
// is known, so that a change that began days ago and one that began today
// read apart.
func (g Growth) EndReason(recorded float64) string {
	out := GrowthEndReason(recorded, g.PerDay)
	if g.LastDay != nil {
		sign := "+"
		if *g.LastDay < 0 {
			sign = "-"
		}
		out += " (" + sign + FmtBytes(math.Abs(*g.LastDay)) + " in the last day)"
	}
	return out
}
