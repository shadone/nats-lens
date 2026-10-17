package fake

import (
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// bandOf maps a band metric to the server metric it spreads across the
// servers.
var bandOf = map[string]string{
	"cluster.cpu_percent": "server.cpu_percent",
	"cluster.mem_bytes":   "server.mem_bytes",
}

// eventMetrics are counts of events: a bucket sums them, and each sample
// counts the events in the sampleEvery that starts at it.
var eventMetrics = map[string]bool{
	"cluster.meta_leader_changes": true,
	"server.slow_consumers":       true,
}

// countIn counts the times in [c, c+sampleEvery).
func countIn(times []time.Time, c time.Time) float64 {
	n := 0.0
	for _, t := range times {
		if !t.Before(c) && t.Before(c.Add(sampleEvery)) {
			n++
		}
	}
	return n
}

// value is a metric's value at fixed-frame time c: the counts that follow
// from the scenario's streams and consumers, else the scenario's sample.
func (sc *scenario) value(metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	switch metric {
	case "cluster.streams", "cluster.kv_buckets", "cluster.object_stores":
		n := 0.0
		for _, sp := range sc.streams {
			kv, obj := strings.HasPrefix(sp.name, "KV_"), strings.HasPrefix(sp.name, "OBJ_")
			if (metric == "cluster.kv_buckets" && kv) || (metric == "cluster.object_stores" && obj) || (metric == "cluster.streams" && !kv && !obj) {
				n++
			}
		}
		return n, true
	case "cluster.accounts":
		// The demo has one account.
		return 1, true
	case "cluster.assets_replicated", "cluster.assets_single":
		// Durable consumers have their stream's replicas. The durable
		// count over time also holds consumers deleted since, which the
		// stream specs no longer list; they lived on replicated streams.
		var repl, single, listed float64
		for _, sp := range sc.streams {
			n := 1 + float64(len(sp.consumers))
			listed += float64(len(sp.consumers))
			if len(sc.peers(sp)) > 1 {
				repl += n
			} else {
				single += n
			}
		}
		if d, ok := sc.sample(sc, "cluster.durable_consumers", e, c); ok {
			repl += d - listed
		}
		if metric == "cluster.assets_single" {
			return single, true
		}
		return repl, true
	}
	return sc.sample(sc, metric, e, c)
}

// bandSeries draws a band metric: at each sample the median, lowest and
// highest server, averaged per bucket, plus the server with the highest
// mean over the range and its own series.
func (s scen) bandSeries(metric string, e *v1.EntityRef, r *v1.TimeRange, maxPoints int32) *v1.Series {
	perServer := bandOf[metric]
	step := api.Step(r, maxPoints, sampleEvery)
	out := &v1.Series{Metric: metric, Entity: e, Unit: api.Metrics[metric].Unit, Step: dur(step)}
	end := earliest(r.GetEnd().AsTime(), s.dataEnd())
	start := r.GetStart().AsTime()
	sums := map[string]float64{}
	counts := map[string]int{}
	for b := api.BucketStart(start, step); b.Before(end); b = b.Add(step) {
		t := b
		if t.Before(start) {
			t = start
		}
		var med, lo, hi float64
		n := 0
		for x := t; x.Before(earliest(b.Add(step), end)); x = x.Add(sampleEvery) {
			var vs []float64
			for _, sp := range s.sc.servers {
				if v, ok := s.sc.value(perServer, serverRef(sp.name), s.clock(x)); ok {
					vs = append(vs, v)
					sums[sp.name] += v
					counts[sp.name]++
				}
			}
			if len(vs) == 0 {
				continue
			}
			slices.Sort(vs)
			med += median(vs)
			lo += vs[0]
			hi += vs[len(vs)-1]
			n++
		}
		if n == 0 {
			continue
		}
		v := med / float64(n)
		out.Points = append(out.Points, &v1.Point{Time: ts(b), Value: v, Low: ptr(lo / float64(n)), High: ptr(hi / float64(n))})
		if out.Min == nil || v < out.GetMin() {
			out.Min = ptr(v)
		}
		if out.Max == nil || v > out.GetMax() {
			out.Max = ptr(v)
		}
	}
	top, best := "", 0.0
	for _, sp := range s.sc.servers {
		if counts[sp.name] == 0 {
			continue
		}
		if m := sums[sp.name] / float64(counts[sp.name]); top == "" || m > best {
			top, best = sp.name, m
		}
	}
	if top != "" {
		out.Top = serverRef(top)
		out.TopPoints = s.series(perServer, out.Top, r, maxPoints).GetPoints()
	}
	return out
}

// median of sorted values; the mean of the middle two for an even count.
func median(vs []float64) float64 {
	n := len(vs)
	if n%2 == 1 {
		return vs[n/2]
	}
	return (vs[n/2-1] + vs[n/2]) / 2
}
