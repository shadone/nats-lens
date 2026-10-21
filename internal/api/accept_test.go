package api

import (
	"testing"
	"time"
)

const mib = 1 << 20

var history = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// bucketed is what Store.GrowthPoints gives for a value read at epochs 30
// seconds apart from history on: the epochs in (end - window, end] in
// buckets of width on the grid from 1970, each bucket's middle time and
// median value.
func bucketed(val func(time.Time) float64, end time.Time, window, width time.Duration) []Sample {
	var out []Sample
	var ts []time.Time
	var vs []float64
	flush := func() {
		if len(ts) > 0 {
			out = append(out, Sample{T: ts[0].Add(ts[len(ts)-1].Sub(ts[0]) / 2), V: median(vs)})
		}
		ts, vs = nil, nil
	}
	from := end.Add(-window)
	at := from.Truncate(30 * time.Second)
	for at.Before(history) {
		at = at.Add(30 * time.Second)
	}
	for ; !at.After(end); at = at.Add(30 * time.Second) {
		if !at.After(from) {
			continue
		}
		if len(ts) > 0 && !at.Truncate(width).Equal(ts[0].Truncate(width)) {
			flush()
		}
		ts, vs = append(ts, at), append(vs, val(at))
	}
	flush()
	return out
}

// spaced is val at every step from end - window on, a grid that does not
// fall on the same times every day when step does not divide a day.
func spaced(val func(time.Time) float64, end time.Time, window, step time.Duration) []Sample {
	var out []Sample
	for at := end.Add(-window).Add(time.Minute); !at.After(end); at = at.Add(step) {
		if !at.Before(history) {
			out = append(out, Sample{T: at, V: val(at)})
		}
	}
	return out
}

type reader struct {
	name string
	rate func(val func(time.Time) float64, end time.Time) *float64
}

var readers = []reader{
	{"store buckets", func(val func(time.Time) float64, end time.Time) *float64 {
		return DailyPerDay(bucketed(val, end, AcceptGrowthWindow, AcceptGrowthBucket))
	}},
	{"7-minute points", func(val func(time.Time) float64, end time.Time) *float64 {
		return DailyPerDay(spaced(val, end, AcceptGrowthWindow, 7*time.Minute))
	}},
}

// A stream that grows only during business hours (09-17 UTC) at a
// constant rate, every day the same: its daily growth never changes, so
// no review ends an acceptance of it, whatever the hours of the
// acceptance and of the review.
func TestDiurnalSteadyStreamNotEnded(t *testing.T) {
	perBusinessHour := 3.0 * mib // 24 MiB/day, every day
	val := func(at time.Time) float64 {
		d := at.Sub(history)
		days := int(d / (24 * time.Hour))
		h := (d % (24 * time.Hour)).Hours()
		inDay := 0.0
		if h > 9 {
			inDay = (min(h, 17) - 9) * perBusinessHour
		}
		return float64(days)*8*perBusinessHour + inDay
	}
	base := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for _, r := range readers {
		for acc := 0; acc < 24; acc++ {
			rec := r.rate(val, base.Add(time.Duration(acc)*time.Hour))
			if rec == nil {
				t.Fatalf("%s: no rate at %02d:00", r.name, acc)
			}
			for rev := 0; rev < 24; rev++ {
				now := r.rate(val, base.Add(24*time.Hour+time.Duration(rev)*time.Hour))
				if GrowthChanged(*rec, *now) {
					t.Errorf("%s: accepted at %02d:00 (%.1f MiB/day), ended at review %02d:00 (%.1f MiB/day)",
						r.name, acc, *rec/mib, rev, *now/mib)
				}
			}
		}
	}
}

// A steady +10 MiB/day stream with one 200 MiB purge: the rate after the
// purge is unchanged, and no review in the day after it ends the
// acceptance.
func TestPurgeStepNotEnded(t *testing.T) {
	purge := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	val := func(at time.Time) float64 {
		v := at.Sub(history).Hours() / 24 * 10 * mib
		if !at.Before(purge) {
			v -= 200 * mib
		}
		return 1000*mib + v
	}
	for _, r := range readers {
		rec := r.rate(val, purge.Add(-time.Hour)) // accepted before the purge: +10 MiB/day
		for h := 1; h < 24; h += 2 {
			if now := r.rate(val, purge.Add(time.Duration(h)*time.Hour)); GrowthChanged(*rec, *now) {
				t.Errorf("%s: %dh after the purge: %.1f MiB/day ends an acceptance at %.1f", r.name, h, *now/mib, *rec/mib)
			}
		}
	}
}

// A stream whose growth goes from +10 to +16 MiB/day for good ends its
// acceptance once the new rate holds for most of the window, and not on
// its first day.
func TestLastingChangeEnds(t *testing.T) {
	change := time.Date(2026, 10, 10, 6, 0, 0, 0, time.UTC)
	val := func(at time.Time) float64 {
		d := at.Sub(history).Hours() / 24 * 10 * mib
		if at.After(change) {
			d += at.Sub(change).Hours() / 24 * 6 * mib
		}
		return d
	}
	for _, r := range readers {
		rec := r.rate(val, change)
		if rec == nil || GrowthChanged(10*mib, *rec) {
			t.Fatalf("%s: recorded %v, want +10 MiB/day", r.name, rec)
		}
		if now := r.rate(val, change.Add(24*time.Hour)); GrowthChanged(*rec, *now) {
			t.Errorf("%s: ended one day after the change (%.1f MiB/day)", r.name, *now/mib)
		}
		if now := r.rate(val, change.Add(5*24*time.Hour)); !GrowthChanged(*rec, *now) {
			t.Errorf("%s: kept five days after the change (%.1f MiB/day)", r.name, *now/mib)
		}
	}
}

// Under one day of samples there is no rate, so nothing is recorded or
// ended; a gap in collection is not bridged.
func TestDailyPerDayNeedsADay(t *testing.T) {
	steady := func(at time.Time) float64 { return at.Sub(history).Hours() * mib }
	if g := DailyPerDay(spaced(steady, history.Add(23*time.Hour), 23*time.Hour, 5*time.Minute)); g != nil {
		t.Errorf("23 hours: %v, want none", *g)
	}
	if g := DailyPerDay(spaced(steady, history.Add(25*time.Hour), 25*time.Hour, 5*time.Minute)); g == nil || *g != 24*mib {
		t.Errorf("25 hours: %v, want 24 MiB/day", g)
	}
	// Collection stopped from hour 2 to hour 26: the only partners a day
	// later fall in the gap.
	var s []Sample
	for _, x := range spaced(steady, history.Add(27*time.Hour), 27*time.Hour, 5*time.Minute) {
		if h := x.T.Sub(history).Hours(); h < 2 || h > 26 {
			s = append(s, x)
		}
	}
	if got := DailyPerDay(s); got != nil {
		t.Errorf("with every partner in a gap: %v, want none", *got)
	}
}

func TestGrowthEndReasonSaysWhatItMeasured(t *testing.T) {
	got := GrowthEndReason(2.1*mib, 4*mib)
	want := "growth changed from +2.1 MiB/day to +4.0 MiB/day (day over day, over the last 7 days)"
	if got != want {
		t.Errorf("reason = %q, want %q", got, want)
	}
}

// A KV bucket whose entries expire by TTL: it fills by 200 MiB during the
// day and empties at night, with no growth from one day to the next. Read
// at any hour its growth stays about zero, so a review never ends its
// acceptance on the 1 MiB floor.
func TestDailyFillAndExpiryNotEnded(t *testing.T) {
	val := func(at time.Time) float64 {
		h := (at.Sub(history) % (24 * time.Hour)).Hours()
		switch {
		case h < 8:
			return 50 * mib
		case h < 18:
			return 50*mib + (h-8)/10*200*mib
		default:
			return 250*mib - (h-18)/6*200*mib
		}
	}
	base := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for _, r := range readers {
		for acc := 0; acc < 24; acc++ {
			rec := r.rate(val, base.Add(time.Duration(acc)*time.Hour))
			for rev := 0; rev < 24; rev++ {
				now := r.rate(val, base.Add(24*time.Hour+time.Duration(rev)*time.Hour))
				if GrowthChanged(*rec, *now) {
					t.Errorf("%s: accepted at %02d:00 (%.2f MiB/day), ended at review %02d:00 (%.2f MiB/day)",
						r.name, acc, *rec/mib, rev, *now/mib)
				}
			}
		}
	}
}
