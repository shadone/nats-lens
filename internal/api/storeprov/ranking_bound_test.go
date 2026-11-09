package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type rateBoundDB struct {
	*sql.DB
	query string
	args  []any
	reads int
}

func (d *rateBoundDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if strings.Contains(q, "/* delivered_rate_summary */") {
		d.query, d.args = q, append([]any(nil), args...)
		d.reads++
	}
	return d.DB.QueryContext(ctx, q, args...)
}

func TestHistoricalRateRankingSQLAndMaterializationBound(t *testing.T) {
	p, _, r := pageScaleProvider(t, api.MaxListRankingRows+50)
	for _, q := range []string{
		`DELETE FROM consumer_stats WHERE epoch=37 AND consumer <> 'worker-00000'`,
		`UPDATE epochs SET started_at=TIMESTAMP '2026-10-17 13:58:00' WHERE epoch=35`,
		`UPDATE epochs SET started_at=TIMESTAMP '2026-10-17 13:59:00' WHERE epoch=36`,
	} {
		if _, err := p.St.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	spy := &rateBoundDB{DB: p.St.DB()}
	p.readDB = spy
	res, err := p.ListConsumers(context.Background(), &v1.ListConsumersRequest{Range: r, Limit: 1})
	if !errors.Is(err, api.ErrResourceExhausted) || res != nil {
		t.Fatalf("response=%v error=%v", res, err)
	}
	if spy.reads != 1 || !strings.Contains(spy.query, "LIMIT ?") || spy.args[len(spy.args)-1] != api.MaxListRankingRows+1 {
		t.Fatalf("rate reads=%d query=%s args=%v", spy.reads, spy.query, spy.args)
	}
	// Execute the captured production query independently: the database returns
	// at most the budget plus one overflow row even with more retained identities.
	rows, err := spy.DB.QueryContext(context.Background(), spy.query, spy.args...)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		n++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if n != api.MaxListRankingRows+1 {
		t.Fatalf("SQL returned %d rows", n)
	}
	// Exercise the same scanner at a small bound: it rejects before adding row 3.
	rates, err := p.deliveredRatesLimited(context.Background(), r, "", "", "", 2)
	if !errors.Is(err, api.ErrResourceExhausted) || rates != nil {
		t.Fatalf("rates=%v error=%v", rates, err)
	}
	rates, err = p.deliveredRatesLimited(context.Background(), r, "APP", "orders-00000", "worker-00001", 2)
	if err != nil || len(rates) != 1 || rates["APP/orders-00000/worker-00001"] <= 0 {
		t.Fatalf("scoped rates=%v error=%v", rates, err)
	}
	for i, want := range []string{"APP", "APP", "orders-00000", "orders-00000", "worker-00001", "worker-00001"} {
		if spy.args[i+2] != want {
			t.Fatalf("scope args=%v", spy.args)
		}
	}
	rates, err = p.deliveredRatesLimited(context.Background(), r, "OTHER", "", "", 2)
	if err != nil || len(rates) != 0 {
		t.Fatalf("cross-account rates=%v error=%v", rates, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = p.deliveredRatesLimited(ctx, r, "", "", "", 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	t.Logf("historical identities=%d, production SQL rows=%d, production map admits at most %d before rejecting overflow", api.MaxListRankingRows+50, n, api.MaxListRankingRows)
}
