package storeprov

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/admit"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/timing"
)

// With a query budget of one slot, the asset-count reads (the
// missing-counts check, then the read or the scan) finish inside and
// outside a read transaction, and give every slot back.
func TestAssetCountsUnderABudgetOfOne(t *testing.T) {
	p := assetProvider(t)
	p.St.SetQueryBudget(1)
	sys := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	run := func(stage string) {
		for _, r := range assetRanges() {
			ctx, cancel := context.WithTimeout(admit.WithFlow(timing.WithSource(context.Background(), timing.SourceAPI)), 20*time.Second)
			for metric := range referenceAssetQueries {
				if _, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: metric, Entity: sys, Range: r, MaxPoints: 7, IncludeObservations: true}); err != nil {
					t.Fatalf("%s: tx %s: %v", stage, metric, err)
				}
				if _, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: metric, Entity: sys, Range: r, MaxPoints: 7}); err != nil {
					t.Fatalf("%s: plain %s: %v", stage, metric, err)
				}
			}
			if _, err := p.GetOverview(ctx, &v1.GetOverviewRequest{Range: r, MaxPoints: 7}); err != nil {
				t.Fatalf("%s: overview: %v", stage, err)
			}
			cancel()
			if a, _ := p.St.QueryBudget().Load(); a != 0 {
				t.Fatalf("%s: %d slots still held", stage, a)
			}
		}
	}
	run("counts")
	if _, err := p.St.DB().Exec(`DELETE FROM epoch_asset_counts WHERE epoch % 3 = 0`); err != nil {
		t.Fatal(err)
	}
	run("fallback")
}
