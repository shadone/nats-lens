package api_test

import (
	"connectrpc.com/connect"
	"context"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/investigation"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math"
	"testing"
	"time"
)

func TestMetricContributorsExactAndUnknownOrdering(t *testing.T) {
	str := func(s string) *string { return &s }
	rows := []*v1.MetricContributor{{Entity: &v1.EntityRef{Key: "a"}, Observations: &v1.ObservationSummary{ObservedTotal: str("9007199254740992"), Maximum: f64(2)}}, {Entity: &v1.EntityRef{Key: "z"}, Observations: &v1.ObservationSummary{ObservedTotal: str("9007199254740993"), Maximum: f64(3)}}, {Entity: &v1.EntityRef{Key: "missing"}, Observations: &v1.ObservationSummary{Maximum: f64(math.Inf(1))}}}
	req := &v1.ListMetricContributorsRequest{Ranking: v1.MetricRanking_METRIC_RANKING_OBSERVED_TOTAL, Descending: true}
	api.SortMetricContributors(rows, req)
	if rows[0].Entity.Key != "z" || rows[2].Entity.Key != "missing" {
		t.Fatal(rows)
	}
	req.Descending = false
	api.SortMetricContributors(rows, req)
	if rows[0].Entity.Key != "a" || rows[2].Entity.Key != "missing" {
		t.Fatal(rows)
	}
	for _, descending := range []bool{false, true} {
		req.Ranking = v1.MetricRanking_METRIC_RANKING_MAXIMUM
		req.Descending = descending
		api.SortMetricContributors(rows, req)
		if rows[2].Entity.Key != "missing" {
			t.Fatal("nonfinite value ranked as known")
		}
	}
}
func TestMetricContributorsHandlerBoundsAndReadAccess(t *testing.T) {
	h := &api.Handler{Systems: api.One("shop", "Shop", fake.New(false))}
	req := &v1.ListMetricContributorsRequest{System: "shop", Metric: "server.cpu_percent", Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-time.Hour)), End: timestamppb.New(fake.FixedNow)}, Limit: 1}
	got, err := h.ListMetricContributors(context.Background(), connect.NewRequest(proto.Clone(req).(*v1.ListMetricContributorsRequest)))
	if err != nil || len(got.Msg.Contributors) != 1 || got.Msg.TotalCount < 2 {
		t.Fatalf("%v %v", got, err)
	}
	if !investigation.AllowedProcedure("/natslens.v1.LensService/ListMetricContributors") || investigation.Method("ListMetricContributors") == nil {
		t.Fatal("read-only agent boundary did not admit ranking")
	}
	for _, change := range []func(*v1.ListMetricContributorsRequest){func(r *v1.ListMetricContributorsRequest) { r.System = "wrong" }, func(r *v1.ListMetricContributorsRequest) { r.Metric = "bad" }, func(r *v1.ListMetricContributorsRequest) { r.Ranking = 999 }, func(r *v1.ListMetricContributorsRequest) { r.Account = "APP" }, func(r *v1.ListMetricContributorsRequest) { r.Limit = -1 }, func(r *v1.ListMetricContributorsRequest) { r.PageToken = "bad" }, func(r *v1.ListMetricContributorsRequest) {
		r.Range.Start = timestamppb.New(fake.FixedNow.Add(-31 * 24 * time.Hour))
	}} {
		bad := proto.Clone(req).(*v1.ListMetricContributorsRequest)
		change(bad)
		if _, err = h.ListMetricContributors(context.Background(), connect.NewRequest(bad)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("invalid request accepted: %v %v", bad, err)
		}
	}
}
func TestMetricContributorsContextScopeAndImmutableReplay(t *testing.T) {
	r := &v1.ListMetricContributorsRequest{Metric: "server.cpu_percent", Ranking: v1.MetricRanking_METRIC_RANKING_MAXIMUM, Limit: 1, Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-time.Hour)), End: timestamppb.New(fake.FixedNow)}}
	makePages := func() *api.EntityPages {
		p := &api.EntityPages{}
		if err := p.InitImmutable("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
			t.Fatal(err)
		}
		return p
	}
	load := func() (*api.EntitySnapshot, error) {
		return &api.EntitySnapshot{Range: proto.Clone(r.Range).(*v1.TimeRange), MetricRows: []*v1.MetricContributor{{Entity: &v1.EntityRef{Key: "a"}}, {Entity: &v1.EntityRef{Key: "b"}}}}, nil
	}
	a, err := api.MetricContributorPage(context.Background(), makePages(), r, load)
	if err != nil {
		t.Fatal(err)
	}
	next := proto.Clone(r).(*v1.ListMetricContributorsRequest)
	next.PageToken = a.NextPageToken
	b, err := api.MetricContributorPage(context.Background(), makePages(), next, load)
	if err != nil || len(b.GetContributors()) != 1 || b.Contributors[0].Entity.Key != "b" {
		t.Fatalf("new process cannot rehydrate metric page: %v %v", b, err)
	}
	next.Descending = true
	if _, err = api.MetricContributorPage(context.Background(), makePages(), next, load); err == nil {
		t.Fatal("token reused under another sort")
	}
}
