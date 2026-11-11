package perf

import (
	"context"
	"testing"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// -query-budget follows query_budget: -1 off, 0 derived from the threads,
// a positive number as given.
func TestQueryBudgetFlagFollowsTheConfig(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct{ flag, want int }{{-1, 0}, {0, config.Config{}.QueryBudgetFor(6)}, {3, 3}} {
		st, err := store.OpenPath(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := applySettings(ctx, st, LoadOptions{QueryBudget: c.flag, Threads: 6}); err != nil {
			t.Fatal(err)
		}
		got := 0
		if g := st.QueryBudget(); g != nil {
			got = g.Limit()
		}
		_ = st.Close()
		if got != c.want {
			t.Errorf("-query-budget %d gave a budget of %d, want %d", c.flag, got, c.want)
		}
	}
}
