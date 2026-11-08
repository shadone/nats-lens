package storeprov_test

import (
	"fmt"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestMetricContributorsGlobalRankingAndFrozenPages(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	start := t0.Truncate(time.Hour)
	ids := []string{}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("S%02d", i)
		ids = append(ids, id)
		r.server(id, fmt.Sprintf("nats-%02d", i))
	}
	r.epoch(1, start, ids...)
	r.epoch(2, start.Add(30*time.Second), ids...)
	for i, id := range ids {
		for epoch := int64(1); epoch <= 2; epoch++ {
			var v any = int64((i + 1) * 10)
			if epoch == 2 {
				v = int64(i)
				if i == 14 {
					v = nil
				}
			}
			r.add("server_stats", map[string]any{"epoch": epoch, "server_id": id, "cpu_percent": v})
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	req := &v1.ListMetricContributorsRequest{Metric: "server.cpu_percent", Range: &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(time.Minute))}, Ranking: v1.MetricRanking_METRIC_RANKING_MAXIMUM, Descending: true, Limit: 5}
	page, err := p.ListMetricContributors(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 16 || len(page.Contributors) != 5 || page.Contributors[0].Entity.Key != "nats-15" || page.Contributors[0].Observations.GetMaximum() != 160 || page.NextPageToken == "" {
		t.Fatalf("ranked loaded page instead of all names: %v", page)
	}
	if _, err = st.DB().Exec("UPDATE server_stats SET cpu_percent=9999 WHERE server_id='S00'"); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for {
		for _, row := range page.Contributors {
			names = append(names, row.Entity.Key)
			if row.Entity.Key == "nats-00" && row.Observations.GetMaximum() != 10 {
				t.Fatal("continuation re-read live summary")
			}
		}
		if page.NextPageToken == "" {
			break
		}
		next := proto.Clone(req).(*v1.ListMetricContributorsRequest)
		next.PageToken = page.NextPageToken
		page, err = p.ListMetricContributors(ctx, next)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(names) != 16 || names[15] != "nats-00" {
		t.Fatal(names)
	}
	req.Ranking = v1.MetricRanking_METRIC_RANKING_LATEST
	req.Descending = false
	req.Limit = 100
	page, err = p.ListMetricContributors(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if page.Contributors[len(page.Contributors)-1].Entity.Key != "nats-14" || page.Contributors[len(page.Contributors)-1].Observations.Latest != nil {
		t.Fatal("unknown latest did not sort last")
	}
	bad := proto.Clone(req).(*v1.ListMetricContributorsRequest)
	bad.Account = "APP"
	if _, err = p.ListMetricContributors(ctx, bad); err == nil {
		t.Fatal("server metric accepted an account measurement filter")
	}
	_ = api.MaxListRankingRows
}
func TestMetricContributorsHistoricalResourceScopeAndSummaryParity(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	start := t0.Truncate(time.Hour)
	r.server("S0", "nats-0")
	r.epoch(1, start, "S0")
	r.epoch(2, start.Add(30*time.Second), "S0")
	r.epoch(3, start.Add(time.Minute), "S0")
	for _, account := range []string{"APP", "OTHER"} {
		for _, name := range []string{"orders-a", "orders-z"} {
			r.add("streams", map[string]any{"account": account, "stream": name, "first_epoch": 1, "last_epoch": 2})
			for epoch := int64(1); epoch <= 2; epoch++ {
				n := int64(10)
				if name == "orders-z" {
					n = 100
				}
				r.add("stream_stats", map[string]any{"epoch": epoch, "server_id": "S0", "account": account, "stream": name, "bytes": n, "messages": n, "is_leader": true})
			}
		}
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second}
	rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(90 * time.Second))}
	got, err := p.ListMetricContributors(ctx, &v1.ListMetricContributorsRequest{Metric: "stream.bytes", Range: rng, Account: "APP", Ranking: v1.MetricRanking_METRIC_RANKING_MEAN, Descending: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalCount != 2 || len(got.Contributors) != 1 || got.Contributors[0].Entity.Key != "APP/orders-z" {
		t.Fatalf("lost historical resource or account scope: %v", got)
	}
	direct, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: "stream.bytes", Range: rng, Entity: got.Contributors[0].Entity, MaxPoints: 1, IncludeObservations: true})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(direct.Series.Observations, got.Contributors[0].Observations) {
		t.Fatalf("ranking changed authoritative observation semantics: %v / %v", direct.Series.Observations, got.Contributors[0].Observations)
	}
}
