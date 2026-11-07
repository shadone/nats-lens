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

func TestGrowthQueriesStayBatchedAndBounded(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	if _, err = st.DB().Exec(`INSERT INTO epochs SELECT i+1, make_timestamp(?::BIGINT + i*30000000), make_timestamp(?::BIGINT + i*30000000), 0, 1, 1, [], [], [], 0, false, [], NULL FROM range(721) t(i)`, first.UnixMicro(), first.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB().Exec(`INSERT INTO servers (server_id,server_name,first_seen_at,last_seen_at,first_epoch,last_epoch) VALUES ('S0','nats-0',?, ?,1,721)`, first, first.Add(6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB().Exec(`INSERT INTO server_stats (epoch,server_id,js_enabled,js_storage) SELECT epoch,'S0',true,epoch*100 FROM epochs`); err != nil {
		t.Fatal(err)
	}
	spy := &countedOnCallDB{DB: st.DB()}
	p := &Provider{St: st, Interval: 30 * time.Second, readDB: spy}
	for _, n := range []int{1, 64} {
		if _, err = st.DB().Exec(`DELETE FROM stream_stats; INSERT INTO stream_stats (epoch,server_id,account,stream,is_leader,bytes,messages) SELECT e.epoch,'S0','APP','orders-' || j,true,e.epoch*100,e.epoch*10 FROM epochs e, range(?) t(j)`, n); err != nil {
			t.Fatal(err)
		}
		for _, zone := range []string{"UTC", "Asia/Kolkata", "America/Los_Angeles"} {
			if _, err = st.DB().Exec(`SET GLOBAL TimeZone = '` + zone + `'`); err != nil {
				t.Fatal(err)
			}
			start, end := first.Add(15*time.Second), first.Add(6*time.Hour+15*time.Second)
			spy.queries = 0
			bytes, err := p.streamGrowth(ctx, start, end, "", "")
			if err != nil {
				t.Fatal(err)
			}
			both, err := p.streamPoints(ctx, start, end, "", "")
			if err != nil {
				t.Fatal(err)
			}
			servers, err := p.serverStoreGrowth(ctx, start, end)
			if err != nil {
				t.Fatal(err)
			}
			storage, err := p.storageGrowth(ctx, &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(end)})
			if err != nil {
				t.Fatal(err)
			}
			if a := api.RobustPerDay(servers["nats-0"]); a == nil || *a != 288000 || storage == nil || *storage != *a {
				t.Fatalf("%s: server %v / storage %v", zone, a, storage)
			}
			if spy.queries != 4 || len(bytes) != n || len(both) != n {
				t.Fatalf("%s / %d: queries=%d bytes=%d points=%d", zone, n, spy.queries, len(bytes), len(both))
			}
			for key, pts := range bytes {
				if len(pts) > api.GrowthBuckets+1 {
					t.Errorf("%s: %d bucket rows", key, len(pts))
				}
				a, b := api.RobustPerDay(pts), api.RobustPerDay(both[key].bytes)
				if a == nil || b == nil || *a != 288000 || *a != *b {
					t.Errorf("%s / %s: growth %v / %v", zone, key, a, b)
				}
				for _, sm := range pts {
					if sm.From.Before(api.GrowthStart(start, end, p.Interval)) || !sm.To.Before(end) {
						t.Fatalf("sample outside bounded query: %+v", sm)
					}
				}
			}
			// The selected end is exclusive, even when a collection lies exactly there.
			edge, err := p.streamGrowth(ctx, first, first.Add(6*time.Hour), "APP", "orders-0")
			if err != nil {
				t.Fatal(err)
			}
			if got := api.RobustPerDay(edge["APP/orders-0"]); got != nil {
				t.Fatalf("exclusive end supplied an observation: %v", *got)
			}
		}
	}
}
