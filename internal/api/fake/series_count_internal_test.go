package fake

import (
	"math"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A count takes its bucket's last sample, as the store does, so it stays
// whole: servers up over the hour of nats-0's outage (09:16 to 09:18) is
// never 2.8.
func TestCountSeriesStayWhole(t *testing.T) {
	s := scen{now: FixedNow, sc: prodScenario()}
	r := &v1.TimeRange{Start: ts(fixed(9, 0, 0)), End: ts(fixed(10, 0, 0))}
	ser := s.series("cluster.servers_up", s.sc.systemRef(), r, 4)
	if len(ser.GetPoints()) == 0 {
		t.Fatal("no points")
	}
	for _, p := range ser.GetPoints() {
		if v := p.GetValue(); v != math.Trunc(v) {
			t.Errorf("servers up at %v = %v", p.GetTime().AsTime().Format(time.TimeOnly), v)
		}
	}
	if ser.GetMin() != math.Trunc(ser.GetMin()) {
		t.Errorf("min = %v", ser.GetMin())
	}
}
