package storeprov_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/timing"
)

// Every API read that runs in a read transaction waits for the query
// budget like any other API read. Each case first answers with the budget
// free, so that its error with the only slot held can only be the wait.
func TestReadTransactionsWaitForTheBudget(t *testing.T) {
	r := func() *v1.TimeRange {
		return &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(2 * time.Minute))}
	}
	stream := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "AFAKEAPPACCOUNT/order-jobs"}
	cases := []struct {
		name string
		read func(context.Context, *storeprov.Provider) error
	}{
		{"timeline", func(ctx context.Context, p *storeprov.Provider) error {
			_, err := p.GetStatusTimeline(ctx, &v1.GetStatusTimelineRequest{Range: r(), Bucket: durationpb.New(30 * time.Second)})
			return err
		}},
		{"observation series", func(ctx context.Context, p *storeprov.Provider) error {
			_, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: "stream.bytes", Entity: stream, Range: r(), MaxPoints: 4, IncludeObservations: true})
			return err
		}},
		{"storage change", func(ctx context.Context, p *storeprov.Provider) error {
			_, err := p.StorageChange(ctx, &v1.GetStorageChangeRequest{A: timestamppb.New(t0.Add(30 * time.Second)), B: timestamppb.New(t0.Add(time.Minute))})
			return err
		}},
		{"contributors", func(ctx context.Context, p *storeprov.Provider) error {
			_, err := p.ListMetricContributors(ctx, &v1.ListMetricContributorsRequest{Metric: "stream.bytes", Range: r(), Limit: 5})
			return err
		}},
		{"finding episodes", func(ctx context.Context, p *storeprov.Provider) error {
			_, err := p.ListFindingEpisodes(ctx, &v1.ListFindingEpisodesRequest{Range: r(), PageSize: 10})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := budgetedStore(t, "tx-budget", 1)
			p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return t0.Add(2 * time.Minute) }}
			api := func(ctx context.Context) context.Context {
				return admit.WithFlow(timing.WithSource(ctx, timing.SourceAPI))
			}
			if err := c.read(api(context.Background()), p); err != nil {
				t.Fatalf("with the budget free: %v", err)
			}
			hctx, hcancel := context.WithCancel(api(context.Background()))
			defer hcancel()
			go func() {
				var v any
				_ = st.DB().QueryRowContext(hctx, `SELECT sum(i) FROM range(1000000000000) t(i)`).Scan(&v)
			}()
			waitUntil(t, "slot taken", func() bool { a, _ := st.QueryBudget().Load(); return a == 1 })
			rctx, rcancel := context.WithTimeout(api(context.Background()), 2*time.Second)
			defer rcancel()
			err := c.read(rctx, p)
			if !errors.Is(err, admit.ErrOverloaded) {
				t.Fatalf("with the only slot held the read answered %v, want a refusal for the budget", err)
			}
		})
	}
}
