package storeprov

import (
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/store"
	"testing"
	"time"
)

func TestReplayClientRollupBoundedQueries(t *testing.T) {
	st, err := store.OpenPath(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db := st.DB()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) VALUES ('S','nats-0',1,2,now(),now())`)
	exec(`INSERT INTO epochs SELECT e,now(),now(),0,1,1,['nats-0'],['nats-0'],[],0,false,[],NULL FROM range(1,3) r(e)`)
	exec(`INSERT INTO epoch_endpoints SELECT e,'S','CONNZ','ok',NULL FROM range(1,3) r(e)`)
	spy := &countedOnCallDB{DB: db}
	p := &Provider{St: st, readDB: spy}
	for _, n := range []int{1, 500, 5000} {
		exec(`DELETE FROM connections; DELETE FROM server_stats`)
		exec(`INSERT INTO server_stats(epoch,server_id,connz_total,connz_returned) SELECT e,'S',?,? FROM range(1,3) r(e)`, n, n)
		exec(`INSERT INTO connections(epoch,server_id,cid,name,account,lang,version,ip) SELECT e,'S',i,'app-'||i||'@pod','APP','go',CASE WHEN e=1 THEN '1.0' ELSE '1.1' END,'10.0.0.1' FROM range(1,3) r(e),range(?) x(i)`, n)
		f := &api.ReplayFacts{EpochA: 1, EpochB: 2}
		spy.queries = 0
		start := time.Now()
		if err := p.replayClientLibraries(t.Context(), f, nil); err != nil {
			t.Fatal(err)
		}
		if spy.queries != 2 || len(f.ClientLibraries.Rows) != 2*n || f.ClientLibraries.Unavailable != "" {
			t.Fatalf("n=%d queries=%d facts=%+v", n, spy.queries, f.ClientLibraries)
		}
		t.Logf("%d applications: 2 queries, %s", n, time.Since(start))
	}
	same := &api.ReplayFacts{EpochA: 1, EpochB: 1}
	spy.queries = 0
	if err := p.replayClientLibraries(t.Context(), same, nil); err != nil {
		t.Fatal(err)
	}
	if spy.queries != 0 || same.ClientLibraries.Unavailable == "" {
		t.Fatal("same collection treated as two observations")
	}
	old := replayRowLimit
	replayRowLimit = 3
	t.Cleanup(func() { replayRowLimit = old })
	f := &api.ReplayFacts{EpochA: 1, EpochB: 2}
	if err := p.replayClientLibraries(t.Context(), f, nil); err != nil {
		t.Fatal(err)
	}
	if f.ClientLibraries.Unavailable == "" || len(f.ClientLibraries.Rows) != 0 {
		t.Fatal("read limit silently compared partial rollups")
	}
}
