package storeprov

import (
	"context"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSlowConsumerInvestigationQueryCountAndCancellation(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	if _, err = st.DB().Exec(`INSERT INTO epochs SELECT i, make_timestamp(?::BIGINT+i*30000000), make_timestamp(?::BIGINT+i*30000000),0,1,1,[],[],[],0,false,[],NULL FROM range(1,3001)t(i)`, now.Add(-25*time.Hour).UnixMicro(), now.Add(-25*time.Hour).UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB().Exec(`INSERT INTO events(id,time,received_at,source,kind,summary) VALUES (1,?,?,'collector','collector.started','started')`, now.Add(-26*time.Hour), now.Add(-26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	spy := &countedOnCallDB{DB: st.DB()}
	p := &Provider{St: st, Interval: 30 * time.Second, readDB: spy}
	req := &v1.GetSlowConsumerInvestigationRequest{Range: &v1.TimeRange{Start: timestamppb.New(now.Add(-24 * time.Hour)), End: timestamppb.New(now)}, Limit: 100}
	if _, err = p.GetSlowConsumerInvestigation(ctx, req); err != nil {
		t.Fatal(err)
	}
	if spy.queries != 6 {
		t.Fatalf("%d queries for 3000 epochs; want 6", spy.queries)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = p.GetSlowConsumerInvestigation(canceled, req); err == nil {
		t.Fatal("canceled query succeeded")
	}
}
