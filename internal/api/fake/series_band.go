package fake

import (
	"math"
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

// Series the demo draws only for the Capacity page's growth, not served
// as metrics: a server's memory store in use, and Account's JetStream use
// of each storage.
const (
	fakeServerMemStore   = "fake.server_memory_store_bytes"
	fakeAccountFileStore = "fake.account_file_store_bytes"
	fakeAccountMemStore  = "fake.account_memory_store_bytes"
)

// value is a metric's value at fixed-frame time c: the counts that follow
// from the scenario's streams and consumers, else the scenario's sample;
// none inside the scenario's noEpochs.
func (sc *scenario) value(metric string, e *v1.EntityRef, c time.Time) (float64, bool) {
	if !sc.hasCollection(c) {
		return 0, false
	}
	switch metric {
	case "cluster.capacity_file_store_bytes":
		var sum float64
		for _, sv := range sc.servers {
			v, ok := sc.value("server.js_storage_bytes", serverRef(sv.name), c)
			if !ok {
				return 0, false
			}
			sum += v
		}
		return sum, true
	case "server.capacity_file_store_bytes":
		return sc.value("server.js_storage_bytes", e, c)
	case fakeServerMemStore:
		// The memory streams the server holds, while it answers.
		if _, ok := sc.value("server.js_storage_bytes", e, c); !ok {
			return 0, false
		}
		var sum float64
		for _, sp := range sc.streams {
			if sc.memoryStorage(sp.name) && slices.Contains(sc.peers(sp), e.GetKey()) {
				v, ok := sc.value("stream.bytes", streamRef(sp.name), c)
				if !ok {
					return 0, false
				}
				sum += v
			}
		}
		return sum, true
	case fakeAccountFileStore, fakeAccountMemStore:
		// Account's streams of that storage times their replicas.
		var sum float64
		for _, sp := range sc.streams {
			if sc.memoryStorage(sp.name) != (metric == fakeAccountMemStore) {
				continue
			}
			v, ok := sc.value("stream.bytes", streamRef(sp.name), c)
			if !ok {
				return 0, false
			}
			sum += v * float64(len(sc.peers(sp)))
		}
		return sum, true
	case "stream.observed_replica_bytes":
		sp, err := sc.findStream(e.GetAccount(), e.GetStream())
		if err != nil {
			return 0, false
		}
		var sum float64
		// Use the same observed views as the detail API. Offline and missing
		// peers have no view, so configured replicas never supply measurements.
		views := (scen{sc: sc}).streamViews(sp, c)
		for _, view := range views {
			sum += float64(view.GetBytes())
		}
		return sum, len(views) > 0
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
// highest server. A bucket's value is the mean of its medians and its
// edges the lowest and highest sample in it; the series' min and max are
// the edges'. The server with the highest mean over the range comes with
// its own averaged series, which lies inside the band.
func (s scen) bandSeries(metric string, e *v1.EntityRef, r *v1.TimeRange, maxPoints int32, observationGrid ...bool) *v1.Series {
	perServer := bandOf[metric]
	step := api.Step(r, maxPoints, sampleEvery)
	out := &v1.Series{Metric: metric, Entity: e, Unit: api.Metrics[metric].Unit, Step: dur(step)}
	end := earliest(r.GetEnd().AsTime(), s.dataEnd())
	start := latest(r.GetStart().AsTime(), s.dataStart())
	sums := map[string]float64{}
	counts := map[string]int{}
	for b := api.BucketStart(start, step); b.Before(end); b = b.Add(step) {
		t := b
		if t.Before(start) {
			t = start
		}
		med, lo, hi := 0.0, math.Inf(1), math.Inf(-1)
		n := 0
		for x := observationSampleStart(t, observationGrid); x.Before(earliest(b.Add(step), end)); x = x.Add(sampleEvery) {
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
			lo, hi = math.Min(lo, vs[0]), math.Max(hi, vs[len(vs)-1])
			n++
		}
		if n == 0 {
			continue
		}
		v := med / float64(n)
		out.Points = append(out.Points, &v1.Point{Time: ts(b), Value: v, Low: ptr(lo), High: ptr(hi)})
		if out.Min == nil || lo < out.GetMin() {
			out.Min = ptr(lo)
		}
		if out.Max == nil || hi > out.GetMax() {
			out.Max = ptr(hi)
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
		out.TopPoints = s.series(perServer, out.Top, r, maxPoints, observationGrid...).GetPoints()
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

// hasCollection is shared by ordinary metrics, event buckets and summaries.
func (sc *scenario) hasCollection(c time.Time) bool {
	for _, h := range sc.noEpochs {
		if c.After(h.from) && c.Before(h.to) {
			return false
		}
	}
	return true
}
