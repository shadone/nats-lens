package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type metadataRead struct {
	query string
	args  []any
}
type metadataDB struct {
	*sql.DB
	reads []metadataRead
}

func (d *metadataDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if strings.HasPrefix(q, "WITH metadata AS") {
		d.reads = append(d.reads, metadataRead{q, append([]any(nil), args...)})
	}
	return d.DB.QueryContext(ctx, q, args...)
}
func execMetadata(t *testing.T, p *Provider, qs ...string) {
	t.Helper()
	for _, q := range qs {
		if _, err := p.St.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListMetadataRetainedKeysAndPlacementMatrix(t *testing.T) {
	p, _, r := pageScaleProvider(t, 3)
	execMetadata(t, p,
		`UPDATE stream_stats SET stream=CASE stream WHEN 'orders-00001' THEN 'KV_stock' WHEN 'orders-00002' THEN 'OBJ_images' ELSE stream END`,
		`INSERT INTO stream_stats SELECT * REPLACE ('OTHER' AS account) FROM stream_stats WHERE epoch=37`,
		`INSERT INTO stream_config(server_id,account,stream,first_epoch,last_epoch,config_hash,replicas) SELECT 'S0',account,stream,1,37,'h',3 FROM stream_stats WHERE epoch=37`,
		`INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current,offline,lag) SELECT 37,'S0',account,stream,'missing-offline',NULL,true,NULL FROM stream_stats WHERE epoch=37`,
		`INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current,offline,lag) SELECT 37,'S0',account,stream,'missing-unknown',NULL,NULL,NULL FROM stream_stats WHERE epoch=37`)
	spy := &metadataDB{DB: p.St.DB()}
	p.readDB = spy
	for _, account := range []string{"", "APP", "OTHER", "absent"} {
		for _, hidden := range []bool{false, true} {
			for _, summary := range []bool{false, true} {
				spy.reads = nil
				res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Account: account, IncludeKv: hidden, SummaryOnly: summary, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if hidden {
					want = 3
				}
				if account == "" {
					want *= 2
				}
				if account == "absent" {
					want = 0
				}
				if int(res.TotalCount) != want {
					t.Fatalf("scope %q hidden %v: %d != %d", account, hidden, res.TotalCount, want)
				}
				if want > 0 {
					got := res.Streams[0].Placement
					states := []v1.PlacementState{v1.PlacementState_PLACEMENT_STATE_LEADER, v1.PlacementState_PLACEMENT_STATE_OFFLINE, v1.PlacementState_PLACEMENT_STATE_MISSING}
					if len(got) != 3 {
						t.Fatal(got)
					}
					for i, s := range states {
						if got[i].State != s {
							t.Fatal(got)
						}
					}
					if (summary && res.Streams[0].Bytes_7D != nil) || (!summary && res.Streams[0].Bytes_7D == nil) {
						t.Fatal("history mode changed")
					}
				}
				placements := 0
				for _, read := range spy.reads {
					if !strings.Contains(read.query, "AS key, s.server_name") && !strings.Contains(read.query, "AS key, ls.server_name") {
						continue
					}
					placements++
					var keys []string
					for _, a := range read.args {
						if k, ok := a.([]string); ok {
							keys = k
						}
					}
					if len(keys) != want || !strings.Contains(read.query, "IN (SELECT unnest(?::VARCHAR[]))") {
						t.Fatalf("unscoped metadata keys=%v want %d", keys, want)
					}
					for _, key := range keys {
						if account != "" && !strings.HasPrefix(key, account+"/") {
							t.Fatal(keys)
						}
						if !hidden && (strings.Contains(key, "/KV_") || strings.Contains(key, "/OBJ_")) {
							t.Fatal(keys)
						}
					}
					if !strings.Contains(read.query, "LIMIT ?") {
						t.Fatal("missing SQL output cap")
					}
					rows, err := spy.DB.QueryContext(context.Background(), read.query, read.args...)
					if err != nil {
						t.Fatal(err)
					}
					count := 0
					for rows.Next() {
						count++
					}
					err = rows.Err()
					rows.Close()
					if err != nil {
						t.Fatal(err)
					}
					factor := 1
					if strings.Contains(read.query, "AS key, ls.server_name") {
						factor = 2
					}
					if count != want*factor {
						t.Fatalf("SQL returned %d metadata rows for %d keys", count, want)
					}
				}
				if placements != 2 {
					t.Fatalf("placement reads=%d", placements)
				}
			}
		}
	}
	// Legacy explicitly all-asset callers still get hidden buckets.
	all, err := p.streamReplicasAll(context.Background(), 37, "APP", "", "")
	if err != nil || len(all) != 3 {
		t.Fatalf("legacy all scope=%d err=%v", len(all), err)
	}
	bucket, err := p.streamReplicas(context.Background(), 37, "APP", "", "KV_")
	if err != nil || len(bucket) != 1 {
		t.Fatalf("bucket scope=%d err=%v", len(bucket), err)
	}
}

func TestListMetadataManyReportsAndBytesFailClosed(t *testing.T) {
	for _, kind := range []string{"reports", "bytes", "configured replicas"} {
		t.Run(kind, func(t *testing.T) {
			p, _, r := pageScaleProvider(t, 1)
			if kind == "reports" {
				execMetadata(t, p, fmt.Sprintf(`INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current) SELECT 37,'S0','APP','orders-00000','peer-' || i,true FROM range(%d) t(i)`, maxListMetadataRows))
			} else if kind == "configured replicas" {
				execMetadata(t, p, `INSERT INTO stream_config(server_id,account,stream,first_epoch,last_epoch,config_hash,replicas) VALUES ('S0','APP','orders-00000',1,37,'h',2147483647)`)
			} else {
				execMetadata(t, p, fmt.Sprintf(`INSERT INTO stream_config(server_id,account,stream,first_epoch,last_epoch,config_hash,replicas) VALUES ('S0','APP','orders-00000',1,37,repeat('x',%d),3)`, maxListMetadataBytes+1))
			}
			res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 1, SummaryOnly: true})
			if !errors.Is(err, api.ErrResourceExhausted) || res != nil {
				t.Fatalf("partial/healthy result on overflow: %v %v", res, err)
			}
		})
	}
}

func TestMetadataSQLScannerBudgetsAndCancellation(t *testing.T) {
	p, _, _ := pageScaleProvider(t, 1)
	spy := &metadataDB{DB: p.St.DB()}
	p.readDB = spy
	visits := 0
	err := p.readMetadata(context.Background(), `SELECT i AS n FROM range(100) t(i)`, nil, &metadataBudget{rows: 2, bytes: 1000}, func([]byte) error { visits++; return nil })
	if !errors.Is(err, api.ErrResourceExhausted) || visits != 2 {
		t.Fatalf("visits=%d err=%v", visits, err)
	}
	read := spy.reads[0]
	if read.args[len(read.args)-1] != 3 {
		t.Fatal(read.args)
	}
	rows, err := spy.DB.QueryContext(context.Background(), read.query, read.args...)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		n++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || n != 3 {
		t.Fatalf("SQL rows=%d err=%v", n, err)
	}
	visits = 0
	err = p.readMetadata(context.Background(), `SELECT repeat('x',100000) AS large`, nil, &metadataBudget{rows: 2, bytes: 32}, func([]byte) error { visits++; return nil })
	if !errors.Is(err, api.ErrResourceExhausted) || visits != 0 {
		t.Fatalf("oversized payload decoded: %d %v", visits, err)
	}
	read = spy.reads[len(spy.reads)-1]
	var raw string
	var size int
	if err := spy.DB.QueryRowContext(context.Background(), read.query, read.args...).Scan(&raw, &size); err != nil || raw != "" || size < 100000 {
		t.Fatalf("wire payload %d original %d err %v", len(raw), size, err)
	}
	// The SQL wire payload is cumulatively byte bounded, not only per-row.
	visits = 0
	err = p.readMetadata(context.Background(), `SELECT repeat('x',10) AS value FROM range(1000)`, nil, &metadataBudget{rows: 100, bytes: 64}, func([]byte) error { visits++; return nil })
	if !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatal(err)
	}
	read = spy.reads[len(spy.reads)-1]
	rows, err = spy.DB.QueryContext(context.Background(), read.query, read.args...)
	if err != nil {
		t.Fatal(err)
	}
	wire, n := 0, 0
	for rows.Next() {
		var payload string
		var size int
		if err = rows.Scan(&payload, &size); err != nil {
			t.Fatal(err)
		}
		wire += len(payload)
		n++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || wire > 64 || n != visits+1 {
		t.Fatalf("wire bytes=%d rows=%d visits=%d err=%v", wire, n, visits, err)
	}
	// A single shared budget spans successive metadata queries.
	b := &metadataBudget{rows: 3, bytes: 1000}
	visits = 0
	for i := 0; i < 2; i++ {
		err = p.readMetadata(context.Background(), `SELECT i FROM range(2) t(i)`, nil, b, func([]byte) error { visits++; return nil })
	}
	if !errors.Is(err, api.ErrResourceExhausted) || visits != 3 {
		t.Fatalf("shared visits=%d err=%v", visits, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.readMetadata(ctx, `SELECT 1`, nil, newMetadataBudget(), func([]byte) error { t.Fatal("decoded canceled read"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestListStatusMetadataMatchesLegacyCurrentPastAndMutes(t *testing.T) {
	p, _, r := pageScaleProvider(t, 2)
	execMetadata(t, p,
		`INSERT INTO findings(id,check_code,entity_type,entity_key,severity,state,last_transition,since,since_epoch,transitioned_at,last_seen_epoch,updated_epoch,detail) VALUES
 ('warn','stream-001','stream','APP/orders-00000','warning','firing','firing',TIMESTAMP '2026-10-17 07:00:00',30,TIMESTAMP '2026-10-17 07:00:00',37,37,'detail'),
 ('critical','consumer-001','consumer','APP/orders-00000/worker-00000','critical','firing','firing',TIMESTAMP '2026-10-17 07:00:00',30,TIMESTAMP '2026-10-17 07:00:00',37,37,'detail'),
 ('hidden','stream-001','stream','APP/KV_hidden','critical','firing','firing',TIMESTAMP '2026-10-17 07:00:00',30,TIMESTAMP '2026-10-17 07:00:00',37,37,'detail')`,
		`INSERT INTO finding_transitions(finding_id,check_code,entity_type,entity_key,epoch,time,kind,severity,detail) SELECT id,check_code,entity_type,entity_key,30,since,'firing',severity,detail FROM findings`,
		`INSERT INTO mutes(id,check_code,reason,muted_by,created_at,expires_at) VALUES(1,'consumer-001','test','test',TIMESTAMP '2026-10-17 08:00:00',TIMESTAMP '2026-10-17 15:00:00')`,
		`INSERT INTO inventory(account,kind,name,stream,consumer,replicas,source,loaded_at) VALUES ('APP','consumer','missing','orders-00000','missing',3,'test',TIMESTAMP '2026-10-17 07:00:00')`)
	for _, past := range []bool{false, true} {
		p.Clock = func() time.Time { return r.End.AsTime() }
		if past {
			p.Clock = func() time.Time { return r.End.AsTime().Add(time.Hour) }
		}
		legacy, err := p.firingFindings(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.listStatusFindings(context.Background(), r, []string{"APP/orders-00000"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatal(got)
		}
		e := refOf(v1.EntityType_ENTITY_TYPE_STREAM, []any{"APP", "orders-00000"})
		ls, lc := statusOf(legacy, e)
		gs, gc := statusOf(got, e)
		if ls != gs || !slices.Equal(lc, gc) {
			t.Fatalf("past %v status %v/%v codes %v/%v", past, ls, gs, lc, gc)
		}
		cs, err := p.ListConsumers(context.Background(), &v1.ListConsumersRequest{Range: r, Stream: "orders-00000"})
		if err != nil {
			t.Fatal(err)
		}
		var missing *v1.ConsumerSummary
		for _, c := range cs.Consumers {
			if !c.Present {
				missing = c
			}
		}
		if missing == nil || missing.GetReplicas() != 3 {
			t.Fatal("missing inventory consumer lost", cs)
		}
	}
	// Large unrelated config/details are never part of list metadata projections.
	execMetadata(t, p, `UPDATE inventory SET filter_subjects=[repeat('x',9000000)]`, `UPDATE findings SET detail=repeat('x',9000000)`)
	if _, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, SummaryOnly: true}); err != nil {
		t.Fatal(err)
	}
}

func TestListMetadataInventoryScopeAndAccountNames(t *testing.T) {
	p, _, r := pageScaleProvider(t, 1)
	execMetadata(t, p,
		`INSERT INTO inventory(account,kind,name,stream,consumer,source,loaded_at) SELECT 'OTHER','consumer','worker-'||i,'KV_hidden','worker-'||i,'test',TIMESTAMP '2026-10-17 07:00:00' FROM range(20001) t(i)`,
		`INSERT INTO accounts(account,account_name,is_system,first_epoch,last_epoch) VALUES ('APP','Shop',false,1,37),('OTHER','Other shop',false,1,37)`)
	res, err := p.ListConsumers(context.Background(), &v1.ListConsumersRequest{Range: r, Account: "APP"})
	if err != nil || res.TotalCount != 1 {
		t.Fatal(res, err)
	}
	if _, err := p.ListConsumers(context.Background(), &v1.ListConsumersRequest{Range: r}); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatal("unbounded missing consumers", err)
	}
	names, err := p.AccountNamesFor(context.Background(), []string{"APP"})
	if err != nil || len(names) != 1 || names["APP"] != "Shop" {
		t.Fatal(names, err)
	}
	// Empty retained key sets select nothing, never every metadata entity.
	got, err := p.listStatusFindings(context.Background(), r, []string{}, true)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}

}

func TestListHiddenMetadataNeverRead(t *testing.T) {
	p, _, r := pageScaleProvider(t, 1)
	execMetadata(t, p,
		`INSERT INTO stream_stats(epoch,server_id,account,stream,is_leader) SELECT 37,'S0','APP',CASE WHEN i%2=0 THEN 'KV_' ELSE 'OBJ_' END || i,true FROM range(20001) t(i)`,
		`INSERT INTO stream_config(server_id,account,stream,first_epoch,last_epoch,config_hash,replicas) SELECT 'S0','APP',stream,1,37,repeat('x',1024),3 FROM stream_stats WHERE epoch=37 AND stream<>'orders-00000'`,
		`INSERT INTO stream_replicas(epoch,server_id,account,stream,peer_name,current) SELECT 37,'S0','APP',stream,'peer',true FROM stream_stats WHERE epoch=37 AND stream<>'orders-00000'`,
		`INSERT INTO inventory(account,kind,name,stream,consumer,source,loaded_at) SELECT 'APP','consumer',stream,stream,'hidden','test',TIMESTAMP '2026-10-17 07:00:00' FROM stream_stats WHERE epoch=37 AND stream<>'orders-00000'`)
	for _, summary := range []bool{false, true} {
		res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 1, SummaryOnly: summary})
		if err != nil || res.GetTotalCount() != 1 || len(res.GetStreams()) != 1 {
			t.Fatal(res, err)
		}
		if res.Streams[0].Entity.Key != "APP/orders-00000" {
			t.Fatal(res)
		}
	}
	// Including hidden matches still fails the ranking bound, rather than truncating totals.
	if _, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, IncludeKv: true, SummaryOnly: true}); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatal(err)
	}
}

func TestListBaseProjectionBytesFailBeforeContext(t *testing.T) {
	p, _, r := pageScaleProvider(t, 20)
	execMetadata(t, p, `UPDATE stream_stats SET leader=repeat('x',600000) WHERE epoch=37`)
	if res, err := p.ListStreams(context.Background(), &v1.ListStreamsRequest{Range: r, Limit: 1, SummaryOnly: true}); !errors.Is(err, api.ErrResourceExhausted) || res != nil {
		t.Fatal(res, err)
	}
	execMetadata(t, p, `UPDATE consumer_stats SET leader=repeat('x',600000) WHERE epoch=37`)
	if res, err := p.ListConsumers(context.Background(), &v1.ListConsumersRequest{Range: r, Limit: 1}); !errors.Is(err, api.ErrResourceExhausted) || res != nil {
		t.Fatal(res, err)
	}
}
