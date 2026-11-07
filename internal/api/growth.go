package api

import (
	"slices"
	"time"
)

// MinGrowthSpan is the shortest span of observations a growth per day is
// computed from. Shorter spans give a slope that one write or one purge
// decides, extrapolated to a day.
const MinGrowthSpan = 6 * time.Hour

// GrowthStart bounds the growth-only lookback. The selected range must
// itself be at least six hours; shorter ranges never borrow old data.
// One collection interval (at most five minutes) lets a half-open range
// include the observation just before its start. No future endpoint is read.
func GrowthStart(start, end time.Time, interval time.Duration) time.Time {
	if end.Sub(start) < MinGrowthSpan || interval <= 0 {
		return start
	}
	return start.Add(-min(interval, 5*time.Minute))
}

// GrowthBuckets is the most points the slope is computed over; longer
// series are reduced to bucket medians first.
const GrowthBuckets = 200

// Sample is one observation of a growing value.
type Sample struct {
	T time.Time
	V float64
	// From and To retain the actual observation bounds when T is a
	// bucket midpoint. Zero means an unreduced sample at T.
	From, To time.Time
}

// RobustPerDay is the Theil-Sen slope of time-ordered samples (the median
// of the slopes between every pair), in units per day, or nil when the
// samples span less than MinGrowthSpan. One large write or purge moves the
// median little, so a single step is not taken for a trend. The slope may
// be negative: a bucket whose entries expire by TTL faster than they are
// written shrinks for real. Both providers compute stream, bucket and
// storage growth with it.
func RobustPerDay(s []Sample) *float64 {
	if len(s) < 2 {
		return nil
	}
	from, to := s[0].From, s[len(s)-1].To
	if from.IsZero() {
		from = s[0].T
	}
	if to.IsZero() {
		to = s[len(s)-1].T
	}
	if to.Sub(from) < MinGrowthSpan {
		return nil
	}
	s = bucketMedians(s, GrowthBuckets)
	slopes := make([]float64, 0, len(s)*(len(s)-1)/2)
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if dt := s[j].T.Sub(s[i].T).Hours() / 24; dt > 0 {
				slopes = append(slopes, (s[j].V-s[i].V)/dt)
			}
		}
	}
	if len(slopes) == 0 {
		return nil
	}
	m := median(slopes)
	return &m
}

// bucketMedians reduces time-ordered samples to at most n, each the
// median time and value of an equal share of the span.
func bucketMedians(s []Sample, n int) []Sample {
	if len(s) <= n {
		return s
	}
	first, width := s[0].T, s[len(s)-1].T.Sub(s[0].T)/time.Duration(n)
	var out []Sample
	for i := 0; i < len(s); {
		b := min(n-1, int(s[i].T.Sub(first)/max(width, 1)))
		j := i
		var ts []time.Time
		var vs []float64
		for ; j < len(s) && min(n-1, int(s[j].T.Sub(first)/max(width, 1))) == b; j++ {
			ts, vs = append(ts, s[j].T), append(vs, s[j].V)
		}
		out = append(out, Sample{T: ts[len(ts)/2], V: median(vs)})
		i = j
	}
	return out
}

func median(xs []float64) float64 {
	c := slices.Clone(xs)
	slices.Sort(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}
