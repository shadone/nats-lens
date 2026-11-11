package storeprov_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/timing"
)

// The provider's Timeline read must wait for the query budget like any
// other API read. Its queries run in a read transaction, which the budget
// skips.
func TestTimelineWaitsForTheBudget(t *testing.T) {
	st := budgetedStore(t, "review-tx-budget", 1)
	prov := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return t0.Add(2 * time.Minute) }}
	hctx, hcancel := context.WithCancel(admit.WithFlow(timing.WithSource(context.Background(), timing.SourceAPI)))
	defer hcancel()
	go func() {
		var v any
		_ = st.DB().QueryRowContext(hctx, `SELECT sum(i) FROM range(1000000000000) t(i)`).Scan(&v)
	}()
	waitUntil(t, "slot taken", func() bool { a, _ := st.QueryBudget().Load(); return a == 1 })

	rctx, rcancel := context.WithTimeout(admit.WithFlow(timing.WithSource(context.Background(), timing.SourceAPI)), 3*time.Second)
	defer rcancel()
	r := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(2 * time.Minute))}
	res, err := prov.GetStatusTimeline(rctx, &v1.GetStatusTimelineRequest{Range: r, Bucket: durationpb.New(30 * time.Second)})
	t.Logf("timeline: %d rows, err %v", len(res.GetRows()), err)
	if err == nil {
		t.Fatal("the Timeline read answered while the system's only slot was held: its queries skipped the budget")
	}
}
