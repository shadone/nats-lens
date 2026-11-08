package fake

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// observationSummary uses the scenario's native sample grid, independently of
// requested chart buckets. Demo observations remain explicitly synthetic.
func (s scen) observationSummary(ctx context.Context, metric string, e *v1.EntityRef, r *v1.TimeRange) (*v1.ObservationSummary, error) {
	start, end := latest(r.GetStart().AsTime(), s.dataStart()), earliest(r.GetEnd().AsTime(), s.dataEnd())
	out := &v1.ObservationSummary{}
	var total uint64
	summed := metric == "cluster.slow_consumers" || eventMetrics[metric]
	for at := api.BucketStart(start.Add(sampleEvery-1), sampleEvery); at.Before(end); at = at.Add(sampleEvery) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var value float64
		var ok bool
		if band, yes := bandOf[metric]; yes {
			var vs []float64
			for _, server := range s.sc.servers {
				if v, known := s.sc.value(band, serverRef(server.name), s.clock(at)); known {
					vs = append(vs, v)
				}
			}
			if len(vs) == 0 {
				continue
			}
			slices.Sort(vs)
			value, ok = median(vs), true
			if out.MinimumLow == nil || vs[0] < out.GetMinimumLow() {
				out.MinimumLow = ptr(vs[0])
			}
			if out.MaximumHigh == nil || vs[len(vs)-1] > out.GetMaximumHigh() {
				out.MaximumHigh = ptr(vs[len(vs)-1])
			}
		} else if metric == "cluster.slow_consumers" || metric == "server.slow_consumers" {
			for _, server := range s.sc.servers {
				if metric == "server.slow_consumers" && server.name != e.GetKey() {
					continue
				}
				value += float64(s.slowCount(server, at, earliest(at.Add(sampleEvery), end)))
			}
			ok = true
		} else {
			value, ok = s.sc.value(metric, e, s.clock(at))
		}
		if !ok {
			continue
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("%w: non-finite demo observation", api.ErrInvalid)
		}
		out.SampleCount++
		out.KnownCount++
		if out.FirstSampleAt == nil {
			out.FirstSampleAt = ts(at)
		}
		out.LastSampleAt = ts(at)
		out.Latest = ptr(value)
		mean := out.GetMean() + (value-out.GetMean())/float64(out.KnownCount)
		out.Mean = ptr(mean)
		if out.Minimum == nil || value < out.GetMinimum() {
			out.Minimum = ptr(value)
		}
		if out.Maximum == nil || value > out.GetMaximum() {
			out.Maximum = ptr(value)
			out.MaximumAt = ts(at)
		}
		if summed {
			if value < 0 || value != math.Trunc(value) {
				return nil, fmt.Errorf("%w: noninteger demo increment", api.ErrInvalid)
			}
			total += uint64(value)
		}
	}
	if summed && out.KnownCount > 0 {
		out.ObservedTotal = ptr(strconv.FormatUint(total, 10))
	}
	return out, nil
}
