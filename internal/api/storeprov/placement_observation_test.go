package storeprov_test

import (
	"connectrpc.com/connect"
	"encoding/json"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

// Wire-level reproducer runs against the old schema too: the old API serves
// the matrix successfully but cannot supply the requested observed evidence.
func TestPlacementObservedDuration(t *testing.T) {
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
