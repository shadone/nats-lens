package storeprov_test

import (
	"connectrpc.com/connect"
	"encoding/json"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

// Wire-level reproducer runs against the old schema too: the old API serves
// the matrix successfully but cannot supply the requested observed evidence.
func TestPlacementObservedDuration(t *testing.T) {
	st := observationStore(t)
	c := serveSeries(t, st, t0.Add(90*time.Second))
	req := &v1.GetPlacementRequest{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(`{"selectedAsset":"APP/orders","selectedServer":"nats-1"}`), req); err != nil {
		t.Fatal(err)
	}
	req.Range = &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(90 * time.Second))}
	res := must(c.GetPlacement(ctx, connect.NewRequest(req)))
	if got := cellOn(asset(res, "APP/orders"), "nats-1").GetState(); got != v1.PlacementState_PLACEMENT_STATE_BEHIND {
		t.Fatalf("negative control: state %v", got)
	}
	b, err := protojson.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Observation *struct {
			Since     string
			Samples   int
			Truncated bool
			State     string
		}
	}
	if err = json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Observation == nil {
		t.Fatal("selected behind cell has no observed-duration evidence")
	}
	if wire.Observation.Since != t0.Add(30*time.Second).Format(time.RFC3339) || wire.Observation.Samples != 2 || wire.Observation.Truncated {
		t.Fatalf("observation %s", b)
	}
}

func observationStore(t *testing.T) *store.Store {
	st := emptyStore(t)
	r := rows{t, st}
	for _, id := range []string{"S0", "S1"} {
		r.server(id, "nats-"+id[1:])
	}
	r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 3})
	for e := int64(1); e <= 3; e++ {
		r.epoch(e, t0.Add(time.Duration(e-1)*30*time.Second), "S0", "S1")
		for _, id := range []string{"S0", "S1"} {
			r.add("stream_stats", map[string]any{"epoch": e, "server_id": id, "account": "APP", "stream": "orders", "is_leader": id == "S0", "created": t0.Add(-time.Hour)})
		}
		r.add("stream_replicas", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "orders", "peer_name": "nats-1", "current": e == 1, "lag": int64(0), "offline": false})
	}
	for _, id := range []string{"S0", "S1"} {
		r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 3, "replicas": 2, "config_hash": "h"})
	}
	return st
}

func TestPlacementObservationStoredBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, sql, boundary string
		samples             int32
		selected            string
	}{
		{"current false zero lag", `UPDATE stream_replicas SET current=false`, "history_limit", 3, "APP/orders"},
		{"offline without own report", `UPDATE stream_replicas SET offline=true; DELETE FROM stream_stats WHERE server_id='S1'`, "history_limit", 3, "APP/orders"},
		{"missing replica with successful JSZ", `DELETE FROM stream_stats WHERE server_id='S1'`, "history_limit", 3, "APP/orders"},
		{"missing reply cannot establish missing replica continuity", `DELETE FROM stream_stats WHERE server_id='S1'; UPDATE epoch_endpoints SET state='missing' WHERE epoch=2 AND server_id='S1' AND endpoint='JSZ'`, "evidence_unknown", 1, "APP/orders"},
		{"no creation evidence", `UPDATE stream_stats SET created=NULL WHERE epoch=2`, "evidence_unknown", 1, "APP/orders"},
		{"failed collection", `UPDATE epoch_endpoints SET state='error' WHERE epoch=2 AND server_id='S0' AND endpoint='JSZ'`, "evidence_unknown", 1, "APP/orders"},
		{"collection gap", `UPDATE epochs SET started_at=started_at - INTERVAL 5 MINUTE WHERE epoch=2`, "collection_gap", 1, "APP/orders"},
		{"incarnation boundary", `UPDATE stream_stats SET created=TIMESTAMP '2026-01-01' WHERE epoch=2 AND server_id='S1'`, "state_changed", 1, "APP/orders"},
		{"all copies change incarnation", `UPDATE stream_stats SET created=TIMESTAMP '2026-01-01' WHERE epoch=2`, "identity_changed", 1, "APP/orders"},
		{"in-sync creation disagreement", `UPDATE stream_replicas SET current=true; UPDATE stream_stats SET created=TIMESTAMP '2026-01-01' WHERE server_id='S1'`, "history_limit", 3, "APP/orders"},
		{"unobserved later identity cannot override observed process", `INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) VALUES ('new-S1','nats-1',3,3,TIMESTAMP '2026-10-17',TIMESTAMP '2026-10-17')`, "state_changed", 2, "APP/orders"},
		{"unrelated account", `SELECT 1`, "", 0, "OTHER/orders"},
		{"absent asset", `SELECT 1`, "", 0, "APP/absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := observationStore(t)
			if _, err := st.DB().Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			c := serveSeries(t, st, t0.Add(90*time.Second))
			req := &v1.GetPlacementRequest{SelectedAsset: tc.selected, SelectedServer: "nats-1", Range: &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(90 * time.Second))}}
			res := must(c.GetPlacement(ctx, connect.NewRequest(req)))
			o := res.GetObservation()
			if tc.samples == 0 {
				if o != nil {
					t.Fatalf("unexpected evidence %v", o)
				}
				return
			}
			if o == nil || o.Samples != tc.samples || o.Boundary != tc.boundary {
				t.Fatalf("observation %v", o)
			}
			cell := cellOn(asset(res, "APP/orders"), "nats-1")
			if cell.State != o.State {
				t.Fatalf("matrix state %v != observed %v", cell.State, o.State)
			}
		})
	}
}

func TestPlacementObservationConsumerAndRange(t *testing.T) {
	st := observationStore(t)
	for _, q := range []string{
		`INSERT INTO consumer_stats(epoch,server_id,account,stream,consumer,is_leader,created) SELECT epoch,server_id,account,stream,'shipper',is_leader,created FROM stream_stats`,
		`INSERT INTO consumer_config(server_id,account,stream,consumer,first_epoch,last_epoch,durable_name,replicas,config_hash) SELECT server_id,account,stream,'shipper',first_epoch,last_epoch,'shipper',replicas,config_hash FROM stream_config`,
		`INSERT INTO consumer_replicas(epoch,server_id,account,stream,consumer,peer_name,current,offline,lag) SELECT epoch,server_id,account,stream,'shipper',peer_name,current,offline,lag FROM stream_replicas`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	c := serveSeries(t, st, t0.Add(90*time.Second))
	req := &v1.GetPlacementRequest{SelectedAsset: "APP/orders/shipper", SelectedServer: "nats-1", Range: &v1.TimeRange{Start: timestamppb.New(t0.Add(30 * time.Second)), End: timestamppb.New(t0.Add(60 * time.Second))}}
	res := must(c.GetPlacement(ctx, connect.NewRequest(req)))
	o := res.GetObservation()
	if o == nil || o.Samples != 1 || !o.AsOf.AsTime().Equal(t0.Add(30*time.Second)) || !o.Since.AsTime().Equal(o.AsOf.AsTime()) || !o.Truncated {
		t.Fatalf("range/as-of/zero-span %v", o)
	}
	if cellOn(asset(res, "APP/orders/shipper"), "nats-1").GetLag() != 0 {
		t.Fatal("zero lag lost")
	}
	req.SelectedServer = "nats-0"
	if o = must(c.GetPlacement(ctx, connect.NewRequest(req))).GetObservation(); o != nil {
		t.Fatalf("leader tenure must remain separate: %v", o)
	}
	req.SelectedServer = "nats-1"
	req.Range.End = timestamppb.New(t0.Add(30 * time.Second))
	req.Range.Start = timestamppb.New(t0)
	if o = must(c.GetPlacement(ctx, connect.NewRequest(req))).GetObservation(); o != nil {
		t.Fatalf("in-sync negative control %v", o)
	}
}

func TestPlacementObservationSystemIsolation(t *testing.T) {
	a, b := observationStore(t), observationStore(t)
	if _, err := b.DB().Exec(`UPDATE stream_replicas SET current=true`); err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return t0.Add(90 * time.Second) }
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.Systems{
		{Ref: &v1.SystemRef{Id: "a"}, Provider: &storeprov.Provider{St: a, Interval: 30 * time.Second, Clock: clock}},
		{Ref: &v1.SystemRef{Id: "b"}, Provider: &storeprov.Provider{St: b, Interval: 30 * time.Second, Clock: clock}},
	}})
	for _, system := range []string{"a", "b", "a"} {
		res := must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{System: system, SelectedAsset: "APP/orders", SelectedServer: "nats-1", Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(clock())}})))
		if (res.Observation != nil) != (system == "a") {
			t.Fatalf("system %s: %v", system, res.Observation)
		}
	}
}

func TestPlacementObservationRequiresMatrixCell(t *testing.T) {
	st := observationStore(t)
	for _, q := range []string{
		`INSERT INTO consumer_stats(epoch,server_id,account,stream,consumer,is_leader,created) SELECT epoch,server_id,account,stream,'ephemeral',is_leader,created FROM stream_stats`,
		`INSERT INTO consumer_config(server_id,account,stream,consumer,first_epoch,last_epoch,replicas,config_hash) SELECT server_id,account,stream,'ephemeral',first_epoch,last_epoch,replicas,config_hash FROM stream_config`,
		`INSERT INTO consumer_replicas(epoch,server_id,account,stream,consumer,peer_name,current,offline,lag) SELECT epoch,server_id,account,stream,'ephemeral',peer_name,current,offline,lag FROM stream_replicas`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	c := serveSeries(t, st, t0.Add(90*time.Second))
	res := must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{SelectedAsset: "APP/orders/ephemeral", SelectedServer: "nats-1", Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(90 * time.Second))}})))
	if asset(res, "APP/orders/ephemeral") != nil {
		t.Fatal("negative control: ephemeral appeared in matrix")
	}
	if res.Observation != nil {
		t.Fatalf("evidence for a cell outside the matrix: %v", res.Observation)
	}
}
