package storeprov_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func trafficCounterEvidence(t *testing.T, s *v1.Series) map[string]any {
	t.Helper()
	observations := observationMap(t, s)
	counter, ok := observations["counterComparisons"].(map[string]any)
	if !ok {
		t.Fatalf("missing comparable counter evidence: %v", observations)
	}
	return counter
}
func TestTrafficCounterComparisonsExactAndScoped(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	start := t0.Truncate(time.Hour)
	for i := 0; i < 3; i++ {
		r.epoch(int64(i+1), start.Add(time.Duration(i)*30*time.Second), "S0", "S1")
		for _, id := range []string{"S0", "S1"} {
			n := []int64{0, 9007199254740993, 9007199254741000}[i]
			if id == "S1" {
				n = int64(i * 10)
			}
			r.add("server_stats", map[string]any{"epoch": int64(i + 1), "server_id": id, "in_msgs": n, "out_msgs": n, "in_bytes": n, "out_bytes": n})
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(90 * time.Second))}
	for _, metric := range []string{"in_msgs_rate", "out_msgs_rate", "in_bytes_rate", "out_bytes_rate"} {
		for _, scope := range []string{"server.", "cluster."} {
			t.Run(scope+metric, func(t *testing.T) {
				entity := serverRef("nats-0")
				want, pairs, processes := "9007199254741000", "2", "1"
				if scope == "cluster." {
					entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
					want, pairs, processes = "9007199254741020", "4", "2"
				}
				var first map[string]any
				for _, points := range []int32{1, 200} {
					out, e := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: scope + metric, Entity: entity, Range: rng, MaxPoints: points, IncludeObservations: true})
					if e != nil {
						t.Fatal(e)
					}
					c := trafficCounterEvidence(t, out.Series)
					if c["observedIncrease"] != want || c["comparisonCount"] != pairs || c["serverIdCount"] != processes {
						t.Fatalf("counter total rounded or scope changed: %v", c)
					}
					if c["firstComparedAt"] != start.Format(time.RFC3339) || c["lastComparedAt"] != start.Add(time.Minute).Format(time.RFC3339) {
						t.Fatalf("comparison envelope: %v", c)
					}
					if first != nil && first["observedIncrease"] != c["observedIncrease"] {
						t.Fatal("bucket count changed total")
					}
					first = c
					if out.Series.GetObservations().ObservedTotal != nil {
						t.Fatal("rate overwrote the event-total contract")
					}
				}
			})
		}
	}
}
func TestTrafficCounterComparisonsExcludeStartBoundaryAndUnknown(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	start := t0.Truncate(time.Hour)
	for i, n := range []any{int64(0), int64(100), int64(100), nil, int64(200), int64(250)} {
		r.epoch(int64(i+1), start.Add(time.Duration(i)*30*time.Second), "S0")
		r.add("server_stats", map[string]any{"epoch": int64(i + 1), "server_id": "S0", "in_msgs": n})
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	read := func(from, to time.Time) map[string]any {
		t.Helper()
		out, e := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: "server.in_msgs_rate", Entity: serverRef("nats-0"), Range: &v1.TimeRange{Start: timestamppb.New(from), End: timestamppb.New(to)}, MaxPoints: 1, IncludeObservations: true})
		if e != nil {
			t.Fatal(e)
		}
		return trafficCounterEvidence(t, out.Series)
	}
	c := read(start.Add(15*time.Second), start.Add(150*time.Second))
	if c["observedIncrease"] != "0" || c["comparisonCount"] != "1" || c["boundaryReports"] != "1" || c["excludedReports"] != "3" {
		t.Fatalf("boundary/null/zero rules: %v", c)
	}
	c = read(start.Add(15*time.Second), start.Add(30*time.Second))
	if c["observedIncrease"] != nil || c["comparisonCount"] != nil || c["firstComparedAt"] != nil {
		t.Fatalf("empty comparison became measured zero: %v", c)
	}
	c = read(start.Add(30*time.Second), start.Add(31*time.Second))
	if c["observedIncrease"] != nil || c["boundaryReports"] != "1" {
		t.Fatalf("cross-start pair credited: %v", c)
	}
}
func TestTrafficCounterComparisonsRestartsResetsAndGap(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("old", "nats-0")
	r.server("new", "nats-0")
	start := t0.Truncate(time.Hour)
	samples := []struct {
		id string
		at time.Duration
		n  int64
	}{{"old", 0, 100}, {"old", 30 * time.Second, 130}, {"old", 60 * time.Second, 2}, {"new", 90 * time.Second, 500}, {"new", 120 * time.Second, 505}, {"new", 210 * time.Second, 510}, {"new", 301 * time.Second, 520}}
	for i, s := range samples {
		r.epoch(int64(i+1), start.Add(s.at), s.id)
		r.add("server_stats", map[string]any{"epoch": int64(i + 1), "server_id": s.id, "in_msgs": s.n})
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	out, e := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: "server.in_msgs_rate", Entity: serverRef("nats-0"), Range: &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(6 * time.Minute))}, IncludeObservations: true})
	if e != nil {
		t.Fatal(e)
	}
	c := trafficCounterEvidence(t, out.Series)
	if c["observedIncrease"] != "40" || c["comparisonCount"] != "3" || c["serverIdCount"] != "2" || c["excludedReports"] != "4" {
		t.Fatalf("restart/reset/90s/91s rules: %v", c)
	}
}
func TestTrafficRatesDoNotAcceptBackwardsTimeAndCounter(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	start := t0.Truncate(time.Hour)
	for i, s := range []struct {
		at time.Duration
		n  int64
	}{{60 * time.Second, 100}, {30 * time.Second, 70}} {
		r.epoch(int64(i+1), start.Add(s.at), "S0")
		r.add("server_stats", map[string]any{"epoch": int64(i + 1), "server_id": "S0", "in_msgs": s.n})
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	out, e := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: "server.in_msgs_rate", Entity: serverRef("nats-0"), Range: &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(2 * time.Minute))}})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Series.Points) != 0 {
		t.Fatalf("backwards time and reset fabricated a positive rate: %v", out.Series.Points)
	}
}

func TestTrafficCounterComparisonsSumBeyondInt64AndRejectNegative(t *testing.T) {
	for _, negative := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact_sum", true: "invalid_negative"}[negative], func(t *testing.T) {
			st := emptyStore(t)
			r := rows{t, st}
			r.server("S0", "nats-0")
			r.server("S1", "nats-1")
			start := t0.Truncate(time.Hour)
			for i := 0; i < 2; i++ {
				r.epoch(int64(i+1), start.Add(time.Duration(i)*30*time.Second), "S0", "S1")
				for _, id := range []string{"S0", "S1"} {
					n := int64(0)
					if i == 1 {
						n = 9223372036854775807
					}
					if negative {
						n = int64(-100 + i*10)
					}
					r.add("server_stats", map[string]any{"epoch": int64(i + 1), "server_id": id, "in_msgs": n})
				}
			}
			p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
			req := &v1.GetTimeSeriesRequest{Metric: "cluster.in_msgs_rate", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}, Range: &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(time.Minute))}, IncludeObservations: true}
			out, e := p.GetTimeSeries(ctx, req)
			if e != nil {
				t.Fatal(e)
			}
			c := trafficCounterEvidence(t, out.Series)
			if negative {
				if c["observedIncrease"] != nil || c["excludedReports"] != "4" || len(out.Series.Points) != 0 {
					t.Fatalf("negative counters became traffic: %v", out.Series)
				}
			} else if c["observedIncrease"] != "18446744073709551614" {
				t.Fatalf("sum overflowed int64: %v", c)
			}
			req.IncludeObservations = false
			out, e = p.GetTimeSeries(ctx, req)
			if e != nil || out.Series.Observations != nil {
				t.Fatalf("ordinary response acquired optional summary: %v %v", out, e)
			}
		})
	}
}
