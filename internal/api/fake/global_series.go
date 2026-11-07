package fake

import (
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
	kill, ok := globalKilled[sp.name]
	if sp.day != 0 || !ok {
		return false
	}
	return !c.Before(kill) && c.Before(fixed(sp.h, sp.m, sp.s))
}

// globalKilled is when the upgrade killed the pods of nats-us-1 to
// nats-us-3, in the fixed frame: each server is down from then until its
// new process starts.
var globalKilled = map[string]time.Time{"nats-us-1": fixed(8, 44, 5), "nats-us-2": fixed(8, 56, 20), "nats-us-3": fixed(9, 4, 50)}

// globalGrowth is how much a server's store grows per day.
func globalGrowth(sp serverSpec) float64 {
	if sp.name == "nats-ap-2" {
		return 1.1 * gib
	}
	return 0.25 * gib
}

// globalConns is the scenario's connection count now: the clients' 1,142
// and its two extra connections in SYS.
const globalConns = 1144.0

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
			for _, v := range st.vanished {
				if c.Before(v.at) {
					n++
				}
			}
		}
		if c.Before(globalInvoiceCreated) {
			n--
		}
		return float64(n), true
	case "server.connections", "server.in_msgs_rate", "server.out_msgs_rate":
		if globalDownAt(sp, c) {
			return 0, false
		}
		v, _ := globalSample(sc, "cluster."+strings.TrimPrefix(metric, "server."), e, c)
		return math.Round(v * float64(sc.connsOn(sp, c)) / globalConns), true
	case "server.mem_bytes":
		return float64(sp.mem) + noise(metric+e.GetKey(), c)*12*mib, !globalDownAt(sp, c)
	case "server.cpu_percent":
		return math.Max(0.5, sp.cpu+noise(metric+e.GetKey(), c)*math.Max(2, sp.cpu*0.3)), !globalDownAt(sp, c)
	case "server.js_storage_bytes":
		return float64(sp.store) - days*globalGrowth(sp), !globalDownAt(sp, c)
	case "stream.bytes", "stream.messages":
		st, _ := sc.findStream(e.GetAccount(), e.GetStream())
		b := math.Max(st.bytes-days*st.perDay, st.bytes*0.05)
		if st.name == "KV_feature-flags" {
			b = globalFlagsBytes(st, c)
		}
		if metric == "stream.bytes" {
			return b, true
		}
		if st.bytes == 0 {
			return 0, true
		}
		return math.Round(float64(st.msgs) * b / st.bytes), true
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
	case "consumer.num_pending", "consumer.num_ack_pending", "consumer.num_redelivered", "consumer.delivered_rate":
		return globalConsumerSample(metric, e, c)
	}
	return 0, false
}

// globalConsumerSample is a consumer metric's value at fixed-frame time c.
func globalConsumerSample(metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	switch name := e.GetConsumer(); {
	case name == "invoice-renderer" && c.Before(globalInvoiceCreated):
		return 0, false
	case name == "invoice-renderer" && metric == "consumer.delivered_rate":
		// A rate needs two reports of the leader.
		return 0, !c.Before(globalInvoiceCreated.Add(sampleEvery))
	case name == "invoice-renderer":
		return 0, true
	case name == "reorder-planner" && metric == "consumer.delivered_rate":
		// Paused but for its run from 02:00 to 02:30 each night.
		if c.Hour() == 2 && c.Minute() < 30 {
			return deliveredRate(e.GetKey(), c), true
		}
		return 0, true
	}
	switch metric {
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
	}
	return 0, false
}
