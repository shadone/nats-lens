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
