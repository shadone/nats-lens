package fake

import (
	"fmt"
	"math"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// globalTraffic is a gauge shaped by the daily curve, noise and the dip of
// the pool-us upgrade; the last quarter hour holds the current value.
func globalTraffic(key string, c time.Time, base, spread, dip, now float64) float64 {
	if !c.Before(fixed(14, 15, 0)) {
		return now
	}
	v := base*(1+wave(c)) + noise(key, c)*spread
	if !c.Before(globalUpgrade[0]) && c.Before(globalUpgrade[1]) {
		v *= dip
	}
	return math.Round(v)
}

// globalDownAt reports whether a server did not answer at c: from its stop
// on for nats-us-4, two minutes before each restart for the others.
func globalDownAt(sp serverSpec, c time.Time) bool {
	if !sp.answering() && !c.Before(sp.down) {
		return true
	}
	if sp.day != 0 {
		return false
	}
	start := fixed(sp.h, sp.m, sp.s)
	return !c.Before(start.Add(-2*time.Minute)) && c.Before(start)
}

// globalGrowth is how much a server's store grows per day.
func globalGrowth(sp serverSpec) float64 {
	if sp.name == "nats-ap-2" {
		return 1.1 * gib
	}
	return 0.25 * gib
}

// globalConns is the scenario's connection count now.
const globalConns = 1142.0

// globalSample returns a metric's value at fixed-frame time c.
func globalSample(sc *scenario, metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	days := FixedNow.Sub(c).Hours() / 24
	sp, _ := sc.server(e.GetKey())
	switch metric {
	case "cluster.servers_up":
		n := 0.0
		for _, x := range sc.servers {
			if !globalDownAt(x, c) {
				n++
			}
		}
		return n, true
	case "cluster.connections":
		return globalTraffic(metric, c, 1090, 50, 0.85, globalConns), true
	case "cluster.in_msgs_rate":
		return globalTraffic(metric, c, 17200, 1800, 0.8, 18400), true
	case "cluster.out_msgs_rate":
		return globalTraffic(metric, c, 38500, 3600, 0.78, 41200), true
	case "cluster.in_bytes_rate", "cluster.out_bytes_rate", "server.in_bytes_rate", "server.out_bytes_rate":
		// About 1.1 KiB per message.
		v, ok := globalSample(sc, strings.Replace(metric, "_bytes_", "_msgs_", 1), e, c)
		return math.Round(v * 1100), ok
	case "cluster.js_storage_bytes":
		var t float64
		for _, x := range sc.servers {
			t += float64(x.store) - days*globalGrowth(x)
		}
		return t, true
	case "cluster.durable_consumers":
		n := 0
		for _, st := range sc.streams {
			n += len(st.consumers)
		}
		return float64(n), true
	case "server.connections", "server.in_msgs_rate", "server.out_msgs_rate":
		if globalDownAt(sp, c) {
			return 0, false
		}
		v, _ := globalSample(sc, "cluster."+strings.TrimPrefix(metric, "server."), e, c)
		return math.Round(v * float64(sp.conns) / globalConns), true
	case "server.mem_bytes":
		return float64(sp.mem) + noise(metric+e.GetKey(), c)*12*mib, !globalDownAt(sp, c)
	case "server.cpu_percent":
		return math.Max(0.5, sp.cpu+noise(metric+e.GetKey(), c)*math.Max(2, sp.cpu*0.3)), !globalDownAt(sp, c)
	case "server.js_storage_bytes":
		return float64(sp.store) - days*globalGrowth(sp), !globalDownAt(sp, c)
	case "stream.bytes", "stream.messages":
		st, _ := sc.findStream(e.GetAccount(), e.GetStream())
		b := math.Max(st.bytes-days*st.perDay, st.bytes*0.05)
		if metric == "stream.bytes" {
			return b, true
		}
		return math.Round(float64(st.msgs) * b / st.bytes), true
	case "consumer.num_pending":
		if e.GetConsumer() == "label-printer" && !c.Before(fixed(13, 40, 0)) {
			return math.Round(40 + 6170*c.Sub(fixed(13, 40, 0)).Hours()/(50.0/60)), true
		}
		return math.Round(40 + noise(metric+e.GetKey(), c)*30), true
	case "consumer.num_ack_pending":
		if e.GetConsumer() == "label-printer" && !c.Before(fixed(13, 40, 0)) {
			return 1000, true
		}
		return math.Round(12 + noise(metric+e.GetKey(), c)*10), true
	case "consumer.num_redelivered":
		if e.GetConsumer() == "label-printer" && !c.Before(fixed(13, 40, 0)) {
			return 212, true
		}
		return 0, true
	case "consumer.delivered_rate":
		// label-printer drains 21/s since 13:40 while 38/s arrive.
		if e.GetConsumer() == "label-printer" && !c.Before(fixed(13, 40, 0)) {
			return math.Round((21+noise(metric+e.GetKey(), c))*10) / 10, true
		}
		if e.GetConsumer() == "label-printer" {
			return math.Round((38+noise(metric+e.GetKey(), c)*4)*10) / 10, true
		}
		return deliveredRate(e.GetKey(), c), true
	case "cluster.meta_leader_changes":
		// nats-eu-1 has led since the start of the data: the us restarts
		// did not touch the meta group's leader.
		return 0, true
	case "server.slow_consumers":
		// Every slow consumer disconnect was label-printer on nats-us-3.
		if sp.name != "nats-us-3" {
			return 0, !globalDownAt(sp, c)
		}
		return countIn(sc.slowEvents, c), !globalDownAt(sp, c)
	}
	return 0, false
}

// globalTiles are example-global's six numbers of the overview.
func globalTiles(s scen, r *v1.TimeRange, maxPoints int32) []*v1.Tile {
	sys := s.sc.systemRef()
	ser := func(metric string) *v1.Series { return s.series(metric, sys, r, maxPoints) }
	up, conns, slow := ser("cluster.servers_up"), ser("cluster.connections"), ser("cluster.slow_consumers")
	storage, in, out, durable := ser("cluster.js_storage_bytes"), ser("cluster.in_msgs_rate"), ser("cluster.out_msgs_rate"), ser("cluster.durable_consumers")
	recentSlow := s.series("cluster.slow_consumers", sys, &v1.TimeRange{Start: ts(r.GetEnd().AsTime().Add(-15 * time.Minute)), End: r.GetEnd()}, 1)
	limit := float64(len(s.sc.servers)) * 100 * gib
	cur := last(storage)
	used := 0.0
	if cur != nil {
		used = *cur / limit * 100
	}
	return []*v1.Tile{
		{Id: "servers", Label: "Servers", Value: last(up), Unit: v1.Unit_UNIT_COUNT, ValueSuffix: fmt.Sprintf("/ %d", len(s.sc.servers)),
			Subtitle: "2 versions in use", Status: v1.Status_STATUS_WARNING, Series: up},
		{Id: "connections", Label: "Connections", Value: last(conns), Unit: v1.Unit_UNIT_COUNT,
			Subtitle: "86 ephemeral consumers", Status: v1.Status_STATUS_OK, Series: conns},
		{Id: "slow_consumers", Label: "Slow consumer events", Value: ptr(sum(slow)), Unit: v1.Unit_UNIT_COUNT, ValueSuffix: spanLabel(r),
			Subtitle: fmt.Sprintf("%d in last 15m", int(sum(recentSlow))), Status: v1.Status_STATUS_WARNING, Series: slow},
		{Id: "js_storage", Label: "JetStream storage", Value: cur, Unit: v1.Unit_UNIT_BYTES, ValueSuffix: fmt.Sprintf("of %.0f GiB", limit/gib),
			Subtitle: fmt.Sprintf("%.1f%% used, fullest nats-ap-2 at 71%%", used), Status: v1.Status_STATUS_INFO, Series: storage, Limit: ptr(limit)},
		{Id: "msgs_rate", Label: "Messages / s", Value: last(in), Unit: v1.Unit_UNIT_PER_SECOND, ValueSuffix: "in",
			Value2: last(out), Value2Suffix: "out", Subtitle: "across 3 clusters", Status: v1.Status_STATUS_OK, Series: in, Series2: out},
		{Id: "consumers", Label: "Consumers", Value: last(durable), Unit: v1.Unit_UNIT_COUNT, ValueSuffix: "durable",
			Value2: ptr(86.0), Value2Suffix: "ephemeral", Subtitle: "1 backlog growing", Status: v1.Status_STATUS_WARNING, Series: durable},
	}
}
