package storeprov

import (
	"math"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestRobustPerDay(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	line := func(n int, step time.Duration, perHour float64) []sample {
		out := make([]sample, n)
		for i := range out {
			out[i] = sample{t: t0.Add(time.Duration(i) * step), v: 1e6 + perHour*float64(i)*step.Hours()}
		}
		return out
	}
	// Three minutes with one write: never extrapolated to a day.
	short := line(6, 30*time.Second, 0)
	short[5].v += 120000
	if got := robustPerDay(short); got != nil {
		t.Errorf("3 minutes of data gave %v, want unset", *got)
	}
	// Below six hours: unset.
	if got := robustPerDay(line(5*60, time.Minute, 1000)); got != nil {
		t.Errorf("5 hours gave %v, want unset", *got)
	}
	// Seven hours of steady growth, 1000 bytes an hour, with one large
	// write and one purge: the slope is the steady one.
	s := line(7*60, time.Minute, 1000)
	s[100].v += 5e6
	s[300].v -= 5e5
	got := robustPerDay(s)
	if got == nil || math.Abs(*got-24000) > 100 {
		t.Errorf("steady growth gave %v, want about 24000 a day", got)
	}
	// A bucket that shrinks under a TTL shrinks every hour: a real slope,
	// so negative growth is allowed.
	if got := robustPerDay(line(8*60, time.Minute, -500)); got == nil || math.Abs(*got+12000) > 100 {
		t.Errorf("shrinking bucket gave %v, want about -12000 a day", got)
	}
	// Flat with two blips stays at zero.
	flat := line(7*60, time.Minute, 0)
	flat[10].v += 1000
	if got := robustPerDay(flat); got == nil || *got != 0 {
		t.Errorf("flat gave %v, want 0", got)
	}
}

func TestStatusOfListsEachCodeOnce(t *testing.T) {
	stream := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "A/s", Account: "A", Stream: "s"}
	f := func(c string) *v1.Finding {
		return &v1.Finding{CheckCode: "consumer-003", Severity: v1.Severity_SEVERITY_WARNING,
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "A/s/" + c, Account: "A", Stream: "s", Consumer: c}}
	}
	_, codes := statusOf([]*v1.Finding{f("a"), f("b")}, stream)
	if len(codes) != 1 || codes[0] != "consumer-003" {
		t.Errorf("codes = %v, want [consumer-003]", codes)
	}
}
