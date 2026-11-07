package fake

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// An application's hourly traffic is what its connections' counters grew
// by over the hour, for an hour in which no server restarted: the
// Applications page's sparkline and the Connections page agree.
func TestDemoTrafficAgreesWithCounters(t *testing.T) {
	for name, sc := range map[string]*scenario{Cluster: prodScenario(), GlobalSystem: globalScenario()} {
		s := newProvider(sc, false).scen()
		from, to := s.at(12, 0, 0), s.at(13, 0, 0)
		grew := map[string]int64{}
		n := map[string]int64{}
		for _, c := range s.connections(from) {
			grew[api.ApplicationOf(c.GetName())] -= c.GetInMsgs()
		}
		for _, c := range s.connections(to) {
			grew[api.ApplicationOf(c.GetName())] += c.GetInMsgs()
			n[api.ApplicationOf(c.GetName())]++
		}
		hour := map[string]int64{}
		for _, f := range s.appTraffic(to.Add(30 * time.Minute)) {
			if f.Hour.Equal(from) {
				hour[f.App] += f.InMsgs
			}
		}
		if len(hour) != len(grew) {
			t.Errorf("%s: traffic of %d applications, %d with connections", name, len(hour), len(grew))
		}
		for app, g := range grew {
			// Each connection's counter is rounded on its own.
			if d := g - hour[app]; d > n[app] || d < -n[app] || g == 0 {
				t.Errorf("%s %s: counters grew %d, the hour holds %d", name, app, g, hour[app])
			}
		}
	}
}

// An hour a server spent partly down holds less than the hour's level:
// its connections moved nothing while it was down.
func TestDemoTrafficDipsWhileAServerIsDown(t *testing.T) {
	sc := prodScenario()
	s := newProvider(sc, false).scen()
	h9 := s.at(9, 0, 0)
	// nats-0 was down from 09:16:02 to 09:18:00: about 2 minutes of 60.
	if r := s.upBetween(sc.servers[0], h9, h9.Add(time.Hour)) / s.trafficBetween(h9, h9.Add(time.Hour)); r > 0.98 || r < 0.96 {
		t.Errorf("09:00 holds %.3f of its level", r)
	}
	if r := s.upBetween(sc.servers[0], h9.Add(time.Hour), h9.Add(2*time.Hour)) / s.trafficBetween(h9.Add(time.Hour), h9.Add(2*time.Hour)); r < 0.9999 {
		t.Errorf("10:00 holds %.4f of its level", r)
	}
}
