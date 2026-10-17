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
		return traffic(metric, c, 168, 14, 0.72, 182), true
	case "cluster.in_msgs_rate":
		return traffic(metric, c, 1180, 260, 0.55, 1240), true
	case "cluster.out_msgs_rate":
		return traffic(metric, c, 3700, 700, 0.5, 3910), true
	case "cluster.in_bytes_rate", "cluster.out_bytes_rate", "server.in_bytes_rate", "server.out_bytes_rate":
		// About 1.2 KiB per message.
		v, ok := prodSample(sc, strings.Replace(metric, "_bytes_", "_msgs_", 1), e, c)
		return math.Round(v * 1230), ok
	case "cluster.js_storage_bytes":
		return (5.81 - days*0.19) * gib, true
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
		return math.Round(v / 3), true
	case "server.mem_bytes":
		return 412*mib + noise(metric+e.GetKey(), c)*40*mib, !down(e.GetKey(), c)
	case "server.cpu_percent":
		return 7.5 + noise(metric+e.GetKey(), c)*4, !down(e.GetKey(), c)
	case "server.js_storage_bytes":
		return 1980*mib - days*65*mib, !down(e.GetKey(), c)
	case "stream.bytes", "stream.messages":
		sp, _ := sc.findStream(e.GetAccount(), e.GetStream())
		b := math.Max(sp.bytes-days*sp.perDay, sp.bytes*0.05)
		if metric == "stream.bytes" {
			return b, true
		}
		return math.Round(float64(sp.msgs) * b / sp.bytes), true
	case "consumer.num_pending", "consumer.num_ack_pending", "consumer.num_redelivered":
		return consumerSample(metric, e, c)
	}
	return 0, false
}

func consumerSample(metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	if e.GetConsumer() == "analytics-aggregator" && !c.Before(fixed(9, 31, 7)) {
		return 0, false
	}
	video := e.GetConsumer() == "media-chunk-pool"
	switch {
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

// series draws a metric over the range: averages of samples for gauges and
// rates, sums of events for cluster.slow_consumers.
func (s scen) series(metric string, e *v1.EntityRef, r *v1.TimeRange, maxPoints int32) *v1.Series {
	step := api.Step(r, maxPoints, sampleEvery)
	out := &v1.Series{Metric: metric, Entity: e, Unit: api.Metrics[metric].Unit, Step: dur(step)}
	end := r.GetEnd().AsTime()
	if s.dataEnd().Before(end) {
		end = s.dataEnd()
	}
	start := r.GetStart().AsTime()
	for b := api.BucketStart(start, step); b.Before(end); b = b.Add(step) {
		t := b
		if t.Before(start) {
			t = start
		}
		stop := earliest(b.Add(step), end)
		var sum float64
		var n int
		if metric == "cluster.slow_consumers" {
			for _, ev := range s.sc.slowEvents {
				if c := ev.Add(s.shift); !c.Before(t) && c.Before(stop) {
					sum++
				}
			}
			n = 1
		} else {
			for x := t; x.Before(stop); x = x.Add(sampleEvery) {
				if v, ok := s.sc.sample(s.sc, metric, e, s.clock(x)); ok {
					sum += v
					n++
				}
			}
		}
		if n == 0 {
			continue
		}
		v := sum / float64(n)
		out.Points = append(out.Points, &v1.Point{Time: ts(b), Value: v})
		if out.Min == nil || v < out.GetMin() {
			out.Min = ptr(v)
		}
		if out.Max == nil || v > out.GetMax() {
			out.Max = ptr(v)
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
				for _, n := range append(append([]string{}, sp.consumers...), sp.expected...) {
					if n == parts[2] {
						return consumerRef(sp.name, n), nil
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("entity %s %q: %w", e.GetType(), e.GetKey(), api.ErrNotFound)
}
