package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ListAssetPlacement places a stream as ListStreams does, without the
// configured replica no server names; a stream the epoch does not hold is
// not reported, and the answer keeps the order asked.
func TestListAssetPlacementAsListStreams(t *testing.T) {
	st := placementStore(t, 4, []replicaRow{
		{id: "S1", name: "nats-1", reports: true, leader: true, hash: "h"},
		{id: "S2", name: "nats-2", reports: true, hash: "h", current: false, lag: 40},
		{id: "S3", name: "nats-3", offline: true},
	})
	want := states(streamPlacementOf(t, st))
	c := serveSeries(t, st, t0.Add(30*time.Second))
	r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}
	res := must(c.ListAssetPlacement(ctx, connect.NewRequest(&v1.ListAssetPlacementRequest{Range: r, Entities: []*v1.EntityRef{
		{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/gone"},
		{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/order-jobs"},
	}})))
	as := res.GetAssets()
	if len(as) != 2 || as[0].GetReported() || len(as[0].GetReplicas()) != 0 {
		t.Fatalf("assets = %v; want APP/gone not reported first", as)
	}
	got := states(as[1].GetReplicas())
	// The fourth configured replica, which no server names, is left out.
	if !slices.Equal(append(got, "="+v1.PlacementState_PLACEMENT_STATE_MISSING.String()), want) || as[1].GetLeader() != "nats-1" || !as[1].GetReported() {
		t.Errorf("order-jobs: %v led by %q; ListStreams places %v", got, as[1].GetLeader(), want)
	}
}
