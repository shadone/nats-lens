package api_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func serverEntity(name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: name}
}

// batchQueries mixes scopes and a per-query max_points.
var batchQueries = []*v1.SeriesQuery{
	{Metric: "cluster.connections"},
	{Metric: "server.in_msgs_rate", Entity: serverEntity("nats-1")},
	{Metric: "consumer.num_pending", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/order-jobs/resize-worker"}, MaxPoints: 24},
	{Metric: "cluster.servers_up", MaxPoints: 1},
}

func TestBatchEqualsSingleCallsInOrder(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	ctx := context.Background()
	res, err := c.GetTimeSeriesBatch(ctx, connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: batchQueries, MaxPoints: 60}))
	if err != nil {
		t.Fatal(err)
	}
	got := res.Msg.GetResults()
	if len(got) != len(batchQueries) {
		t.Fatalf("got %d results, want %d", len(got), len(batchQueries))
	}
	for i, q := range batchQueries {
		mp := q.GetMaxPoints()
		if mp == 0 {
			mp = 60
		}
		one, err := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{
			Metric: q.GetMetric(), Entity: q.GetEntity(), Range: res.Msg.GetRange(), MaxPoints: mp,
		}))
		if err != nil {
			t.Fatalf("%s: %v", q.GetMetric(), err)
		}
		if got[i].GetError() != "" {
			t.Fatalf("result %d: unexpected error %q", i, got[i].GetError())
		}
		if !proto.Equal(got[i].GetSeries(), one.Msg.GetSeries()) {
			t.Errorf("result %d (%s) differs from GetTimeSeries", i, q.GetMetric())
		}
	}
}

func TestBatchBadQueryFailsAlone(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	queries := []*v1.SeriesQuery{
		{Metric: "cluster.connections"},
		{Metric: "cluster.no_such_metric"},
		{Metric: "server.in_msgs_rate", Entity: serverEntity("nats-99")},
		{Metric: "server.in_msgs_rate"},
		{Metric: "server.in_msgs_rate", Entity: serverEntity("nats-0")},
	}
	res, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: queries}))
	if err != nil {
		t.Fatal(err)
	}
	r := res.Msg.GetResults()
	for _, i := range []int{0, 4} {
		if r[i].GetError() != "" || len(r[i].GetSeries().GetPoints()) == 0 {
			t.Errorf("result %d: error %q, %d points; want a served series", i, r[i].GetError(), len(r[i].GetSeries().GetPoints()))
		}
	}
	for i, want := range map[int]string{1: "unknown metric", 2: "not found", 3: "needs an entity"} {
		if !strings.Contains(r[i].GetError(), want) || r[i].GetSeries() != nil {
			t.Errorf("result %d: error %q, series %v; want an error with %q", i, r[i].GetError(), r[i].GetSeries(), want)
		}
	}
}

func TestBatchQueryCount(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	for _, n := range []int{0, api.MaxBatchQueries + 1} {
		q := make([]*v1.SeriesQuery, n)
		for i := range q {
			q[i] = &v1.SeriesQuery{Metric: "cluster.connections"}
		}
		_, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q}))
		if codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%d queries: got %v, want invalid_argument", n, err)
		}
	}
	q := make([]*v1.SeriesQuery, api.MaxBatchQueries)
	for i := range q {
		q[i] = &v1.SeriesQuery{Metric: "cluster.connections"}
	}
	res, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q}))
	if err != nil || len(res.Msg.GetResults()) != api.MaxBatchQueries {
		t.Errorf("%d queries: %v", api.MaxBatchQueries, err)
	}
}

// recording is a fake whose clock moves a minute on every read, and which
// records the range of each GetTimeSeries call. A metric named in fail
// returns an unexpected error.
type recording struct {
	*fake.Provider
	mu     sync.Mutex
	now    time.Time
	ranges []*v1.TimeRange
	fail   string
}

func (r *recording) Now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = r.now.Add(time.Minute)
	return r.now
}

func (r *recording) GetTimeSeries(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
	r.mu.Lock()
	r.ranges = append(r.ranges, req.GetRange())
	r.mu.Unlock()
	if req.GetMetric() == r.fail {
		return nil, errors.New("disk on fire")
	}
	return r.Provider.GetTimeSeries(ctx, req)
}

func TestBatchSharesOneRange(t *testing.T) {
	p := &recording{Provider: fake.New(false), now: fake.FixedNow}
	_, c := apitest.Serve(t, p)
	res, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: batchQueries}))
	if err != nil {
		t.Fatal(err)
	}
	want := res.Msg.GetRange()
	if want.GetEnd() == nil || want.GetEnd().AsTime().Sub(want.GetStart().AsTime()) != api.DefaultRange {
		t.Fatalf("range %v, want %s ending at the resolved now", want, api.DefaultRange)
	}
	if len(p.ranges) != len(batchQueries) {
		t.Fatalf("%d provider calls, want %d", len(p.ranges), len(batchQueries))
	}
	for i, r := range p.ranges {
		if !proto.Equal(r, want) {
			t.Errorf("call %d read %v, want %v", i, r, want)
		}
	}
}

func TestBatchUnexpectedErrorFailsTheCall(t *testing.T) {
	p := &recording{Provider: fake.New(false), now: fake.FixedNow, fail: "server.in_msgs_rate"}
	_, c := apitest.Serve(t, p)
	_, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: batchQueries}))
	if codeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("got %v, want internal without the provider's text", err)
	}
}

func TestBatchRoutesBySystem(t *testing.T) {
	second := &recording{Provider: fake.New(false), now: fake.FixedNow}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.Systems{
		{Ref: &v1.SystemRef{Id: "a"}, Provider: fake.New(false)},
		{Ref: &v1.SystemRef{Id: "b"}, Provider: second},
	}})
	q := []*v1.SeriesQuery{{Metric: "cluster.connections"}}
	if _, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q, System: "b"})); err != nil {
		t.Fatal(err)
	}
	if len(second.ranges) != 1 {
		t.Errorf("system b read %d series, want 1", len(second.ranges))
	}
	_, single := c.GetTimeSeries(context.Background(), connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.connections", System: "nope"}))
	_, batch := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q, System: "nope"}))
	if codeOf(batch) != codeOf(single) || codeOf(batch) != connect.CodeInvalidArgument {
		t.Errorf("unknown system: batch %v, single %v; want both invalid_argument", batch, single)
	}
}

func TestBatchExplicitRange(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	end := fake.FixedNow.Add(-time.Hour)
	r := &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}
	res, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{
		Queries: []*v1.SeriesQuery{{Metric: "cluster.connections"}}, Range: r,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(res.Msg.GetRange(), r) {
		t.Errorf("range %v, want %v", res.Msg.GetRange(), r)
	}
	bad := &v1.TimeRange{Start: timestamppb.New(end), End: timestamppb.New(end)}
	_, err = c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{
		Queries: []*v1.SeriesQuery{{Metric: "cluster.connections"}}, Range: bad,
	}))
	if codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("empty range: got %v, want invalid_argument", err)
	}
}

// panicking panics on one metric.
type panicking struct{ *fake.Provider }

func (p panicking) GetTimeSeries(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
	if req.GetMetric() == "server.in_msgs_rate" {
		panic("boom")
	}
	return p.Provider.GetTimeSeries(ctx, req)
}

func TestBatchPanicFailsTheCallNotTheProcess(t *testing.T) {
	var logs syncBuffer
	_, c := apitest.ServeHandler(t, &api.Handler{
		Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, panicking{fake.New(false)}),
		Log:     slog.New(slog.NewTextHandler(&logs, nil)),
	})
	_, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: batchQueries}))
	if codeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "boom") {
		t.Errorf("got %v, want internal without the panic's text", err)
	}
	if !strings.Contains(logs.String(), "server.in_msgs_rate panicked: boom") {
		t.Errorf("log %q does not name the metric and the panic", logs.String())
	}
	if _, err := c.GetTimeSeries(context.Background(), connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.connections"})); err != nil {
		t.Errorf("server after the panic: %v", err)
	}
}
