package api_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/timing"
)

// budgeted is the demo provider with a query budget; its series calls
// count how many run at once and, while want of them have not yet met,
// wait for each other.
type budgeted struct {
	*fake.Provider
	gate *admit.Gate
	want int
	// wait is how long a call waits for want of them to meet.
	wait time.Duration

	mu      sync.Mutex
	running int
	most    int
	met     chan struct{}
	once    sync.Once
}

func (b *budgeted) QueryBudget() *admit.Gate { return b.gate }

func (b *budgeted) GetTimeSeries(ctx context.Context, req *v1.GetTimeSeriesRequest) (*v1.GetTimeSeriesResponse, error) {
	b.mu.Lock()
	b.running++
	b.most = max(b.most, b.running)
	if b.running == b.want {
		b.once.Do(func() { close(b.met) })
	}
	b.mu.Unlock()
	select {
	case <-b.met:
	case <-time.After(b.wait):
	}
	defer func() { b.mu.Lock(); b.running--; b.mu.Unlock() }()
	return b.Provider.GetTimeSeries(ctx, req)
}

func batchOf(n int) []*v1.SeriesQuery {
	out := make([]*v1.SeriesQuery, n)
	for i := range out {
		out[i] = &v1.SeriesQuery{Metric: "cluster.connections"}
	}
	return out
}

// A batch reads four series at a time while the store's budget is idle and
// one at a time while it is busy, so that it never adds three queries to
// a store whose callers already wait.
func TestBatchParallelismFollowsTheBudget(t *testing.T) {
	// Four calls of an idle batch meet however slow the host; the wait
	// only bounds a broken batch.
	idle := &budgeted{Provider: fake.New(false), gate: admit.New(8, nil), want: 4, wait: 20 * time.Second, met: make(chan struct{})}
	_, c := apitest.Serve(t, idle)
	if _, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: batchOf(12)})); err != nil {
		t.Fatal(err)
	}
	if idle.most != 4 {
		t.Fatalf("on an idle store the batch read %d series at once, want 4", idle.most)
	}

	// Two calls of a busy batch never meet; each waits briefly so that a
	// batch that wrongly read in parallel would overlap them.
	busy := &budgeted{Provider: fake.New(false), gate: admit.New(1, nil), want: 2, wait: 200 * time.Millisecond, met: make(chan struct{})}
	hold, err := busy.gate.Enter(context.Background(), admit.Request{Source: timing.SourceAPI})
	if err != nil {
		t.Fatal(err)
	}
	defer hold()
	_, c = apitest.Serve(t, busy)
	if _, err := c.GetTimeSeriesBatch(context.Background(), connect.NewRequest(&v1.GetTimeSeriesBatchRequest{Queries: batchOf(6)})); err != nil {
		t.Fatal(err)
	}
	if busy.most != 1 {
		t.Fatalf("on a busy store the batch read %d series at once, want 1", busy.most)
	}
}

var _ api.QueryBudgeter = (*budgeted)(nil)
