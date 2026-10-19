package fake_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// placementAt reads a demo system's matrix for the hour before end.
func placementAt(t *testing.T, system string, end time.Time, f v1.PlacementFilter) *v1.GetPlacementResponse {
	t.Helper()
	r := &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}
	return must(demo(t).GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{System: system, Range: r, Filter: f})))
}

// cellStates is "server:STATE" per cell of the asset with key, in order.
func cellStates(res *v1.GetPlacementResponse, key string) []string {
	for _, s := range res.GetStreams() {
		for _, a := range append([]*v1.PlacementAsset{s}, s.GetConsumers()...) {
			if a.GetEntity().GetKey() != key {
				continue
			}
			var out []string
			for _, c := range a.GetCells() {
				out = append(out, c.GetServer()+":"+strings.TrimPrefix(c.GetState().String(), "PLACEMENT_STATE_"))
			}
			return out
		}
	}
	return nil
}

func assetOf(res *v1.GetPlacementResponse, key string) *v1.PlacementAsset {
	for _, s := range res.GetStreams() {
		for _, a := range append([]*v1.PlacementAsset{s}, s.GetConsumers()...) {
			if a.GetEntity().GetKey() == key {
				return a
			}
		}
	}
	return nil
}

// example-global shows every state: the replica behind, the one that
// disagrees, the missing one, the stream without a leader and the offline
// server; its consumers follow their streams.
func TestDemoPlacementGlobal(t *testing.T) {
	res := placementAt(t, fake.GlobalSystem, fake.FixedNow, v1.PlacementFilter_PLACEMENT_FILTER_UNSPECIFIED)
	k := func(name string) string { return fake.Account + "/" + name }
	for key, want := range map[string][]string{
		k("search-index"):                  {"nats-ap-0:LEADER", "nats-ap-1:BEHIND", "nats-ap-2:IN_SYNC"},
		k("customer-notifications"):        {"nats-ap-2:LEADER", "nats-ap-3:IN_SYNC", "nats-ap-4:DISAGREES"},
		k("return-requests"):               {"nats-us-6:LEADER", "nats-us-3:MISSING", "nats-us-8:IN_SYNC"},
		k("return-requests/refund-issuer"): {"nats-us-6:LEADER", "nats-us-3:MISSING", "nats-us-8:IN_SYNC"},
		k("basket-events"):                 {"nats-eu-6:UNSPECIFIED", "nats-eu-7:UNSPECIFIED", "nats-eu-8:UNSPECIFIED"},
		k("shipping-updates"):              {"nats-us-5:LEADER", "nats-us-3:IN_SYNC", "nats-us-4:OFFLINE"},
		k("search-index/search-indexer"):   {"nats-ap-0:LEADER", "nats-ap-1:IN_SYNC", "nats-ap-2:IN_SYNC"},
	} {
		if got := cellStates(res, key); !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if d := assetOf(res, k("customer-notifications")).GetCells()[2]; !slices.Equal(d.GetDiffers(), []string{"config_hash"}) {
		t.Errorf("nats-ap-4 differs %v", d.GetDiffers())
	}
	if c := assetOf(res, k("search-index")).GetCells()[1]; c.GetLag() != 420 {
		t.Errorf("nats-ap-1 lag %d, want 420", c.GetLag())
	}
	off := assetOf(res, k("shipping-updates")).GetCells()[2]
	if off.Messages != nil || len(off.GetPeers()) != 0 {
		t.Errorf("offline nats-us-4 reports %v", off)
	}
	if l := assetOf(res, k("order-jobs")).GetCells()[0]; !slices.Equal(l.GetPeers(), []string{"nats-eu-1", "nats-eu-2"}) {
		t.Errorf("order-jobs leader names %v", l.GetPeers())
	}
	if n := res.GetCounts(); n.GetAll() == 0 || n.GetServersDisagree() != 1 || n.GetReplicaBehind() != 1 || n.GetMissing() < 2 {
		t.Errorf("counts = %v", n)
	}
	if len(res.GetServers()) != 24 || res.GetEpochAt() == nil {
		t.Errorf("%d servers, epoch at %v", len(res.GetServers()), res.GetEpochAt())
	}
}

// The matrix follows the range's epoch: a day ago nats-us-4 still
// answered.
func TestDemoPlacementFollowsTheRange(t *testing.T) {
	key := fake.Account + "/shipping-updates"
	day := placementAt(t, fake.GlobalSystem, fake.FixedNow.Add(-24*time.Hour), v1.PlacementFilter_PLACEMENT_FILTER_UNSPECIFIED)
	if got := cellStates(day, key); !slices.Equal(got, []string{"nats-us-3:LEADER", "nats-us-4:IN_SYNC", "nats-us-5:IN_SYNC"}) {
		t.Errorf("a day ago %s = %v", key, got)
	}
	if at := day.GetEpochAt().AsTime(); fake.FixedNow.Add(-24*time.Hour).Sub(at) > time.Minute {
		t.Errorf("epoch at %v", at)
	}
	// The stream list reads the same epoch.
	r := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-25 * time.Hour)), End: timestamppb.New(fake.FixedNow.Add(-24 * time.Hour))}
	for _, s := range must(demo(t).ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: fake.GlobalSystem, Range: r}))).GetStreams() {
		if s.GetEntity().GetKey() == key && s.GetPlacement()[1].GetState() != v1.PlacementState_PLACEMENT_STATE_IN_SYNC {
			t.Errorf("ListStreams a day ago: %v", s.GetPlacement())
		}
	}
}

// In example-prod resize-worker's copy on nats-0 has another creation
// time: the servers disagree filter lists it, with that cell marked.
func TestDemoPlacementProdDisagreement(t *testing.T) {
	res := placementAt(t, fake.Cluster, fake.FixedNow, v1.PlacementFilter_PLACEMENT_FILTER_SERVERS_DISAGREE)
	if len(res.GetStreams()) != 1 || res.GetStreams()[0].GetEntity().GetStream() != "order-jobs" || res.GetStreams()[0].GetMatches() {
		t.Fatalf("servers disagree lists %v", res.GetStreams())
	}
	cons := res.GetStreams()[0].GetConsumers()
	if len(cons) != 1 || cons[0].GetEntity().GetConsumer() != "resize-worker" {
		t.Fatalf("consumers %v", cons)
	}
	for _, c := range cons[0].GetCells() {
		if want := c.GetServer() == "nats-0"; slices.Contains(c.GetDiffers(), "created") != want {
			t.Errorf("%s differs %v", c.GetServer(), c.GetDiffers())
		}
	}
	if res.GetCounts().GetSingleReplica() != 1 {
		t.Errorf("single replica %d, want KV_worker-presence", res.GetCounts().GetSingleReplica())
	}
}
