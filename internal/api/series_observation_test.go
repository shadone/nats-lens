package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestSeriesObservationBatchOptionAndBounds(t *testing.T) {
	p := &scripted{Provider: fake.New(false)}
	p.fn = func(_ context.Context, r *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
		s := &v1.Series{Metric: r.Metric}
		if r.IncludeObservations {
			s.Observations = &v1.ObservationSummary{SampleCount: 7}
		}
		return &v1.GetTimeSeriesResponse{Series: s}, nil
	}
	_, c := apitest.Serve(t, p)
	rng := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-time.Hour)), End: timestamppb.New(fake.FixedNow)}
	got, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Range: rng, Queries: []*v1.SeriesQuery{{Metric: "cluster.connections", IncludeObservations: true}, {Metric: "cluster.connections"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.Results[0].GetSeries().GetObservations().GetSampleCount() != 7 || got.Msg.Results[1].GetSeries().Observations != nil {
		t.Fatalf("per-query opt-in lost: %v", got.Msg)
	}
	rng.Start = timestamppb.New(fake.FixedNow.Add(-31 * 24 * time.Hour))
	got, err = c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Range: rng, Queries: []*v1.SeriesQuery{{Metric: "cluster.connections", IncludeObservations: true}, {Metric: "cluster.connections"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Msg.Results[0].Error, "30 days") || got.Msg.Results[1].Series == nil {
		t.Fatalf("wide observation query must fail alone, legacy remains valid: %v", got.Msg)
	}
	_, err = c.GetTimeSeries(context.Background(), connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.connections", Range: rng, IncludeObservations: true}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("wide summary accepted: %v", err)
	}
}
