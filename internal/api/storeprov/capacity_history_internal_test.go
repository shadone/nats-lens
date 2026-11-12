package storeprov

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestCapacityHistoryBoundedReads(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	for _, q := range []string{
		`INSERT INTO epochs SELECT i+1,make_timestamp(1792224000000000+i*30000000),make_timestamp(1792224000000000+i*30000000),0,1,1,[],[],[],0,false,[],NULL FROM range(721) t(i)`,
		`INSERT INTO servers (server_id,server_name,first_seen_at,last_seen_at,first_epoch,last_epoch) VALUES ('S0','nats-0',TIMESTAMP '2026-10-17 08:00:00',TIMESTAMP '2026-10-17 14:00:00',1,721)`,
		`INSERT INTO server_stats (epoch,server_id,js_enabled,js_storage) SELECT epoch,'S0',true,epoch*100 FROM epochs`,
		`INSERT INTO stream_stats (epoch,server_id,account,stream,is_leader,bytes) SELECT epoch,'S0','APP','orders-' || j,true,epoch*10 FROM epochs,range(64) t(j)`,
	} {
		if _, err = st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	spy := &countedOnCallDB{DB: st.DB()}
	p := &Provider{St: st, Interval: 30 * time.Second, readDB: spy}
	r := &v1.TimeRange{Start: timestamppb.New(start.Add(15 * time.Second)), End: timestamppb.New(start.Add(6 * time.Hour))}
	baseline := map[string]*v1.Series{}
	for _, zone := range []string{"UTC", "Asia/Kolkata", "America/Los_Angeles"} {
		if _, err = st.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
			t.Fatal(err)
		}
		spy.queries = 0
		for _, tc := range []struct {
			metric string
			e      *v1.EntityRef
			last   float64
		}{
			{"cluster.capacity_file_store_bytes", nil, 72000},
			{"server.capacity_file_store_bytes", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0", Server: "nats-0"}, 72000},
			{"stream.observed_replica_bytes", &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/orders-0", Account: "APP", Stream: "orders-0"}, 7200},
		} {
			s, err := p.series(ctx, tc.metric, tc.e, r, 96)
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Points) == 0 || len(s.Points) > 97 {
				t.Fatalf("%s %s: %d rows", zone, tc.metric, len(s.Points))
			}
			for _, point := range s.Points {
				if point.Time.AsTime().Before(start) || !point.Time.AsTime().Before(r.End.AsTime()) {
					t.Fatalf("%s: point outside selected range: %v", zone, point)
				}
			}
			if first := baseline[tc.metric]; first != nil {
				if !proto.Equal(first, s) {
					t.Fatalf("%s %s: series differs from UTC", zone, tc.metric)
				}
			} else {
				baseline[tc.metric] = s
			}
			// The exclusive end never contributes epoch 721, even in non-UTC sessions.
			if s.GetMax() >= tc.last+10 {
				t.Fatalf("%s %s: end leaked: %v", zone, tc.metric, s.GetMax())
			}
		}
		// The stream series also checks that its epochs have stream samples.
		if spy.queries != 4 {
			t.Fatalf("%s: %d reads, want exactly one per series and the samples check", zone, spy.queries)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = p.series(canceled, "cluster.capacity_file_store_bytes", nil, r, 96); err == nil {
		t.Fatal("canceled read succeeded")
	}
}
