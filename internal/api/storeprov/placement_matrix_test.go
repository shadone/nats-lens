package storeprov_test

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
)

// placementClient serves st with its clock 30 seconds after the
// conformance store's last epoch.
func placementClient(t *testing.T, st *store.Store) (natslensv1connect.LensServiceClient, time.Time) {
	t.Helper()
	clock := confAt(confEpochs).Add(30 * time.Second)
	p := &storeprov.Provider{St: st, Interval: confStep, Live: true, Clock: func() time.Time { return clock }}
	_, c := apitest.ServeHandler(t, &api.Handler{Systems: api.One(api.DefaultSystemID, api.DefaultSystemID, p)})
	return c, clock
}

func getPlacement(t *testing.T, c natslensv1connect.LensServiceClient, end time.Time, f v1.PlacementFilter) *v1.GetPlacementResponse {
	t.Helper()
	r := &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}
	return must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{Range: r, Filter: f})))
}

// asset finds an asset by key among the streams and their consumers.
func asset(res *v1.GetPlacementResponse, key string) *v1.PlacementAsset {
	for _, s := range res.GetStreams() {
		if s.GetEntity().GetKey() == key {
			return s
		}
		for _, c := range s.GetConsumers() {
			if c.GetEntity().GetKey() == key {
				return c
			}
		}
	}
	return nil
}

// cellsText is "server:STATE" per cell, in order.
func cellsText(a *v1.PlacementAsset) []string {
	var out []string
	for _, c := range a.GetCells() {
		out = append(out, c.GetServer()+":"+c.GetState().String()[len("PLACEMENT_STATE_"):])
	}
	return out
}

func cellOn(a *v1.PlacementAsset, server string) *v1.PlacementCell {
	for _, c := range a.GetCells() {
		if c.GetServer() == server {
			return c
		}
	}
	return nil
}

// Each cell state of the matrix, read from the conformance store: the
// states of the summaries' squares, with what each server reports.
func TestPlacementCellStates(t *testing.T) {
	st, _ := conformanceStore(t)
	c, clock := placementClient(t, st)
	res := getPlacement(t, c, clock, v1.PlacementFilter_PLACEMENT_FILTER_UNSPECIFIED)
	if got := res.GetEpochAt().AsTime(); !got.Equal(confAt(confEpochs)) {
		t.Errorf("epoch_at = %v, want %v", got, confAt(confEpochs))
	}
	var names []string
	for _, s := range res.GetServers() {
		names = append(names, s.GetName())
	}
	if !slices.Equal(names, []string{"nats-0", "nats-1", "nats-2", "nats-3"}) {
		t.Errorf("servers = %v", names)
	}
	var keys []string
	for _, s := range res.GetStreams() {
		keys = append(keys, s.GetEntity().GetKey())
	}
	if want := []string{"APP/KV_sessions", "APP/orders", "APP/payments", "APP/returns", "APP/wishlist"}; !slices.Equal(keys, want) {
		t.Errorf("streams = %v, want %v", keys, want)
	}
	for key, want := range map[string][]string{
		"APP/orders":           {"nats-0:LEADER", "nats-1:IN_SYNC", "nats-2:BEHIND"},
		"APP/payments":         {"nats-0:LEADER", "nats-1:DISAGREES", "nats-2:OFFLINE"},
		"APP/returns":          {"nats-1:LEADER", "nats-2:MISSING", "nats-3:IN_SYNC"},
		"APP/wishlist":         {"nats-0:UNSPECIFIED", "nats-1:UNSPECIFIED", "nats-2:UNSPECIFIED"},
		"APP/KV_sessions":      {"nats-2:LEADER"},
		"APP/payments/settler": {"nats-0:LEADER", "nats-1:DISAGREES"},
		"APP/orders/fast":      {"nats-0:LEADER"},
	} {
		a := asset(res, key)
		if a == nil {
			t.Errorf("%s not listed", key)
			continue
		}
		if got := cellsText(a); !slices.Equal(got, want) {
			t.Errorf("%s cells = %v, want %v", key, got, want)
		}
	}
	orders := asset(res, "APP/orders")
	if orders.GetLeader() != "nats-0" || orders.GetMessages() != 1000*confEpochs || orders.GetReplicas() != 3 || orders.GetUnplaced() != 0 {
		t.Errorf("orders = leader %q, %d msgs, R%d, %d unplaced", orders.GetLeader(), orders.GetMessages(), orders.GetReplicas(), orders.GetUnplaced())
	}
	if b := cellOn(orders, "nats-2"); b.GetLag() != 500 {
		t.Errorf("orders on nats-2: lag %d", b.GetLag())
	}
	if l := cellOn(orders, "nats-0"); l.Lag != nil || !slices.Equal(l.GetPeers(), []string{"nats-1", "nats-2"}) || l.GetMessages() != 1000*confEpochs {
		t.Errorf("orders leader cell: lag %v, peers %v, %d msgs", l.Lag, l.GetPeers(), l.GetMessages())
	}
	if f := cellOn(orders, "nats-1"); len(f.GetPeers()) != 0 || f.GetMessages() != 10 {
		t.Errorf("orders on nats-1: peers %v (it lists none), %d msgs", f.GetPeers(), f.GetMessages())
	}
	pay := asset(res, "APP/payments")
	if d := cellOn(pay, "nats-1"); !slices.Equal(d.GetDiffers(), []string{"config_hash"}) || d.GetConfigHash() != "other" {
		t.Errorf("payments on nats-1: differs %v, hash %q", d.GetDiffers(), d.GetConfigHash())
	}
	if o := cellOn(pay, "nats-2"); o.Messages != nil || o.GetCreated() != nil || len(o.GetDiffers()) != 0 {
		t.Errorf("payments on offline nats-2 has a report: %v", o)
	}
	if !pay.GetServersDisagree() {
		t.Error("payments: servers_disagree unset")
	}
	settler := asset(res, "APP/payments/settler")
	s1 := cellOn(settler, "nats-1")
	if !slices.Equal(s1.GetDiffers(), []string{"created", "config_hash"}) && !slices.Equal(s1.GetDiffers(), []string{"config_hash", "created"}) {
		t.Errorf("settler on nats-1 differs %v, want created and config_hash", s1.GetDiffers())
	}
	// Two servers, a tie: the leader's value is the reference.
	if l := cellOn(settler, "nats-0"); len(l.GetDiffers()) != 0 {
		t.Errorf("settler leader differs %v", l.GetDiffers())
	}
	if !slices.Equal(cellOn(settler, "nats-0").GetPeers(), []string{"nats-1"}) || !settler.GetDurable() || settler.GetUnplaced() != 1 {
		t.Errorf("settler: peers %v, durable %v, unplaced %d", cellOn(settler, "nats-0").GetPeers(), settler.GetDurable(), settler.GetUnplaced())
	}
	if fast := asset(res, "APP/orders/fast"); fast.GetUnplaced() != 2 || fast.GetReplicas() != 3 {
		t.Errorf("fast: R%d with %d unplaced, want R3 with 2", fast.GetReplicas(), fast.GetUnplaced())
	}
	if w := asset(res, "APP/wishlist"); w.GetLeader() != "" || w.LeaderSince != nil {
		t.Errorf("wishlist: leader %q since %v", w.GetLeader(), w.GetLeaderSince())
	}
}

// The counts cover every asset whatever the filter; a filter keeps the
// streams that pass it or have consumers that do.
func TestPlacementFilters(t *testing.T) {
	st, _ := conformanceStore(t)
	c, clock := placementClient(t, st)
	want := &v1.PlacementCounts{All: 9, ServersDisagree: 2, ReplicaBehind: 1, Missing: 5, SingleReplica: 1}
	for _, tc := range []struct {
		f    v1.PlacementFilter
		want map[string]bool // asset key: whether it matches
	}{
		{v1.PlacementFilter_PLACEMENT_FILTER_SERVERS_DISAGREE, map[string]bool{"APP/payments": true, "APP/payments/settler": true}},
		{v1.PlacementFilter_PLACEMENT_FILTER_REPLICA_BEHIND, map[string]bool{"APP/orders": true}},
		{v1.PlacementFilter_PLACEMENT_FILTER_MISSING, map[string]bool{"APP/orders": false, "APP/orders/fast": true, "APP/orders/fresh": true,
			"APP/orders/planner": true, "APP/payments": false, "APP/payments/settler": true, "APP/returns": true}},
		{v1.PlacementFilter_PLACEMENT_FILTER_SINGLE_REPLICA, map[string]bool{"APP/KV_sessions": true}},
	} {
		res := getPlacement(t, c, clock, tc.f)
		if got := res.GetCounts(); got.GetAll() != want.GetAll() || got.GetServersDisagree() != want.GetServersDisagree() ||
			got.GetReplicaBehind() != want.GetReplicaBehind() || got.GetMissing() != want.GetMissing() || got.GetSingleReplica() != want.GetSingleReplica() {
			t.Errorf("%v: counts %v, want %v", tc.f, got, want)
		}
		got := map[string]bool{}
		for _, s := range res.GetStreams() {
			got[s.GetEntity().GetKey()] = s.GetMatches()
			for _, c := range s.GetConsumers() {
				got[c.GetEntity().GetKey()] = c.GetMatches()
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("%v: listed %v, want %v", tc.f, got, tc.want)
			continue
		}
		for k, m := range tc.want {
			if g, ok := got[k]; !ok || g != m {
				t.Errorf("%v: %s listed %v matching %v, want matching %v", tc.f, k, ok, g, m)
			}
		}
	}
}

// A range without an epoch lists the servers and no assets; an unknown
// filter is invalid.
func TestPlacementEmptyAndInvalid(t *testing.T) {
	st, _ := conformanceStore(t)
	c, _ := placementClient(t, st)
	res := getPlacement(t, c, confStart.Add(-48*time.Hour), v1.PlacementFilter_PLACEMENT_FILTER_UNSPECIFIED)
	if len(res.GetStreams()) != 0 || res.GetEpochAt() != nil || res.GetCounts().GetAll() != 0 {
		t.Errorf("before the history: %d streams, epoch_at %v, counts %v", len(res.GetStreams()), res.GetEpochAt(), res.GetCounts())
	}
	_, err := c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{Filter: v1.PlacementFilter(99)}))
	if codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("unknown filter: %v, want invalid_argument", err)
	}
}

// The leader's tenure comes from its own report; a past range reads its
// own epoch.
func TestPlacementLeaderSinceAndPastEpoch(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	since := t0.Add(-3 * time.Hour)
	r.epoch(1, t0, "S0", "S1")
	r.epoch(2, t0.Add(time.Minute), "S0", "S1")
	r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 2})
	for _, id := range []string{"S0", "S1"} {
		r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": "orders", "first_epoch": 1,
			"last_epoch": 2, "config_hash": "h", "replicas": 2})
	}
	// Epoch 1: nats-0 leads, nats-1 behind. Epoch 2: nats-1 leads.
	r.add("stream_stats", map[string]any{"epoch": 1, "server_id": "S0", "account": "APP", "stream": "orders", "is_leader": true, "leader_since": since})
	r.add("stream_stats", map[string]any{"epoch": 1, "server_id": "S1", "account": "APP", "stream": "orders", "is_leader": false})
	r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": "S0", "account": "APP", "stream": "orders", "peer_name": "nats-1",
		"current": false, "offline": false, "lag": 12})
	r.add("stream_stats", map[string]any{"epoch": 2, "server_id": "S0", "account": "APP", "stream": "orders", "is_leader": false})
	r.add("stream_stats", map[string]any{"epoch": 2, "server_id": "S1", "account": "APP", "stream": "orders", "is_leader": true,
		"leader_since": t0.Add(50 * time.Second)})
	r.add("stream_replicas", map[string]any{"epoch": 2, "server_id": "S1", "account": "APP", "stream": "orders", "peer_name": "nats-0",
		"current": true, "offline": false, "lag": 0})
	c := serveSeries(t, st, t0.Add(90*time.Second))
	at := func(end time.Time) *v1.PlacementAsset {
		r := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(end)}
		res := must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{Range: r})))
		return asset(res, "APP/orders")
	}
	old := at(t0.Add(30 * time.Second))
	if got := cellsText(old); !slices.Equal(got, []string{"nats-0:LEADER", "nats-1:BEHIND"}) || !old.GetLeaderSince().AsTime().Equal(since) {
		t.Errorf("epoch 1: %v, leader since %v", got, old.GetLeaderSince())
	}
	if l := cellOn(old, "nats-1").GetLag(); l != 12 {
		t.Errorf("epoch 1: nats-1 lag %d, want 12", l)
	}
	cur := at(t0.Add(90 * time.Second))
	if got := cellsText(cur); !slices.Equal(got, []string{"nats-1:LEADER", "nats-0:IN_SYNC"}) || !cur.GetLeaderSince().AsTime().Equal(t0.Add(50*time.Second)) {
		t.Errorf("epoch 2: %v, leader since %v", got, cur.GetLeaderSince())
	}
}
