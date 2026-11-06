package checks

import (
	"fmt"
	"testing"
	"time"
)

func TestSourceHealthScale(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 4.3 million source reports")
	}
	const n, k = 2880, 500
	f := newFx(t)
	f.server("S0", "nats-0", 1, n, t0.Add(-24*time.Hour), "2.15.0")
	f.exec(fmt.Sprintf(`INSERT INTO epochs SELECT e, TIMESTAMP '2026-10-17 09:00:00' + to_seconds(30*(e-1)),
 TIMESTAMP '2026-10-17 09:00:00' + to_seconds(30*(e-1)),100,1,1,['nats-0'],['nats-0'],[]::VARCHAR[],0,false,[],NULL FROM range(1,%d) t(e)`, n+1))
	f.exec(fmt.Sprintf(`INSERT INTO stream_stats (epoch,server_id,account,stream,is_leader,source_config_known,created,leader_since)
 SELECT e,'S0','APP','combined-'||i,true,true,TIMESTAMP '2026-10-16 09:00:00',TIMESTAMP '2026-10-16 09:00:00'
 FROM range(1,%d) t(e),range(%d) u(i)`, n+1, k))
	f.exec(fmt.Sprintf(`INSERT INTO stream_source_stats (epoch,server_id,account,stream,source_id,name,config,reported,active_ns,error)
 SELECT e,'S0','APP','combined-'||i,'source-'||j,'orders','{}',true,
 CASE WHEN j=0 THEN 180000000000 ELSE 1000000000 END,
 CASE WHEN j=1 AND e%%2=0 THEN 'origin missing' END
 FROM range(1,%d) t(e),range(%d) u(i),range(3) v(j)`, n+1, k))
	for _, code := range []string{"stream-022", "stream-023"} {
		start := time.Now()
		rows := f.eval(code, n)
		took := time.Since(start)
		t.Logf("%s: %d streams, %d source reports: %d findings in %s", code, k, n*k*3, len(rows), took)
		if len(rows) != k {
			t.Errorf("%s: %d findings, want %d", code, len(rows), k)
		}
		if took > 4*time.Second {
			t.Errorf("%s took %s, want <4s under framework's 5s timeout", code, took)
		}
	}
}
