package api_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

func msHour(h int) time.Time { return microT0.Add(time.Duration(h) * time.Hour) }

func msCols(from time.Time, step time.Duration, ms ...float64) []api.MicroAverage {
	var out []api.MicroAverage
	for i, v := range ms {
		out = append(out, api.MicroAverage{At: from.Add(time.Duration(i) * step), Ms: v})
	}
	return out
}

func msEq(t *testing.T, name string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil || !got.Equal(want) {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

// Every hour before the epoch's slow: the first hour, whatever the epoch's
// hour shows so far.
func TestMicroSlowStartCaseEveryHourSlow(t *testing.T) {
	hours := []api.MicroAverage{{At: msHour(0), Ms: 40}, {At: msHour(1), Ms: 40}, {At: msHour(2), Ms: 4}}
	msEq(t, "epoch hour fast", api.MicroSlowStart(hours, nil, 4, msHour(2).Add(time.Minute)), msHour(0))
	hours[2].Ms = 40
	msEq(t, "epoch hour slow", api.MicroSlowStart(hours, nil, 4, msHour(2).Add(time.Minute)), msHour(0))
	// Only the epoch's hour.
	msEq(t, "only the epoch's hour", api.MicroSlowStart(hours[2:], nil, 4, msHour(2).Add(time.Minute)), msHour(2))
}

// Slow from exactly an hour boundary.
func TestMicroSlowStartCaseOnHourBoundary(t *testing.T) {
	hours := []api.MicroAverage{{At: msHour(0), Ms: 4}, {At: msHour(1), Ms: 40}, {At: msHour(2), Ms: 40}}
	var ms []float64
	for i := 0; i < 24; i++ {
		v := 4.0
		if i >= 12 {
			v = 40
		}
		ms = append(ms, v)
	}
	cols := msCols(msHour(0), 5*time.Minute, ms...)
	msEq(t, "boundary", api.MicroSlowStart(hours, cols, 4, msHour(2).Add(30*time.Minute)), msHour(1))
}

// A fast hour inside a slow stretch: the run is the one after it.
func TestMicroSlowStartCaseFastHourInsideSlow(t *testing.T) {
	hours := []api.MicroAverage{{At: msHour(0), Ms: 40}, {At: msHour(1), Ms: 4}, {At: msHour(2), Ms: 40}, {At: msHour(3), Ms: 40}}
	var ms []float64
	for i := 0; i < 24; i++ {
		v := 4.0
		if i >= 10 {
			v = 40
		}
		ms = append(ms, v)
	}
	cols := msCols(msHour(1), 5*time.Minute, ms...)
	msEq(t, "inside", api.MicroSlowStart(hours, cols, 4, msHour(3).Add(10*time.Minute)), msHour(1).Add(50*time.Minute))
}

// A level change as hours age out: a higher level makes an hour fast that
// was slow, so the run starts later.
func TestMicroSlowStartCaseLevelChange(t *testing.T) {
	hours := []api.MicroAverage{{At: msHour(0), Ms: 4}, {At: msHour(1), Ms: 20}, {At: msHour(2), Ms: 60}, {At: msHour(3), Ms: 60}}
	// Each hour's collections at the hour's average.
	var cols []api.MicroAverage
	for _, h := range hours {
		cols = append(cols, msCols(h.At, 5*time.Minute, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms, h.Ms)...)
	}
	msEq(t, "level 4", api.MicroSlowStart(hours, cols, 4, msHour(3).Add(time.Minute)), msHour(1))
	msEq(t, "level 10", api.MicroSlowStart(hours, cols, 10, msHour(3).Add(time.Minute)), msHour(2))
}

// A brief fast collection in the next hour: at 1:50 the run starts at
// 1:55; at 1:55, the span's last collection, the run starts after the
// span, at 2:00, never earlier.
func TestMicroSlowStartCaseBlipMovesStartBack(t *testing.T) {
	hours := []api.MicroAverage{{At: msHour(0), Ms: 4}, {At: msHour(1), Ms: 40}, {At: msHour(2), Ms: 40}}
	mk := func(blip int) []api.MicroAverage {
		var ms []float64
		for i := 0; i < 24; i++ {
			v := 4.0
			if i >= 12 && i != blip {
				v = 40
			}
			ms = append(ms, v)
		}
		return msCols(msHour(0), 5*time.Minute, ms...)
	}
	epoch := msHour(2).Add(30 * time.Minute)
	a := api.MicroSlowStart(hours, mk(22), 4, epoch)
	b := api.MicroSlowStart(hours, mk(23), 4, epoch)
	t.Logf("blip at 1:50 -> %v; blip at 1:55 -> %v", a, b)
	if b != nil && a != nil && b.Before(*a) {
		t.Errorf("a later blip moves the start back: %v then %v", a, b)
	}
	// At 1:55 the span (hours 0 and 1) ends fast: the run starts after it.
	msEq(t, "blip at 1:50", a, msHour(1).Add(55*time.Minute))
	msEq(t, "blip at 1:55", b, msHour(2))
}

// Over random hourly series with idle hours and collections consistent
// with their hours, a slow endpoint always has a start, within the hours
// and the epoch, after the newest fast hour before the epoch's.
func TestMicroSlowStartCaseProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	checked := 0
	for n := 0; n < 20000; n++ {
		level := 1 + rng.Float64()*20
		nh := 1 + rng.Intn(30)
		var hours, cols []api.MicroAverage
		pSlow := rng.Float64()
		for h := 0; h < nh; h++ {
			if h > 0 && h < nh-1 && rng.Float64() < 0.2 {
				continue // idle
			}
			k := 1 + rng.Intn(12)
			if h == nh-1 {
				k = 1 + rng.Intn(4)
			}
			hourSlow := rng.Float64() < pSlow
			sum := 0.0
			for c := 0; c < k; c++ {
				v := level * (0.5 + rng.Float64())
				if (hourSlow && rng.Float64() < 0.9) || (!hourSlow && rng.Float64() < 0.1) {
					v = level*3 + 5 + rng.Float64()*50
				}
				if rng.Float64() < 0.05 {
					v = level*3 + 5 // exactly at the rule's edge
				}
				cols = append(cols, api.MicroAverage{At: msHour(h).Add(time.Duration(c) * 5 * time.Minute), Ms: v})
				sum += v
			}
			hours = append(hours, api.MicroAverage{At: msHour(h), Ms: sum / float64(k)})
		}
		epoch := cols[len(cols)-1].At
		last := cols[len(cols)-1].Ms
		if !api.MicroIsSlow(&last, &level) {
			continue
		}
		checked++
		got := api.MicroSlowStart(hours, cols, level, epoch)
		if got == nil {
			t.Fatalf("case %d: slow without a start: hours %v cols %v", n, hours, cols)
		}
		if got.After(epoch) || got.Before(hours[0].At) {
			t.Fatalf("case %d: start %v outside [%v, %v]", n, got, hours[0].At, epoch)
		}
		if from, _, ok := api.MicroSlowSpan(hours, level, epoch); ok && !got.After(from) {
			t.Fatalf("case %d: start %v not after the fast hour %v", n, got, from)
		}
		// Every hour before the epoch's from the start's hour on is slow.
		eh := epoch.Truncate(time.Hour)
		for _, h := range hours {
			if h.At.After(*got) && h.At.Before(eh) && !api.MicroIsSlow(&h.Ms, &level) {
				t.Fatalf("case %d: fast hour %v at or after the start %v", n, h.At, got)
			}
		}
	}
	if checked < 1000 {
		t.Fatalf("checked %d", checked)
	}
	t.Logf("checked %d slow cases", checked)
}
