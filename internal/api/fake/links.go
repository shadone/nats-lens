package fake

import (
	"fmt"
	"time"
)

// globalLinks is how example-global's servers connect: three pooled
// routes between any two servers of a cluster, nats-eu-3's at 4 ms where
// the others of eu and us take about 0.4 ms and those of ap about 4.6 ms; a gateway from every server to each other
// cluster, eu-us 84 ms, eu-ap 166 ms and us-ap 212 ms, nats-ap-1's to us
// reconnecting three times in the last hour; a route between nats-eu-3 and
// nats-eu-5 replaced three times today, twice in the last hour; and 30 warehouses and pickup
// points of the shop as leaf nodes on eu and us. pickup-0417 dropped 17
// times in the last day and has not connected since 13:52; pickup-0420
// connected for the first time in the newest collection. The upgrade of
// the us pool moved the gateways and leaf nodes connected to nats-us-1 to
// nats-us-4 to other servers.
func globalLinks() *linkSpec {
	l := &linkSpec{
		pool:       3,
		slowRoutes: map[string]time.Duration{"nats-eu-3": 4 * time.Millisecond},
		// ap's servers lie in zones further apart: its routes take about
		// 4.6 ms, all alike, so none is slow.
		routeBase: map[string]time.Duration{"ap": 4300 * time.Microsecond},
		gatewayRTT: map[[2]string]time.Duration{
			{"eu", "us"}: 84 * time.Millisecond,
			{"ap", "eu"}: 166 * time.Millisecond,
			{"ap", "us"}: 212 * time.Millisecond,
		},
		gatewayPending: map[string]int64{"nats-ap-1>us": 18 * kib},
		flaps:          map[string][]time.Time{"nats-ap-1>us": {fixed(13, 41, 10), fixed(13, 58, 40), fixed(14, 17, 20)}},
		routeFlaps:     map[[2]string][]time.Time{{"nats-eu-3", "nats-eu-5"}: {fixed(11, 4, 50), fixed(13, 47, 20), fixed(14, 12, 40)}},
		stopped:        globalStopped,
	}
	site := func(name, cluster string, hub int, rttMS float64, subs int64) siteSpec {
		return siteSpec{name: name, cluster: cluster, hub: hub, rtt: time.Duration(rttMS * float64(time.Millisecond)), subs: subs}
	}
	l.sites = []siteSpec{
		site("warehouse-eu-01", "eu", 0, 14.2, 160),
		site("warehouse-eu-02", "eu", 2, 17.8, 152),
		site("warehouse-eu-03", "eu", 5, 21.4, 148),
		site("warehouse-eu-04", "eu", 7, 15.9, 156),
	}
	// warehouse-eu-03 reconnected twice today.
	l.sites[2].reconnects = []time.Time{fixed(7, 12, 10), fixed(11, 48, 40)}
	for i := range 12 {
		l.sites = append(l.sites, site(fmt.Sprintf("pickup-%04d", 401+i), "eu", i%9, 24+float64(i*7%31), int64(40+i*5)))
	}
	flapping := site("pickup-0417", "eu", 1, 48.6, 44)
	flapping.outages = [][2]time.Time{
		{on16(16, 5), on16(16, 12)}, {on16(19, 40), on16(19, 41)}, {fixed(3, 10, 0), fixed(3, 25, 0)},
		{fixed(6, 30, 0), fixed(6, 31, 30)}, {fixed(10, 2, 0), fixed(10, 9, 0)}, {fixed(12, 15, 0), fixed(12, 40, 0)},
		{fixed(13, 52, 10), {}},
	}
	flapping.reconnects = []time.Time{on16(17, 20), on16(18, 44), on16(21, 3), on16(23, 58), fixed(1, 12, 0),
		fixed(4, 47, 0), fixed(8, 5, 0), fixed(11, 30, 0), fixed(13, 5, 0), fixed(13, 31, 0)}
	fresh := site("pickup-0420", "eu", 3, 31.2, 38)
	fresh.since = fixed(14, 29, 40)
	l.sites = append(l.sites, flapping, fresh,
		site("warehouse-us-01", "us", 0, 18.3, 158),
		site("warehouse-us-02", "us", 2, 22.7, 150),
		site("warehouse-us-03", "us", 6, 26.1, 154))
	for i, hub := range []int{1, 3, 4, 5, 7, 8, 0, 1, 4} {
		l.sites = append(l.sites, site(fmt.Sprintf("pickup-%04d", 1201+i), "us", hub, 29+float64(i*11%41), int64(42+i*4)))
	}
	return l
}

// globalStopped is when an example-global server first stopped answering
// on the scenario's day: nats-us-1 to nats-us-3 when the upgrade killed
// their pods, nats-us-4 when it went down.
func globalStopped(name string) (time.Time, bool) {
	if t, ok := globalKilled[name]; ok {
		return t, true
	}
	if name == "nats-us-4" {
		return globalDown, true
	}
	return time.Time{}, false
}

// prodLinks gives example-prod three pooled routes between its servers
// and one of their own for the APP account.
func prodLinks() *linkSpec { return &linkSpec{pool: 3, pinned: []string{Account}} }

// stagingLinks gives example-staging three pooled routes.
func stagingLinks() *linkSpec { return &linkSpec{pool: 3} }
