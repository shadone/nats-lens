package storeprov_test

import (
	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestReviewSlowInvestigationChartBoundary(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	for i, seconds := range []int{0, 150} {
		r.epoch(int64(i+1), t0.Add(time.Duration(seconds)*time.Second), "S0")
		r.add("server_stats", map[string]any{"epoch": i + 1, "server_id": "S0", "slow_consumers": i * 7})
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: time.Minute, Live: true, Clock: func() time.Time { return t0.Add(3 * time.Minute) }})
	rng := &v1.TimeRange{Start: timestamppb.New(t0.Add(150 * time.Second)), End: timestamppb.New(t0.Add(3 * time.Minute))}
	inv := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(&v1.GetSlowConsumerInvestigationRequest{Range: rng})))
	chart := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Range: rng, Metric: "cluster.slow_consumers"})))
	var n int64
	for _, p := range chart.GetSeries().GetPoints() {
		n += int64(p.Value)
	}
	if n != inv.Detections {
		t.Fatalf("same range: chart=%d investigation=%d observations=%v", n, inv.Detections, inv.Observations)
	}
}
func TestReviewSlowHeadlineInvestigationBoundary(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	for i, v := range []int{0, 5, 8} {
		r.epoch(int64(i+1), t0.Add(time.Duration(i)*30*time.Second), "S0")
		r.add("server_stats", map[string]any{"epoch": i + 1, "server_id": "S0", "slow_consumers": v})
	}
	if _, err := st.DB().Exec(`UPDATE servers SET last_epoch=3,last_seen_at=?`, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	c := serveSeries(t, st, t0.Add(90*time.Second))
	rng := &v1.TimeRange{Start: timestamppb.New(t0.Add(15 * time.Second)), End: timestamppb.New(t0.Add(90 * time.Second))}
	inv := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(&v1.GetSlowConsumerInvestigationRequest{Range: rng})))
	list := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{Range: rng})))
	var n int64
	for _, s := range list.Servers {
		n += s.GetSlowConsumersInRange()
	}
	if n != inv.Detections {
		t.Fatalf("same range: headline=%d investigation=%d", n, inv.Detections)
	}
}
