package storeprov_test

import (
	"context"
	"errors"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func storageCompareFixture(t *testing.T) (*storeprov.Provider, *v1.GetStorageChangeRequest) {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"S0", "S1", "S2"} {
		r.server(id, "nats-"+id[1:])
	}
	for e := int64(1); e <= 2; e++ {
		when := at.Add(time.Duration(e-1) * time.Minute)
		r.epoch(e, when, "S0", "S1", "S2")
		if _, err := st.DB().Exec(`UPDATE epochs SET finished_at=started_at+INTERVAL 5 SECOND,expected_names=['nats-0','nats-1','nats-2'] WHERE epoch=?`, e); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"S0", "S1", "S2"} {
			v := int64(1000)
			if e == 2 {
				v = 1100
			}
			r.add("server_stats", map[string]any{"epoch": e, "server_id": id, "js_enabled": true, "js_storage": v, "js_memory": 10})
		}
	}
	if _, err := st.DB().Exec(`UPDATE epoch_endpoints SET state='missing' WHERE epoch=2 AND server_id='S2' AND endpoint='JSZ';UPDATE server_stats SET js_storage=NULL,js_memory=NULL WHERE epoch=2 AND server_id='S2'`); err != nil {
		t.Fatal(err)
	}
	created := at.Add(-time.Hour)
	add := func(e int64, id, name, storage string, b int64, replicas int32) {
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": id, "account": "APP", "stream": name, "created": created, "bytes": b, "is_leader": id == "S0", "raft_group": "group-" + name})
		r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": name, "created": created, "first_epoch": e, "last_epoch": e, "config_hash": name + id + string(rune(e)), "storage": storage, "replicas": replicas})
	}
	add(1, "S0", "orders", "file", 100, 1)
	add(2, "S0", "orders", "file", 180, 2)
	add(2, "S1", "orders", "file", 180, 2)
	add(1, "S0", "old-archive", "file", 50, 1)
	add(1, "S2", "payments", "file", 80, 1)
	add(1, "S0", "sessions", "memory", 10, 1)
	add(2, "S0", "sessions", "memory", 30, 1)
	r.add("events", map[string]any{"id": int64(9007199254740999), "time": at.Add(30 * time.Second), "received_at": at.Add(31 * time.Second), "source": "jetstream", "kind": "stream.deleted", "account": "APP", "stream": "old-archive", "summary": "archive deleted"})
	return &storeprov.Provider{St: st, Interval: time.Minute, Clock: func() time.Time { return at.Add(2 * time.Minute) }}, &v1.GetStorageChangeRequest{System: "example-prod", A: timestamppb.New(at.Add(5 * time.Second)), B: timestamppb.New(at.Add(time.Minute + 5*time.Second)), PageSize: 100}
}
func findStorageRow(t *testing.T, r *v1.GetStorageChangeResponse, stream, kind, storage string) *v1.StorageChangeRow {
	t.Helper()
	for _, x := range r.Rows {
		if x.Stream == stream && x.RowKind == kind && x.Storage == storage {
			return x
		}
	}
	t.Fatalf("missing %s/%s/%s", kind, stream, storage)
	return nil
}
func TestStorageChangeStoredAttribution(t *testing.T) {
	p, req := storageCompareFixture(t)
	facts, err := p.StorageChange(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := api.FinishStorageChange(req, facts)
	if err != nil {
		t.Fatal(err)
	}
	if res.A.Epoch != 1 || res.B.Epoch != 2 || res.B.InventoryComplete || res.A.GetAge().AsDuration() != 0 {
		t.Fatalf("endpoint evidence=%v %v", res.A, res.B)
	}
	order := findStorageRow(t, res, "orders", "resource", "file")
	if order.Delta.GetLogical() != 80 || order.Delta.GetObservedReplicas() != 260 || order.Delta.GetEstimatedReplicas() != 260 {
		t.Fatalf("replica versus logical contribution=%v", order)
	}
	deleted := findStorageRow(t, res, "old-archive", "resource", "file")
	if deleted.Delta.GetLogical() != -50 || len(deleted.EventIds) != 1 || deleted.EventIds[0] != 9007199254740999 {
		t.Fatalf("observed deletion evidence=%v", deleted)
	}
	missing := findStorageRow(t, res, "payments", "resource", "file")
	if missing.GetAfter().Logical != nil || missing.GetDelta().Logical != nil {
		t.Fatalf("missing became zero: %v", missing)
	}
	for _, total := range res.Totals {
		if total.Storage == "file" && (total.GetDelta().ServerReported != nil || total.Unreconciled != nil) {
			t.Fatalf("missing server report fabricated residual: %v", total)
		}
	}
}
func TestStorageChangeNoFutureAndPinnedRetention(t *testing.T) {
	p, req := storageCompareFixture(t)
	req.B = timestamppb.New(req.B.AsTime().Add(-time.Second))
	f, err := p.StorageChange(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if f.B == nil || f.B.Epoch != 1 || f.B.FinishedAt.AsTime().After(req.B.AsTime()) {
		t.Fatalf("future observation selected: %v", f.B)
	}
	req.EpochB = 2
	if _, err = p.StorageChange(ctx, req); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("future pin: %v", err)
	}
	req.EpochB = 99
	if _, err = p.StorageChange(ctx, req); !errors.Is(err, api.ErrFailedPrecondition) {
		t.Fatalf("missing pin fallback: %v", err)
	}
}
func TestStorageChangeAccountAndReplay(t *testing.T) {
	p, req := storageCompareFixture(t)
	req.Account = "OTHER"
	f, err := p.StorageChange(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Rows {
		if r.RowKind == "resource" {
			t.Fatalf("account leak=%v", r)
		}
	}
	req.Account = ""
	req.PageSize = 1
	f, err = p.StorageChange(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	first, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	req.PageToken = first.NextPageToken
	req.EpochA = first.A.Epoch
	req.EpochB = first.B.Epoch
	p.Clock = func() time.Time { return req.B.AsTime().Add(time.Hour) }
	f, err = p.StorageChange(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	if proto.Equal(first.Rows[0], second.Rows[0]) || second.TotalCount != first.TotalCount {
		t.Fatal("unstable pinned continuation")
	}
}

func TestStorageChangeIncarnationEvidenceBoundaries(t *testing.T) {
	for _, which := range []string{"unknown_created", "replacement_predates_a", "created_after_collection"} {
		t.Run(which, func(t *testing.T) {
			p, req := storageCompareFixture(t)
			switch which {
			case "unknown_created":
				if _, err := p.St.DB().Exec(`UPDATE stream_stats SET created=NULL WHERE stream='orders';UPDATE stream_config SET created=NULL WHERE stream='orders'`); err != nil {
					t.Fatal(err)
				}
			case "replacement_predates_a":
				created := req.A.AsTime().Add(-30 * time.Minute)
				for _, q := range []string{`UPDATE stream_stats SET created=? WHERE epoch=2 AND stream='orders'`, `UPDATE stream_config SET created=? WHERE first_epoch=2 AND stream='orders'`} {
					if _, err := p.St.DB().Exec(q, created); err != nil {
						t.Fatal(err)
					}
				}
			case "created_after_collection":
				created := req.B.AsTime().Add(time.Minute)
				for _, q := range []string{`UPDATE stream_stats SET created=? WHERE epoch=2 AND stream='orders'`, `UPDATE stream_config SET created=? WHERE first_epoch=2 AND stream='orders'`} {
					if _, err := p.St.DB().Exec(q, created); err != nil {
						t.Fatal(err)
					}
				}
			}
			f, err := p.StorageChange(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			res, err := api.FinishStorageChange(req, f)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range res.Rows {
				if row.Stream != "orders" {
					continue
				}
				if row.GetDelta().Logical != nil {
					t.Fatalf("uncertain incarnation produced contribution: %v", row)
				}
				if which == "unknown_created" && row.GetBefore().Logical == nil {
					t.Fatal("known endpoint bytes were discarded with unknown incarnation")
				}
				if which == "replacement_predates_a" && row.GetBefore().GetLogical() == 100 && row.GetAfter().Logical != nil {
					t.Fatal("a replacement predating A proved retirement after A")
				}
			}
		})
	}
}

func TestStorageChangeRestartMigrationAndBuckets(t *testing.T) {
	p, req := storageCompareFixture(t)
	r := rows{t, p.St}
	r.server("R0", "nats-0")
	for _, q := range []string{`UPDATE server_stats SET server_id='R0' WHERE epoch=2 AND server_id='S0'`, `UPDATE stream_stats SET server_id='R0' WHERE epoch=2 AND server_id='S0'`, `UPDATE stream_config SET server_id='R0' WHERE first_epoch=2 AND server_id='S0'`, `UPDATE epoch_endpoints SET server_id='R0' WHERE epoch=2 AND server_id='S0'`, `UPDATE stream_config SET storage='file' WHERE first_epoch=2 AND stream='sessions'`} {
		if _, err := p.St.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for e := int64(1); e <= 2; e++ {
		id := "S0"
		if e == 2 {
			id = "R0"
		}
		created := req.A.AsTime().Add(-time.Hour)
		r.add("stream_stats", map[string]any{"epoch": e, "server_id": id, "account": "APP", "stream": "KV_order-cache", "created": created, "bytes": 10 + e, "is_leader": true})
		r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": "KV_order-cache", "created": created, "first_epoch": e, "last_epoch": e, "config_hash": "cache", "storage": "file", "replicas": 1})
	}
	f, err := p.StorageChange(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	memory := findStorageRow(t, res, "sessions", "resource", "memory")
	file := findStorageRow(t, res, "sessions", "resource", "file")
	if memory.Delta.GetLogical() != -10 || file.Delta.GetLogical() != 30 {
		t.Fatalf("storage migration mixed file and memory: %v / %v", memory, file)
	}
	bucket := findStorageRow(t, res, "KV_order-cache", "resource", "file")
	if bucket.Bucket != "order-cache" || bucket.Kind != v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE || bucket.Delta.GetLogical() != 1 {
		t.Fatalf("bucket missing/mislabeled=%v", bucket)
	}
	found := false
	for _, row := range res.Rows {
		if row.RowKind == "server" && row.Server == "nats-0" && row.Storage == "file" {
			found = true
			if row.Delta.GetServerReported() != 100 || row.ProcessesA[0] != "S0" || row.ProcessesB[0] != "R0" {
				t.Fatalf("restart identity lost=%v", row)
			}
		}
	}
	if !found {
		t.Fatal("server restart row missing")
	}
}
func TestStorageChangeBoundedCaptureAndCancellation(t *testing.T) {
	p, req := storageCompareFixture(t)
	for _, q := range []string{`INSERT INTO stream_config (server_id,account,stream,created,first_epoch,last_epoch,config_hash,storage,replicas) SELECT 'S0','APP','bulk-'||i,TIMESTAMP '2026-10-01',1,1,'config','file',1 FROM range(21000) r(i)`, `INSERT INTO stream_stats (epoch,server_id,account,stream,created,bytes,is_leader) SELECT 1,'S0','APP','bulk-'||i,TIMESTAMP '2026-10-01',1,true FROM range(21000) r(i)`} {
		if _, err := p.St.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.StorageChange(ctx, req); !errors.Is(err, api.ErrResourceExhausted) {
		t.Fatalf("ranking overflow produced partial success: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.StorageChange(canceled, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	req.Account = "OTHER"
	if _, err := p.StorageChange(ctx, req); err != nil {
		t.Fatalf("scope did not constrain bounded reads: %v", err)
	}
}
