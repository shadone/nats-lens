package storeprov

import (
	"context"
	"errors"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"strings"
	"testing"
)

func TestMetricContributorsCandidateBudgetsAndCancellation(t *testing.T) {
	p, _, rng := pageScaleProvider(t, 0)
	_, err := p.St.DB().Exec(`INSERT INTO stream_stats(epoch,server_id,account,stream,bytes,is_leader) SELECT 37,'S0','APP','orders-'||i,100,true FROM range(?) t(i)`, api.MaxListRankingRows+1)
	if err != nil {
		t.Fatal(err)
	}
	req := &v1.ListMetricContributorsRequest{Metric: "stream.bytes", Range: rng, Limit: 1}
	if out, err := p.ListMetricContributors(context.Background(), req); !errors.Is(err, api.ErrResourceExhausted) || out != nil {
		t.Fatalf("unbounded candidate capture: %v %v", out, err)
	}
	req.Query = "orders-20000"
	if out, err := p.ListMetricContributors(context.Background(), req); err != nil || out.TotalCount != 1 {
		t.Fatalf("narrowing did not recover: %v %v", out, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.ListMetricContributors(canceled, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	_, err = p.St.DB().Exec(`INSERT INTO stream_stats(epoch,server_id,account,stream,bytes,is_leader) VALUES(37,'S0','APP',?,10,true)`, strings.Repeat("x", api.MaxListContextBytes))
	if err != nil {
		t.Fatal(err)
	}
	req.Query = "xxxx"
	if _, err = p.ListMetricContributors(context.Background(), req); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatalf("unbounded candidate bytes: %v", err)
	}
}
func TestMetricContributorsScaleUsesSharedAggregateQuery(t *testing.T) {
	p, _, rng := pageScaleProvider(t, 1200)
	got, err := p.ListMetricContributors(context.Background(), &v1.ListMetricContributorsRequest{Metric: "stream.bytes", Range: rng, Ranking: v1.MetricRanking_METRIC_RANKING_MAXIMUM, Descending: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalCount != 1200 || len(got.Contributors) != 10 || got.Contributors[0].Entity.Key != "APP/orders-01199" {
		t.Fatal(got)
	}
}
func TestMetricContributorsShareOneReadBoundary(t *testing.T) {
	p, _, rng := pageScaleProvider(t, 2)
	spy := &observationWriter{DB: p.St.DB()}
	spy.write = func() {
		if _, err := p.St.DB().Exec(`UPDATE stream_stats SET bytes=999999 WHERE stream='orders-00000'`); err != nil {
			t.Fatal(err)
		}
	}
	p.readDB = spy
	got, err := p.ListMetricContributors(context.Background(), &v1.ListMetricContributorsRequest{Metric: "stream.bytes", Range: rng, Ranking: v1.MetricRanking_METRIC_RANKING_MAXIMUM, Descending: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !spy.written || len(got.Contributors) != 1 || got.Contributors[0].Entity.Key != "APP/orders-00001" {
		t.Fatalf("ranking crossed its capture boundary: %v", got)
	}
}
