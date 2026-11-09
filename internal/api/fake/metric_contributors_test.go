package fake

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"testing"
	"time"
)

func TestDemoMetricContributorsFamiliesAndFrozenPages(t *testing.T) {
	p := New(false)
	rng := &v1.TimeRange{Start: ts(FixedNow.Add(-time.Hour)), End: ts(FixedNow)}
	for _, metric := range []string{"server.cpu_percent", "server.in_msgs_rate", "stream.bytes", "consumer.num_pending"} {
		t.Run(metric, func(t *testing.T) {
			req := &v1.ListMetricContributorsRequest{Metric: metric, Range: rng, Ranking: v1.MetricRanking_METRIC_RANKING_MAXIMUM, Descending: true, Limit: 1}
			got, err := p.ListMetricContributors(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if got.TotalCount < 2 || len(got.Contributors) != 1 || got.NextPageToken == "" {
				t.Fatal(got)
			}
			c := got.Contributors[0]
			direct, err := p.GetTimeSeries(context.Background(), &v1.GetTimeSeriesRequest{Metric: metric, Entity: c.Entity, Range: rng, IncludeObservations: true, MaxPoints: 1})
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(c.Observations, direct.Series.Observations) {
				t.Fatalf("demo ranking formula drift: %v / %v", c.Observations, direct.Series.Observations)
			}
			next := proto.Clone(req).(*v1.ListMetricContributorsRequest)
			next.PageToken = got.NextPageToken
			b, err := p.ListMetricContributors(context.Background(), next)
			if err != nil || len(b.GetContributors()) != 1 || b.Contributors[0].Entity.Key == c.Entity.Key {
				t.Fatalf("%v %v", b, err)
			}
		})
	}
}
