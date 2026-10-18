package api_test

// Cases from the review of GetTimeSeriesBatch: cancellation, parallelism,
// panics and max_points.

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// scripted runs fn for every GetTimeSeries call.
type scripted struct {
	*fake.Provider
	fn func(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error)
}

func (s *scripted) GetTimeSeries(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
	return s.fn(ctx, req)
}

func nQueries(metrics ...string) []*v1.SeriesQuery {
	q := make([]*v1.SeriesQuery, len(metrics))
	for i, m := range metrics {
		q[i] = &v1.SeriesQuery{Metric: m}
	}
	return q
}

// A fatal error cancels the queries in flight and stops dispatching more.
func TestBatchReviewFatalCancelsOthers(t *testing.T) {
	var started, canceled atomic.Int32
	p := &scripted{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
		started.Add(1)
		if req.GetMetric() == "cluster.connections" {
			time.Sleep(50 * time.Millisecond)
			return nil, errors.New("disk on fire")
		}
		select {
		case <-ctx.Done():
			canceled.Add(1)
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return nil, errors.New("not canceled")
		}
	}
	_, c := apitest.Serve(t, p)
	q := nQueries("cluster.connections", "cluster.servers_up", "cluster.servers_up", "cluster.servers_up",
		"cluster.servers_up", "cluster.servers_up", "cluster.servers_up", "cluster.servers_up")
	t0 := time.Now()
	_, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q}))
	if d := time.Since(t0); d > 3*time.Second {
		t.Fatalf("took %s", d)
	}
	if codeOf(err) != connect.CodeInternal {
		t.Fatalf("got %v, want internal (the first error, not canceled)", err)
	}
	if started.Load() != 4 || canceled.Load() != 3 {
		t.Errorf("started %d canceled %d; want 4 and 3", started.Load(), canceled.Load())
	}
}

func TestBatchReviewParallelismAndOrder(t *testing.T) {
	var cur, peak atomic.Int32
	p := &scripted{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
		n := cur.Add(1)
		for {
			o := peak.Load()
			if n <= o || peak.CompareAndSwap(o, n) {
				break
			}
		}
		// Later queries finish first.
		time.Sleep(time.Duration(40-req.GetMaxPoints()) * time.Millisecond)
		cur.Add(-1)
		return &v1.GetTimeSeriesResponse{Series: &v1.Series{Metric: fmt.Sprint(req.GetMaxPoints())}}, nil
	}
	q := make([]*v1.SeriesQuery, api.MaxBatchQueries)
	for i := range q {
		q[i] = &v1.SeriesQuery{Metric: "cluster.connections", MaxPoints: int32(i + 1)}
	}
	_, c := apitest.Serve(t, p)
	res, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q}))
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 4 || peak.Load() < 2 {
		t.Errorf("peak concurrency %d, want 2..4", peak.Load())
	}
	for i, r := range res.Msg.GetResults() {
		if want := fmt.Sprint(i + 1); r.GetSeries().GetMetric() != want {
			t.Errorf("result %d is %q, want %q", i, r.GetSeries().GetMetric(), want)
		}
	}
}

func TestBatchReviewAllPanicNoHang(t *testing.T) {
	p := &scripted{Provider: fake.New(false)}
	p.fn = func(context.Context, *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) { panic("boom") }
	_, c := apitest.Serve(t, p)
	q := make([]*v1.SeriesQuery, api.MaxBatchQueries)
	for i := range q {
		q[i] = &v1.SeriesQuery{Metric: "cluster.connections"}
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q}))
		done <- err
	}()
	select {
	case err := <-done:
		if codeOf(err) != connect.CodeInternal {
			t.Errorf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hung")
	}
}

// A client that goes away cancels the workers, and no goroutine is left.
func TestBatchReviewClientCancelNoLeak(t *testing.T) {
	// A counter, not a WaitGroup: a worker may enter after the client gave
	// up, and WaitGroup.Add racing Wait is a misuse the race detector flags.
	var running atomic.Int32
	p := &scripted{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
		running.Add(1)
		defer running.Add(-1)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	srv, c := apitest.Serve(t, p)
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	q := make([]*v1.SeriesQuery, 16)
	for i := range q {
		q[i] = &v1.SeriesQuery{Metric: "cluster.connections"}
	}
	_, err := c.GetTimeSeriesBatch(ctx, connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: q}))
	if err == nil {
		t.Fatal("want an error")
	}
	stop := time.Now().Add(3 * time.Second)
	for running.Load() > 0 && time.Now().Before(stop) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := running.Load(); n > 0 {
		t.Fatalf("%d workers still running after the client went away", n)
	}
	// The connection the cancelled call left idle has goroutines of its
	// own on both ends; close it so that only a leak is counted.
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		srv.Client().CloseIdleConnections()
		time.Sleep(20 * time.Millisecond)
	}
	if g := runtime.NumGoroutine(); g > before+2 {
		t.Errorf("goroutines %d, before %d", g, before)
	}
}

func TestBatchReviewMaxPointsChecks(t *testing.T) {
	_, c := apitest.Serve(t, fake.New(false))
	res, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{
		Queries: []*v1.SeriesQuery{{Metric: "cluster.connections", MaxPoints: -1}, {Metric: "cluster.connections", MaxPoints: 5000}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.GetResults()[0].GetError() == "" {
		t.Errorf("negative per-query max_points: no error")
	}
	if res.Msg.GetResults()[1].GetError() != "" {
		t.Errorf("clamped max_points: %q", res.Msg.GetResults()[1].GetError())
	}
	_, err = c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{
		Queries: []*v1.SeriesQuery{{Metric: "cluster.connections"}}, MaxPoints: -1,
	}))
	if codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("negative batch max_points: %v", err)
	}
}
