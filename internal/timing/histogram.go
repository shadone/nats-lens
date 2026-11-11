// Package timing keeps the process's own latency histograms and gauges for
// /metrics: API calls, store queries, the store's write lock and the
// background jobs. Every family has a fixed set of label names whose values
// come from a small closed set (procedure names, job names, system IDs);
// none carries an entity, a query or a value read from NATS. A family
// holds at most MaxSeries label sets; later ones are counted under the
// value "other".
package timing

import (
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// MaxSeries bounds the label sets of one family.
const MaxSeries = 256

// Overflow is the label value a family uses once it holds MaxSeries sets.
const Overflow = "other"

// LatencyBuckets are the upper bounds, in seconds, of the duration
// histograms: from 1 ms, below which a DuckDB query never answers, to one
// minute, the longest interval an epoch write or a check run may take.
var LatencyBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// CountBuckets are the upper bounds of the queries-per-call histogram.
var CountBuckets = []float64{0, 1, 2, 5, 10, 20, 50, 100, 200, 500, 1000}

// Histogram counts observations into fixed buckets. It is safe for
// concurrent use and takes no lock.
type Histogram struct {
	bounds []float64
	counts []atomic.Uint64 // one per bound, then +Inf
	sum    atomic.Uint64   // float64 bits
	count  atomic.Uint64
}

func newHistogram(bounds []float64) *Histogram {
	return &Histogram{bounds: bounds, counts: make([]atomic.Uint64, len(bounds)+1)}
}

// Observe adds one observation.
func (h *Histogram) Observe(v float64) {
	if h == nil {
		return
	}
	i, _ := slices.BinarySearch(h.bounds, v)
	h.counts[i].Add(1)
	for {
		old := h.sum.Load()
		if h.sum.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			break
		}
	}
	h.count.Add(1)
}

// snapshot returns the cumulative bucket counts, the sum and the count.
// Concurrent observations may land between the reads; the count is read
// last and raised to the buckets' total, so that +Inf never exceeds it.
func (h *Histogram) snapshot() (cum []uint64, sum float64, count uint64) {
	cum = make([]uint64, len(h.counts))
	var run uint64
	for i := range h.counts {
		run += h.counts[i].Load()
		cum[i] = run
	}
	sum = math.Float64frombits(h.sum.Load())
	count = max(h.count.Load(), run)
	return cum, sum, count
}

// family is the label bookkeeping HistogramVec and GaugeVec share.
type family struct {
	name, help string
	labels     []string
	mu         sync.RWMutex
	keys       map[string][]string // joined values -> values
}

func (f *family) key(values []string) (string, []string) {
	if len(values) != len(f.labels) {
		panic("timing: " + f.name + " takes " + strings.Join(f.labels, ", "))
	}
	return strings.Join(values, "\x00"), values
}

// HistogramVec is a family of histograms by label values.
type HistogramVec struct {
	family
	bounds []float64
	series map[string]*Histogram
}

// NewHistogramVec declares a histogram family. It is registered with the
// default registry, which /metrics writes.
func NewHistogramVec(name, help string, labels []string, bounds []float64) *HistogramVec {
	v := &HistogramVec{family: family{name: name, help: help, labels: labels, keys: map[string][]string{}},
		bounds: bounds, series: map[string]*Histogram{}}
	register(v)
	return v
}

// With returns the histogram of the label values, in the order of the
// label names.
func (v *HistogramVec) With(values ...string) *Histogram {
	k, vals := v.key(values)
	v.mu.RLock()
	h := v.series[k]
	v.mu.RUnlock()
	if h != nil {
		return h
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if h = v.series[k]; h != nil {
		return h
	}
	if len(v.series) >= MaxSeries {
		vals = slices.Repeat([]string{Overflow}, len(vals))
		k = strings.Join(vals, "\x00")
		if h = v.series[k]; h != nil {
			return h
		}
	}
	h = newHistogram(v.bounds)
	v.series[k] = h
	v.keys[k] = slices.Clone(vals)
	return h
}

// GaugeVec is a family of gauges by label values that callers raise and
// lower, such as calls in flight.
type GaugeVec struct {
	family
	series map[string]*atomic.Int64
}

// NewGaugeVec declares a gauge family, registered like NewHistogramVec.
func NewGaugeVec(name, help string, labels []string) *GaugeVec {
	g := &GaugeVec{family: family{name: name, help: help, labels: labels, keys: map[string][]string{}},
		series: map[string]*atomic.Int64{}}
	register(g)
	return g
}

// With returns the gauge of the label values.
func (g *GaugeVec) With(values ...string) *atomic.Int64 {
	k, vals := g.key(values)
	g.mu.RLock()
	c := g.series[k]
	g.mu.RUnlock()
	if c != nil {
		return c
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if c = g.series[k]; c != nil {
		return c
	}
	if len(g.series) >= MaxSeries {
		vals = slices.Repeat([]string{Overflow}, len(vals))
		k = strings.Join(vals, "\x00")
		if c = g.series[k]; c != nil {
			return c
		}
	}
	c = new(atomic.Int64)
	g.series[k] = c
	g.keys[k] = slices.Clone(vals)
	return c
}

// Count returns the observations of the label values' histogram, 0 when
// it has none; unlike With, it adds no series.
func (v *HistogramVec) Count(values ...string) uint64 {
	k, _ := v.key(values)
	v.mu.RLock()
	defer v.mu.RUnlock()
	if h := v.series[k]; h != nil {
		return h.count.Load()
	}
	return 0
}

// Series is one histogram's state: its label values, cumulative bucket
// counts by Bounds (then +Inf), the sum and the count.
type Series struct {
	Labels  []string
	Bounds  []float64
	Buckets []uint64
	Sum     float64
	Count   uint64
}

// Series returns every histogram of the family, for a caller that reads
// the values itself rather than through /metrics.
func (v *HistogramVec) Series() []Series {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]Series, 0, len(v.series))
	for _, k := range v.sortedKeys() {
		cum, sum, count := v.series[k].snapshot()
		out = append(out, Series{Labels: slices.Clone(v.keys[k]), Bounds: v.bounds, Buckets: cum, Sum: sum, Count: count})
	}
	return out
}
