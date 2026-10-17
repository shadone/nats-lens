package fake_test

import (
	"slices"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var tileIDs = []string{"servers", "meta_leader", "streams", "kv", "object_stores", "consumers", "accounts", "clients", "storage"}

func overviewTiles(t *testing.T, sys string) map[string]*v1.Tile {
	t.Helper()
	o := must(demo(t).GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: sys})))
	var ids []string
	out := map[string]*v1.Tile{}
	for _, tile := range o.GetTiles() {
		ids = append(ids, tile.GetId())
		out[tile.GetId()] = tile
		if tile.GetDescription() == "" || tile.GetLabel() == "" {
			t.Errorf("%s tile %s: label %q, description %q", sys, tile.GetId(), tile.GetLabel(), tile.GetDescription())
		}
	}
	if !slices.Equal(ids, tileIDs) {
		t.Fatalf("%s tiles = %v, want %v", sys, ids, tileIDs)
	}
	return out
}

// example-prod's tiles: the meta leader by name with the upgrade's two
// changes, the counts from its streams, and statuses from its findings.
func TestProdTiles(t *testing.T) {
	tiles := overviewTiles(t, "example-prod")
	want := map[string]float64{"servers": 3, "meta_leader": 2, "streams": 8, "kv": 3, "object_stores": 2, "consumers": 13,
		"accounts": 1, "clients": 182}
	for id, v := range want {
		if tiles[id].Value == nil || tiles[id].GetValue() != v {
			t.Errorf("tile %s = %v, want %v", id, tiles[id].Value, v)
		}
	}
	if m := tiles["meta_leader"]; m.GetValueText() != "nats-0" || m.GetSubtitle() != "2 leader changes in range" {
		t.Errorf("meta leader = %q, %q", m.GetValueText(), m.GetSubtitle())
	}
	status := map[string]v1.Status{"servers": v1.Status_STATUS_OK, "streams": v1.Status_STATUS_WARNING,
		"consumers": v1.Status_STATUS_CRITICAL, "accounts": v1.Status_STATUS_NO_DATA}
	for id, st := range status {
		if got := tiles[id].GetStatus(); got != st {
			t.Errorf("tile %s status %v, want %v", id, got, st)
		}
	}
	if got := tiles["clients"].GetSubtitle(); got != "8 apps · 7 slow" {
		t.Errorf("clients subtitle = %q, want 8 apps and the 7 slow consumer events", got)
	}
	if s := tiles["storage"]; s.GetLimit() != 150*(1<<30) || s.GetValueSuffix() != "of 150 GiB" {
		t.Errorf("storage = %v", s)
	}
}

// example-global's tiles follow its story: a server down, a replica
// offline, the slow consumer and the growing backlog.
func TestGlobalTiles(t *testing.T) {
	tiles := overviewTiles(t, "example-global")
	if m := tiles["meta_leader"]; m.GetValueText() != "nats-eu-1" || m.GetValue() != 0 {
		t.Errorf("meta leader = %q, %v changes", m.GetValueText(), m.GetValue())
	}
	status := map[string]v1.Status{"servers": v1.Status_STATUS_WARNING, "streams": v1.Status_STATUS_WARNING,
		"clients": v1.Status_STATUS_WARNING, "consumers": v1.Status_STATUS_WARNING, "storage": v1.Status_STATUS_INFO,
		"kv": v1.Status_STATUS_WARNING, "object_stores": v1.Status_STATUS_OK}
	for id, st := range status {
		if got := tiles[id].GetStatus(); got != st {
			t.Errorf("tile %s status %v, want %v", id, got, st)
		}
	}
	if got := tiles["consumers"].GetSubtitle(); got != "1 behind" {
		t.Errorf("consumers subtitle = %q, want 1 behind", got)
	}
	if got := tiles["streams"].GetSubtitle(); got != "11 of 11 replicated · 0 lagging" {
		t.Errorf("streams subtitle = %q", got)
	}
}

func placementText(ps []*v1.ReplicaPlacement) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.GetServer()+"="+p.GetState().String())
	}
	return out
}

// A stream's squares are its replicas by server name: the leader, the
// followers in sync, and a replica on a server that does not answer
// offline.
func TestFakePlacement(t *testing.T) {
	c := demo(t)
	streams := func(sys string) map[string][]string {
		out := map[string][]string{}
		for _, s := range must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: sys, IncludeKv: true}))).GetStreams() {
			out[s.GetEntity().GetStream()] = placementText(s.GetPlacement())
		}
		return out
	}
	want := []string{"nats-0=PLACEMENT_STATE_LEADER", "nats-1=PLACEMENT_STATE_IN_SYNC", "nats-2=PLACEMENT_STATE_IN_SYNC"}
	if got := streams("example-prod")["order-jobs"]; !slices.Equal(got, want) {
		t.Errorf("order-jobs = %v, want %v", got, want)
	}
	want = []string{"nats-us-3=PLACEMENT_STATE_LEADER", "nats-us-4=PLACEMENT_STATE_OFFLINE", "nats-us-5=PLACEMENT_STATE_IN_SYNC"}
	if got := streams("example-global")["shipping-updates"]; !slices.Equal(got, want) {
		t.Errorf("shipping-updates = %v, want %v", got, want)
	}
}

// DELIVERED_RATE with limit 6 gives the six fastest durables, fastest
// first; the missing consumer has no rate.
func TestFakeBusiestConsumers(t *testing.T) {
	c := demo(t)
	cs := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Sort: v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE, Limit: 6}))).GetConsumers()
	if len(cs) != 6 {
		t.Fatalf("%d consumers, want 6", len(cs))
	}
	for i, x := range cs {
		if x.DeliveredPerSecond == nil || (i > 0 && x.GetDeliveredPerSecond() > cs[i-1].GetDeliveredPerSecond()) {
			t.Fatalf("row %d %s at %v/s (set %v) is not in falling order", i, x.GetEntity().GetName(), x.GetDeliveredPerSecond(), x.DeliveredPerSecond != nil)
		}
	}
	all := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Sort: v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE}))).GetConsumers()
	last := all[len(all)-1]
	if last.GetEntity().GetName() != "analytics-aggregator" || last.DeliveredPerSecond != nil {
		t.Errorf("last = %s (rate set %v), want the missing analytics-aggregator without a rate", last.GetEntity().GetName(), last.DeliveredPerSecond != nil)
	}
}
