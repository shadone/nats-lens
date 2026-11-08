package storeprov_test

import (
	"encoding/json"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func requestObservations(req *v1.GetTimeSeriesRequest) {
	if f := req.ProtoReflect().Descriptor().Fields().ByName("include_observations"); f != nil {
		req.ProtoReflect().Set(f, protoreflect.ValueOfBool(true))
	}
}
func observationMap(t *testing.T, s *v1.Series) map[string]any {
	t.Helper()
	b, e := protojson.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	v, ok := out["observations"].(map[string]any)
	if !ok {
		t.Fatalf("missing native observation summary: %s", b)
	}
	return v
}
func TestSeriesObservationSummaryBeforeBucketing(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	start := t0.Truncate(time.Hour)
	for i, v := range []any{10.0, 100.0, 30.0, nil} {
		r.epoch(int64(i+1), start.Add(time.Duration(i)*30*time.Second), "S0")
		r.add("server_stats", map[string]any{"epoch": int64(i + 1), "server_id": "S0", "cpu_percent": v, "mem_bytes": int64((i + 1) * 1024), "connections": int64(i)})
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(2 * time.Minute))}
	for _, metric := range []string{"server.cpu_percent", "server.mem_bytes", "server.connections"} {
		t.Run(metric, func(t *testing.T) {
			var summary map[string]any
			for _, points := range []int32{1, 200} {
				req := &v1.GetTimeSeriesRequest{Metric: metric, Entity: serverRef("nats-0"), Range: rng, MaxPoints: points}
				requestObservations(req)
				res, err := p.GetTimeSeries(ctx, req)
				if err != nil {
					t.Fatal(err)
				}
				got := observationMap(t, res.Series)
				if summary != nil {
					a, _ := json.Marshal(summary)
					b, _ := json.Marshal(got)
					if string(a) != string(b) {
						t.Fatalf("resolution changed raw summary: %s vs %s", a, b)
					}
				}
				summary = got
			}
			if summary["sampleCount"] != "4" {
				t.Fatalf("sample count: %v", summary)
			}
			if metric == "server.cpu_percent" {
				if summary["knownCount"] != "3" || summary["maximum"] != 100.0 || summary["maximumAt"] != start.Add(30*time.Second).Format(time.RFC3339) || summary["latest"] != nil {
					t.Fatalf("raw spike/null evidence: %v", summary)
				}
			}
			if metric == "server.connections" && summary["minimum"] != float64(0) {
				t.Fatalf("measured zero lost: %v", summary)
			}
		})
	}
}

func TestSeriesObservationMetricSpecificEvidence(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	start := t0.Truncate(time.Hour)
	for i := 0; i < 3; i++ {
		r.epoch(int64(i+1), start.Add(time.Duration(i)*30*time.Second), "S0", "S1")
		for j, id := range []string{"S0", "S1"} {
			r.add("server_stats", map[string]any{"epoch": int64(i + 1), "server_id": id, "cpu_percent": [][]float64{{0, 100, 10}, {10, 20, 200}}[j][i], "in_msgs": []int64{0, 60, 180}[i], "slow_consumers": []int64{0, 3, 5}[i]})
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(90 * time.Second))}
	read := func(metric string, entity *v1.EntityRef) *v1.Series {
		t.Helper()
		out, e := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: metric, Entity: entity, Range: rng, MaxPoints: 1, IncludeObservations: true})
		if e != nil {
			t.Fatal(e)
		}
		return out.Series
	}
	band := read("cluster.cpu_percent", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}).GetObservations()
	if band.GetMaximum() != 105 || band.GetMaximumHigh() != 200 || band.GetMinimumLow() != 0 || !band.GetMaximumAt().AsTime().Equal(start.Add(time.Minute)) {
		t.Fatalf("band median versus server edges: %v", band)
	}
	rate := read("server.in_msgs_rate", serverRef("nats-0")).GetObservations()
	if rate.GetMean() != 3 || rate.GetMaximum() != 4 || rate.ObservedTotal != nil || rate.GetKnownCount() != 2 {
		t.Fatalf("rate summary must not invent volume: %v", rate)
	}
	counts := read("server.slow_consumers", serverRef("nats-0"))
	if counts.GetObservations().GetObservedTotal() != "5" || counts.Points[0].Value != 5 {
		t.Fatalf("event increments differ from bucket totals: %v", counts)
	}
	unknown := read("server.mem_bytes", serverRef("nats-0")).GetObservations()
	if unknown.GetSampleCount() != 3 || unknown.KnownCount != 0 || unknown.Latest != nil || unknown.Mean != nil || unknown.MaximumAt != nil {
		t.Fatalf("missing gauge became zero: %v", unknown)
	}
	rng.Start = timestamppb.New(start.Add(30 * time.Second))
	rng.End = timestamppb.New(start.Add(60 * time.Second))
	bounded := read("server.cpu_percent", serverRef("nats-0")).GetObservations()
	if bounded.SampleCount != 1 || bounded.GetMaximum() != 100 || !bounded.FirstSampleAt.AsTime().Equal(start.Add(30*time.Second)) {
		t.Fatalf("half-open bounds: %v", bounded)
	}
}
