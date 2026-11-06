package storeprov

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDataHistoryBoundaryKeepsFourQueries(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	if _, err = st.DB().Exec(`INSERT INTO epochs SELECT i, make_timestamp(?::BIGINT + i * 30000000), make_timestamp(?::BIGINT + i * 30000000), 0, 1, 1, [], [], [], 0, false, [], NULL FROM range(1, 3001) t(i)`, now.Add(-25*time.Hour).UnixMicro(), now.Add(-25*time.Hour).UnixMicro()); err != nil {
		t.Fatal(err)
	}
	spy := &countedOnCallDB{DB: st.DB()}
	p := &Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return now }, readDB: spy}
	f, err := p.DataHistory(ctx, &v1.GetDataHistoryRequest{Range: &v1.TimeRange{Start: timestamppb.New(now.Add(-time.Hour)), End: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	if spy.queries != 4 {
		t.Fatalf("%d queries, want 4", spy.queries)
	}
	for _, w := range api.HistoryUseWindows(f) {
		sample := f.Samples[w.ID]
		if w.ID == "growth" && !sample.Before.IsZero() {
			t.Fatal("growth read a boundary predecessor")
		}
		if w.ID == "replay" && (sample.Before.IsZero() || !sample.Before.Before(w.Start)) {
			t.Fatalf("missing observed predecessor: %v", sample)
		}
	}
}
