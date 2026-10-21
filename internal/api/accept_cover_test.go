package api

import (
	"testing"
	"time"
)

// coverDay is a week of samples one bucket apart before the last day, then
// the last day's samples at the times last gives, on a line of 1 MiB/day.
func coverDay(t time.Time, last []time.Time) []Sample {
	w := AcceptGrowthBucket
	val := func(at time.Time) float64 { return float64(at.Sub(t.Add(-30*day)).Hours()) / 24 * mib }
	var s []Sample
	for at := t.Add(-week - day - 2*w); !at.After(t.Add(-week + w)); at = at.Add(w) {
		s = append(s, Sample{T: at, V: val(at)})
	}
	for _, at := range last {
		s = append(s, Sample{T: at, V: val(at)})
	}
	return s
}

// pairs is the last day as pairs of samples a minute apart, each pair gap
// bucket widths after the previous one.
func pairs(t time.Time, gap float64) []time.Time {
	var out []time.Time
	step := time.Duration(gap*float64(AcceptGrowthBucket)) + time.Minute
	for at := t.Add(-day + time.Minute); !at.After(t); at = at.Add(step) {
		out = append(out, at)
		if b := at.Add(time.Minute); !b.After(t) {
			out = append(out, b)
		}
	}
	return out
}

// Pairs of samples enough for the count, 2.55 bucket widths apart, cover
// the day; 2.65 apart they cover nothing, so the stretch that counts as
// covered is 2.6 widths (coverStretch) to within a twentieth.
func TestCoverStretchIsTwoPointSixWidths(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	if g := WeeklyGrowth(coverDay(at, pairs(at, 2.55)), at, AcceptGrowthBucket); g == nil {
		t.Errorf("pairs 2.55 widths apart: no rate, want one")
	}
	if g := WeeklyGrowth(coverDay(at, pairs(at, 2.65)), at, AcceptGrowthBucket); g != nil {
		t.Errorf("pairs 2.65 widths apart: rate %v, want none", g.PerDay)
	}
}

// Single samples 2.5 widths apart cover the whole day, but number 12 where
// a full day has 30 buckets: only the count floor (half the buckets) holds
// them back.
func TestCountFloorRefusesACoveredDayOfFewSamples(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	var last []time.Time
	for x := at; x.After(at.Add(-day)); x = x.Add(-time.Duration(2.5 * float64(AcceptGrowthBucket))) {
		last = append(last, x)
	}
	for i, j := 0, len(last)-1; i < j; i, j = i+1, j-1 {
		last[i], last[j] = last[j], last[i]
	}
	if g := WeeklyGrowth(coverDay(at, last), at, AcceptGrowthBucket); g != nil {
		t.Errorf("%d samples covering the day: rate %v, want none", len(last), g.PerDay)
	}
}
