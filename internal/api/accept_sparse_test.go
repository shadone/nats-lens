package api

import (
	"math"
	"testing"
	"time"
)

// A collector that keeps one bucket in k over the last day (crash-looping,
// starved) spreads its few points over the whole day. One in 2 still
// gives a rate that the cycle does not move; one in 3 gives none.
func TestSparseSpreadDayDoesNotEndOnACycle(t *testing.T) {
	val := func(at time.Time) float64 {
		h := at.Sub(history).Hours()
		return 1000*mib + h/24*10*mib + 60*mib*math.Sin(2*math.Pi*h/5)
	}
	s := newSeries(val)
	acc := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	rec := WeeklyGrowth(s.points(acc, everyPoint), acc, AcceptGrowthBucket)
	if rec == nil {
		t.Fatal("no rate at acceptance")
	}
	for _, k := range []int{2, 3} {
		ended, rated, minN := 0, 0, 1000
		for h := 30; h < 30+5*24; h++ {
			at := acc.Add(time.Duration(h) * time.Hour)
			keep := func(x time.Time) bool {
				if !x.After(at.Add(-day)) {
					return true
				}
				return (x.UnixNano()/int64(AcceptGrowthBucket))%int64(k) == 0
			}
			pts := s.points(at, keep)
			n := 0
			for _, p := range pts {
				if p.T.After(at.Add(-day)) {
					n++
				}
			}
			g := WeeklyGrowth(pts, at, AcceptGrowthBucket)
			if g != nil {
				rated++
				minN = min(minN, n)
				if GrowthChanged(rec.PerDay, g.PerDay) {
					if ended == 0 {
						t.Logf("k=%d +%dh, %d points: %s", k, h, n, g.EndReason(rec.PerDay))
					}
					ended++
				}
			}
		}
		t.Logf("one bucket in %d: %d of 120 give a rate (fewest points %d), %d end the acceptance", k, rated, minN, ended)
		if ended > 0 {
			t.Errorf("one bucket in %d: %d of 120 reviews end on the cycle", k, ended)
		}
	}
}

// At a 2-hour collect interval, three points 6 hours apart in the last day
// cover none of it and are too few: no rate.
func TestThreePointsInADayGiveNoRate(t *testing.T) {
	w := 2 * time.Hour
	at := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	val := func(x time.Time) float64 { return x.Sub(history).Hours() / 24 * 10 * mib }
	var s []Sample
	for x := at.Add(-week - day - time.Hour).Truncate(w); !x.After(at.Add(-week + time.Hour)); x = x.Add(w) {
		s = append(s, Sample{T: x, V: val(x)})
	}
	for _, x := range []time.Time{at.Add(-18 * time.Hour), at.Add(-12 * time.Hour), at.Add(-6 * time.Hour)} {
		s = append(s, Sample{T: x, V: val(x)})
	}
	if g := WeeklyGrowth(s, at, w); g != nil {
		t.Errorf("3 points in the day give a rate %.2f MiB/day", g.PerDay/mib)
	}
}

// A stretch between two paired points counts as covered up to
// coverStretch widths, and not 1ns more. The points come in close pairs so
// that their count stays above the floor.
func TestCoveredStretchLimit(t *testing.T) {
	w := AcceptGrowthBucket
	at := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	limit := time.Duration(coverStretch * float64(w))
	day0 := func(gap time.Duration) []Sample {
		var s []Sample
		for x := at.Add(-week - day - time.Hour); !x.After(at.Add(-week + time.Hour)); x = x.Add(w / 4) {
			s = append(s, Sample{T: x, V: 0})
		}
		for x := at.Add(-day + gap); !x.After(at); x = x.Add(gap + w/10) {
			s = append(s, Sample{T: x, V: 7 * mib})
			if y := x.Add(w / 10); !y.After(at) {
				s = append(s, Sample{T: y, V: 7 * mib})
			}
		}
		return s
	}
	if WeeklyGrowth(day0(limit), at, w) == nil {
		t.Error("stretches of exactly the limit: no rate")
	}
	if g := WeeklyGrowth(day0(limit+time.Nanosecond), at, w); g != nil {
		t.Error("stretches of the limit + 1ns: a rate")
	}
}

// A 2-hour interval on the 5-hour cycle: a full day gives a rate that
// stays put; a day with one epoch in three (4 points, 6 hours apart) gives
// none.
func TestSparseDayAtTwoHoursDoesNotEndOnACycle(t *testing.T) {
	w := 2 * time.Hour
	val := func(x time.Time) float64 {
		h := x.Sub(history).Hours()
		return 1000*mib + h/24*10*mib + 60*mib*math.Sin(2*math.Pi*h/5)
	}
	read := func(t0 time.Time, k int64) (*Growth, int) {
		var s []Sample
		n := 0
		for _, sp := range AcceptGrowthSpans(t0) {
			for x := sp.From.Truncate(w); !x.After(sp.To); x = x.Add(w) {
				if !x.After(sp.From) {
					continue
				}
				if x.After(t0.Add(-day)) && (x.UnixNano()/int64(w))%k != 0 {
					continue
				}
				if x.After(t0.Add(-day)) {
					n++
				}
				s = append(s, Sample{T: x, V: val(x)})
			}
		}
		return WeeklyGrowth(s, t0, w), n
	}
	acc := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	rec, _ := read(acc, 1)
	if rec == nil {
		t.Fatal("no rate")
	}
	for _, k := range []int64{1, 3} {
		ended, rated := 0, 0
		for h := 1; h < 1+7*24; h++ {
			g, n := read(acc.Add(time.Duration(h)*time.Hour), k)
			if g != nil {
				rated++
				if GrowthChanged(rec.PerDay, g.PerDay) {
					if ended == 0 {
						t.Logf("k=%d +%dh %d points: %s", k, h, n, g.EndReason(rec.PerDay))
					}
					ended++
				}
			}
		}
		t.Logf("2h interval, one epoch in %d: %d of 168 rated, %d end", k, rated, ended)
		if k > 1 && ended > 0 {
			t.Errorf("2h interval, one epoch in %d: %d of 168 reviews end on the cycle", k, ended)
		}
	}
}
