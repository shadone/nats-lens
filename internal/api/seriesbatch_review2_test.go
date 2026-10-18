package api_test

// Cases from the second review of GetTimeSeriesBatch: the client's
// timeout and the other calls' QueryTimeout.

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The web client sends Connect-Timeout-Ms (45 s, CALL_TIMEOUT_MS); connect-go
// applies it to the handler's context, so the client's limit, not
// BatchTimeout, bounds every query of a batch.
func TestBatchReview2ClientTimeoutBoundsTheBatch(t *testing.T) {
	var mu sync.Mutex
	var left time.Duration
	p := &scripted{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
		d, ok := ctx.Deadline()
		mu.Lock()
		if ok {
			left = time.Until(d)
		}
		mu.Unlock()
		return &v1.GetTimeSeriesResponse{Series: &v1.Series{}}, nil
	}
	_, c := apitest.Serve(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.GetTimeSeriesBatch(ctx, connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: nQueries("cluster.connections")})); err != nil {
		t.Fatal(err)
	}
	if left > 3*time.Second {
		t.Errorf("query had %s left; want the client's 2 s to bound it", left)
	}
}

// Every other RPC still has QueryTimeout (30 s) in the provider.
func TestBatchReview2OtherCallsKeepQueryTimeout(t *testing.T) {
	var left time.Duration
	p := &scripted{Provider: fake.New(false)}
	p.fn = func(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
		d, _ := ctx.Deadline()
		left = time.Until(d)
		return &v1.GetTimeSeriesResponse{Series: &v1.Series{}}, nil
	}
	_, c := apitest.Serve(t, p)
	if _, err := c.GetTimeSeries(context.Background(), connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.connections"})); err != nil {
		t.Fatal(err)
	}
	if left > 30*time.Second || left < 29*time.Second {
		t.Errorf("GetTimeSeries had %s; want 30 s", left)
	}
}
