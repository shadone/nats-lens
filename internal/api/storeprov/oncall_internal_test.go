package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

type countedOnCallDB struct {
	*sql.DB
	queries int
}

func (c *countedOnCallDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.queries++
	return c.DB.QueryContext(ctx, q, args...)
}
func (c *countedOnCallDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.queries++
	return c.DB.QueryRowContext(ctx, q, args...)
}

// Count actual fact queries, not the number of helper calls. Reads of
// finding/mute/acknowledgement records remain inside store's own methods.
func TestOnCallBatchQueryCount(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	if _, err = st.DB().Exec(`INSERT INTO epochs VALUES (1,?,?,0,3,0,['nats-0','nats-1','nats-2'],[],[],0,true,[],NULL)`, at, at); err != nil {
		t.Fatal(err)
	}
	spy := &countedOnCallDB{DB: st.DB()}
	p := &Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return at.Add(time.Second) }, readDB: spy}
	var baseline int
	for _, n := range []int{1, 10, 100} {
		// Keep a fixed episode and grow only its cardinality.
		if _, err = st.DB().Exec(`DELETE FROM check_runs; DELETE FROM finding_transitions; DELETE FROM findings`); err != nil {
			t.Fatal(err)
		}
		var rs []store.FindingRow
		for i := 0; i < n; i++ {
			rs = append(rs, store.FindingRow{EntityKey: fmt.Sprintf("APP/orders/worker-%03d", i), Severity: "warning", Detail: "no report"})
		}
		if _, err = st.ApplyCheckRuns(ctx, 1, []store.CheckRun{{Code: "consumer-010", EntityType: "consumer", Status: store.RunOK, Rows: rs}}); err != nil {
			t.Fatal(err)
		}
		spy.queries = 0
		f, err := p.OnCall(ctx, &v1.GetOnCallRequest{FindingId: "consumer-010:APP/orders/worker-000"})
		if err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			baseline = spy.queries
			if baseline == 0 {
				t.Fatal("no reads")
			}
		}
		if spy.queries != baseline {
			t.Fatalf("%d assets: %d queries, baseline %d", n, spy.queries, baseline)
		}
		card := api.FinishOnCall(f, nil, "")
		if card.AssetsTotal != int32(n) || len(card.Assets) != min(n, 10) {
			t.Fatal(card)
		}
	}
	t.Logf("%d fact queries for 1, 10 and 100 affected consumers", baseline)
}
