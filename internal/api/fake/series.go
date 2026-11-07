package fake

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// sampleEvery is the spacing of the samples averaged into one bucket; it
// matches the collector's default interval.
const sampleEvery = 30 * time.Second

// fixed returns hh:mm:ss on 2026-10-17 in the fixed frame.
func fixed(h, m, s int) time.Time { return time.Date(2026, 10, 17, h, m, s, 0, time.UTC) }

// noise is a deterministic value in [-0.5, 0.5) for a key and a quarter
// hour, so that every range and step draws the same curve.
func noise(key string, c time.Time) float64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s/%d", key, c.Unix()/900)
	return float64(h.Sum64()%10000)/10000 - 0.5
}

// wave is the daily traffic curve, between -0.18 and 0.18.
func wave(c time.Time) float64 {
	frac := c.Sub(FixedNow.Add(-24*time.Hour)).Hours() / 24
	return math.Sin(frac*2*math.Pi-1.2) * 0.18
}

func inUpgrade(c time.Time) bool { return !c.Before(fixed(9, 14, 0)) && c.Before(fixed(9, 50, 0)) }

// down reports whether a server process was stopped at c.
func down(server string, c time.Time) bool {
	spans := map[string][2]time.Time{
		"nats-0": {fixed(9, 16, 2), fixed(9, 18, 0)},
		"nats-1": {fixed(9, 29, 5), fixed(9, 31, 2)},
		"nats-2": {fixed(9, 41, 10), fixed(9, 44, 21)},
	}
	sp, ok := spans[server]
	return ok && !c.Before(sp[0]) && c.Before(sp[1])
}

// prodSlowConsumerEvents are the times of example-prod's slow consumer
// disconnects.
var prodSlowConsumerEvents = []time.Time{
	fixed(9, 16, 40), fixed(9, 17, 5), fixed(9, 17, 30), fixed(9, 18, 10), fixed(9, 31, 20), fixed(9, 32, 0),
	time.Date(2026, 10, 16, 22, 21, 0, 0, time.UTC),
}

// prodSlowServers are the servers of prodSlowConsumerEvents, in order:
// clients moved off each server the upgrade restarted.
var prodSlowServers = []string{"nats-1", "nats-2", "nats-1", "nats-2", "nats-0", "nats-2", "nats-0"}

// prodMetaLeaderChanges are when the upgrade moved example-prod's meta
// leader: to nats-1 when nats-0 stopped, back to nats-0 when nats-1 did.
var prodMetaLeaderChanges = []time.Time{fixed(9, 16, 5), fixed(9, 29, 8)}

// slowOn counts the slow consumer events on one server in the sample at c.
func slowOn(events []time.Time, servers []string, server string, c time.Time) float64 {
	var on []time.Time
	for i, t := range events {
		if servers[i] == server {
			on = append(on, t)
		}
	}
	return countIn(on, c)
}

// deliveredRate is a consumer's steady delivery rate: a base between 3
// and 60 messages per second from its key, shaped by the daily curve.
func deliveredRate(key string, c time.Time) float64 {
	h := fnv.New64a()
	fmt.Fprint(h, key)
	base := 3 + float64(h.Sum64()%5700)/100
	return math.Round(base*(1+wave(c)+noise("delivered"+key, c)*0.1)*10) / 10
}

// traffic is a gauge shaped by the daily curve, noise and the upgrade dip;
// the last quarter hour holds the scenario's current value.
func traffic(key string, c time.Time, base, spread, dip, now float64) float64 {
	if !c.Before(fixed(14, 15, 0)) {
		return now
	}
	v := base*(1+wave(c)) + noise(key, c)*spread
	if inUpgrade(c) {
		v *= dip
	}
	return math.Round(v)
}

func serversUp(sc *scenario, c time.Time) float64 {
	n := 0.0
	for _, sp := range sc.servers {
		if !down(sp.name, c) {
			n++
		}
	}
	return n
}

// prodSample returns a metric's value at fixed-frame time c; ok is false
// where there is no data.
func prodSample(sc *scenario, metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	days := FixedNow.Sub(c).Hours() / 24
	switch metric {
	case "cluster.servers_up":
		return serversUp(sc, c), true
	case "cluster.connections":
		return traffic(metric, c, 168, 14, 0.72, float64(sc.totalConns(c))), true
	case "cluster.in_msgs_rate":
		return traffic(metric, c, 1180, 260, 0.55, 1240), true
	case "cluster.out_msgs_rate":
		return traffic(metric, c, 3700, 700, 0.5, 3910), true
	case "cluster.in_bytes_rate", "cluster.out_bytes_rate", "server.in_bytes_rate", "server.out_bytes_rate":
		// About 1.2 KiB per message.
		v, ok := prodSample(sc, strings.Replace(metric, "_bytes_", "_msgs_", 1), e, c)
		return math.Round(v * 1230), ok
	case "cluster.js_storage_bytes":
		return 5.81*gib - days*51*mib, true
	case "cluster.durable_consumers":
		if c.Before(fixed(9, 31, 7)) {
			return 14, true
		}
		return 13, true
	case "server.connections", "server.in_msgs_rate", "server.out_msgs_rate":
		if down(e.GetKey(), c) {
			return 0, false
		}
		v, _ := prodSample(sc, "cluster."+strings.TrimPrefix(metric, "server."), e, c)
		if sp, ok := sc.server(e.GetKey()); ok && metric == "server.connections" {
			// Each server's share of the connections.
			return math.Round(v * float64(sc.connsOn(sp, c)) / float64(sc.totalConns(c))), true
		}
		return math.Round(v / 3), true
	case "server.mem_bytes":
		return 412*mib + noise(metric+e.GetKey(), c)*40*mib, !down(e.GetKey(), c)
	case "server.cpu_percent":
		return 7.5 + noise(metric+e.GetKey(), c)*4, !down(e.GetKey(), c)
	case "server.js_storage_bytes":
		return 1980*mib - days*17*mib, !down(e.GetKey(), c)
	case "stream.bytes", "stream.messages":
		sp, _ := sc.findStream(e.GetAccount(), e.GetStream())
		b := math.Max(sp.bytes-days*sp.perDay, sp.bytes*0.05)
		if metric == "stream.bytes" {
			return b, true
		}
		if sp.bytes == 0 {
			return 0, true
		}
		return math.Round(float64(sp.msgs) * b / sp.bytes), true
	case "consumer.num_pending", "consumer.num_ack_pending", "consumer.num_redelivered", "consumer.delivered_rate":
		return consumerSample(metric, e, c)
	case "cluster.meta_leader_changes":
		return countIn(prodMetaLeaderChanges, c), true
	case "server.slow_consumers":
		return slowOn(sc.slowEvents, prodSlowServers, e.GetKey(), c), !down(e.GetKey(), c)
	}
	return 0, false
}

func consumerSample(metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	if e.GetConsumer() == "analytics-aggregator" && !c.Before(fixed(9, 31, 7)) {
		return 0, false
	}
	video := e.GetConsumer() == "media-chunk-pool"
	switch {
	case metric == "consumer.delivered_rate" && video && !c.Before(fixed(11, 0, 0)):
		// The pool drains slower than the windows arrive.
		return math.Round(deliveredRate(e.GetKey(), c)*0.6*10) / 10, true
	case metric == "consumer.delivered_rate":
		return deliveredRate(e.GetKey(), c), true
	case metric == "consumer.num_pending" && video && !c.Before(fixed(11, 0, 0)):
		return math.Round(8820 + 9600*c.Sub(fixed(11, 0, 0)).Hours()/3.5), true
	case metric == "consumer.num_pending" && video:
		return math.Round(8820 + noise(metric, c)*600), true
	case metric == "consumer.num_pending":
		return math.Round(40 + noise(metric+e.GetKey(), c)*30), true
	case metric == "consumer.num_ack_pending":
		return math.Round(12 + noise(metric+e.GetKey(), c)*10), true
	case video:
		return 37, true
	}
	return 0, true
}

// series draws a metric over the range: the last sample of each bucket for
// counts, with the bucket's lowest and highest sample, averages of samples for other gauges and rates, sums of events
// for cluster.slow_consumers and the event metrics.
func (s scen) series(metric string, e *v1.EntityRef, r *v1.TimeRange, maxPoints int32) *v1.Series {
	if _, ok := bandOf[metric]; ok {
		return s.bandSeries(metric, e, r, maxPoints)
	}
	step := api.Step(r, maxPoints, sampleEvery)
	out := &v1.Series{Metric: metric, Entity: e, Unit: api.Metrics[metric].Unit, Step: dur(step)}
	end := r.GetEnd().AsTime()
	if s.dataEnd().Before(end) {
		end = s.dataEnd()
	}
	start := latest(r.GetStart().AsTime(), s.dataStart())
	for b := api.BucketStart(start, step); b.Before(end); b = b.Add(step) {
		t := b
		if t.Before(start) {
			t = start
		}
		stop := earliest(b.Add(step), end)
		var sum float64
		var n int
		var lo, hi *float64
		if metric == "cluster.slow_consumers" {
			for _, ev := range s.sc.slowEvents {
				if c := ev.Add(s.shift); !c.Before(t) && c.Before(stop) {
					sum++
				}
			}
			n = 1
		} else if eventMetrics[metric] {
			// Samples on a grid of sampleEvery, so that no event counts
			// in two buckets.
			for x := api.BucketStart(t.Add(sampleEvery-time.Nanosecond), sampleEvery); x.Before(stop); x = x.Add(sampleEvery) {
				if v, ok := s.sc.value(metric, e, s.clock(x)); ok {
					sum += v
				}
			}
			n = 1
		} else if out.Unit == v1.Unit_UNIT_COUNT {
			// A count takes the bucket's last sample, as the store's
			// arg_max does, so it stays whole, with its lowest and highest.
			for x := t; x.Before(stop); x = x.Add(sampleEvery) {
				if v, ok := s.sc.value(metric, e, s.clock(x)); ok {
					sum, n = v, 1
					if lo == nil || v < *lo {
						lo = ptr(v)
					}
					if hi == nil || v > *hi {
						hi = ptr(v)
					}
				}
			}
		} else {
			for x := t; x.Before(stop); x = x.Add(sampleEvery) {
				if v, ok := s.sc.value(metric, e, s.clock(x)); ok {
					sum += v
					n++
				}
			}
		}
		if n == 0 {
			continue
		}
		v := sum / float64(n)
		out.Points = append(out.Points, &v1.Point{Time: ts(b), Value: v, Low: lo, High: hi})
		// As for a band, min and max take in a count bucket's edges.
		low, high := v, v
		if lo != nil && hi != nil {
			low, high = *lo, *hi
		}
		if out.Min == nil || low < out.GetMin() {
			out.Min = ptr(low)
		}
		if out.Max == nil || high > out.GetMax() {
			out.Max = ptr(high)
		}
	}
	return out
}

// GetTimeSeries draws one metric of a known entity.
func (p *Provider) GetTimeSeries(_ context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
	e, err := p.sc.resolveEntity(req.GetEntity())
	if err != nil {
		return nil, err
	}
	return &v1.GetTimeSeriesResponse{Series: p.scen().series(req.GetMetric(), e, req.GetRange(), req.GetMaxPoints())}, nil
}

// resolveEntity checks that the entity exists and fills in its parts.
func (sc *scenario) resolveEntity(e *v1.EntityRef) (*v1.EntityRef, error) {
	switch e.GetType() {
	case v1.EntityType_ENTITY_TYPE_SYSTEM:
		return sc.systemRef(), nil
	case v1.EntityType_ENTITY_TYPE_SERVER:
		if _, ok := sc.server(e.GetKey()); ok {
			return serverRef(e.GetKey()), nil
		}
	case v1.EntityType_ENTITY_TYPE_STREAM:
		acc, name, _ := strings.Cut(e.GetKey(), "/")
		if _, err := sc.findStream(acc, name); err == nil {
			return streamRef(name), nil
		}
	case v1.EntityType_ENTITY_TYPE_CONSUMER:
		parts := strings.Split(e.GetKey(), "/")
		if len(parts) == 3 {
			if sp, err := sc.findStream(parts[0], parts[1]); err == nil {
				names := append(append([]string{}, sp.consumers...), sp.expected...)
				for _, v := range sp.vanished {
					names = append(names, v.name)
				}
				for _, n := range names {
					if n == parts[2] {
						return consumerRef(sp.name, n), nil
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("entity %s %q: %w", e.GetType(), e.GetKey(), api.ErrNotFound)
}
