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
	status := map[string]v1.Status{"servers": v1.Status_STATUS_OK, "streams": v1.Status_STATUS_INFO,
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
		"clients": v1.Status_STATUS_WARNING, "consumers": v1.Status_STATUS_WARNING, "storage": v1.Status_STATUS_WARNING,
		"kv": v1.Status_STATUS_WARNING, "object_stores": v1.Status_STATUS_OK}
	for id, st := range status {
		if got := tiles[id].GetStatus(); got != st {
			t.Errorf("tile %s status %v, want %v", id, got, st)
		}
	}
	if got := tiles["consumers"].GetSubtitle(); got != "1 behind" {
		t.Errorf("consumers subtitle = %q, want 1 behind", got)
	}
	if got := tiles["streams"].GetSubtitle(); got != "13 of 13 replicated · 1 lagging" {
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
	global := streams("example-global")
	if got := global["shipping-updates"]; !slices.Equal(got, want) {
		t.Errorf("shipping-updates = %v, want %v", got, want)
	}
	// The other squares the store draws: a follower with another
	// configuration, one the leader lists that does not report the stream,
	// and a stream without a leader.
	for stream, want := range map[string][]string{
		"customer-notifications": {"nats-ap-2=PLACEMENT_STATE_LEADER", "nats-ap-3=PLACEMENT_STATE_IN_SYNC", "nats-ap-4=PLACEMENT_STATE_DISAGREES"},
		"return-requests":        {"nats-us-6=PLACEMENT_STATE_LEADER", "nats-us-3=PLACEMENT_STATE_MISSING", "nats-us-8=PLACEMENT_STATE_IN_SYNC"},
		"basket-events":          {"nats-eu-6=PLACEMENT_STATE_UNSPECIFIED", "nats-eu-7=PLACEMENT_STATE_UNSPECIFIED", "nats-eu-8=PLACEMENT_STATE_UNSPECIFIED"},
	} {
		if got := global[stream]; !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", stream, got, want)
		}
	}
	// The disagreeing stream says so, with the differing hash in its
	// detail; the missing replica has no server view.
	d := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{System: "example-global", Account: "APP", Stream: "customer-notifications"})))
	if !d.GetStream().GetServersDisagree() || len(d.GetDisagreements()) != 1 || d.GetDisagreements()[0].GetField() != "config_hash" {
		t.Errorf("customer-notifications: disagree %v, %v", d.GetStream().GetServersDisagree(), d.GetDisagreements())
	}
	r := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{System: "example-global", Account: "APP", Stream: "return-requests"})))
	if len(r.GetServers()) != 2 || slices.ContainsFunc(r.GetServers(), func(v *v1.StreamServerView) bool { return v.GetServer() == "nats-us-3" }) {
		t.Errorf("return-requests views = %v", r.GetServers())
	}
	// The Overview shows the ten worst streams: they include each of
	// these squares.
	top := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: "example-global", Limit: 10}))).GetStreams()
	for _, want := range []string{"shipping-updates", "customer-notifications", "return-requests", "basket-events"} {
		if !slices.ContainsFunc(top, func(s *v1.StreamSummary) bool { return s.GetEntity().GetStream() == want }) {
			t.Errorf("%s is not among the ten worst streams", want)
		}
	}
	w := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{System: "example-global", Account: "APP", Stream: "basket-events"})))
	if w.GetStream().GetLeader() != "" || slices.ContainsFunc(w.GetServers(), (*v1.StreamServerView).GetIsLeader) {
		t.Errorf("basket-events leader %q, views %v", w.GetStream().GetLeader(), w.GetServers())
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
	// Each row has its own backlog, not one number copied to every row.
	pending, acks := map[int64]bool{}, map[int64]bool{}
	for _, x := range cs {
		pending[x.GetNumPending()], acks[x.GetNumAckPending()] = true, true
	}
	if len(pending) < 4 || len(acks) < 4 {
		t.Errorf("busiest rows share their backlog: %d pending values, %d ack pending values", len(pending), len(acks))
	}
	all := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Sort: v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE}))).GetConsumers()
	last := all[len(all)-1]
	if last.GetEntity().GetName() != "analytics-aggregator" || last.DeliveredPerSecond != nil {
		t.Errorf("last = %s (rate set %v), want the missing analytics-aggregator without a rate", last.GetEntity().GetName(), last.DeliveredPerSecond != nil)
	}
}

// search-index's replica on nats-ap-1 is behind its leader, below
// stream-003's 1000 entries: BEHIND in the placement, not current with its
// lag in the stream's server views, and no lagging finding.
func TestFakeLaggingReplica(t *testing.T) {
	c := demo(t)
	sys := "example-global"
	res := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{System: sys, Account: "APP", Stream: "search-index"})))
	want := []string{"nats-ap-0=PLACEMENT_STATE_LEADER", "nats-ap-1=PLACEMENT_STATE_BEHIND", "nats-ap-2=PLACEMENT_STATE_IN_SYNC"}
	if got := placementText(res.GetStream().GetPlacement()); !slices.Equal(got, want) {
		t.Errorf("placement = %v, want %v", got, want)
	}
	for _, v := range res.GetServers() {
		if v.GetServer() == "nats-ap-1" && (v.GetCurrent() || v.GetLag() != 420) {
			t.Errorf("nats-ap-1 view = current %v, lag %d; want not current, 420", v.GetCurrent(), v.GetLag())
		}
	}
	for _, f := range res.GetFindings() {
		if f.GetCheckCode() == "stream-003" {
			t.Errorf("a lag of 420 fired %v", f)
		}
	}
}

// The leader comes first even when another replica sorts before it.
func TestFakePlacementLeaderFirst(t *testing.T) {
	c := demo(t)
	for _, s := range must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: "example-global"}))).GetStreams() {
		if s.GetEntity().GetStream() != "order-archive" {
			continue
		}
		want := []string{"nats-ap-2=PLACEMENT_STATE_LEADER", "nats-ap-1=PLACEMENT_STATE_IN_SYNC", "nats-ap-5=PLACEMENT_STATE_IN_SYNC"}
		if got := placementText(s.GetPlacement()); !slices.Equal(got, want) {
			t.Fatalf("order-archive = %v, want %v", got, want)
		}
		return
	}
	t.Fatal("no order-archive")
}

// A count of one takes the singular suffix: "1 bucket", never "1 buckets".
func TestTileSuffixesAgreeWithTheCount(t *testing.T) {
	for _, sys := range []string{"example-prod", "example-global", "example-staging"} {
		for id, tile := range overviewTiles(t, sys) {
			suf := tile.GetValueSuffix()
			if tile.Value == nil || suf == "" || suf[0] == '/' || suf[:2] == "of" {
				continue
			}
			if one, plural := tile.GetValue() == 1, suf[len(suf)-1] == 's'; one == plural {
				t.Errorf("%s tile %s: %v %q", sys, id, tile.GetValue(), suf)
			}
		}
	}
}

// example-global has a consumer for each delivery rate the store gives:
// set, zero for the paused reorder-planner, and unset for invoice-renderer,
// created in the newest epoch; the unset one sorts last by rate.
func TestGlobalConsumerRates(t *testing.T) {
	c := demo(t)
	cs := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{System: "example-global", Sort: v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE}))).GetConsumers()
	by := map[string]*v1.ConsumerSummary{}
	for _, x := range cs {
		by[x.GetEntity().GetConsumer()] = x
	}
	if p := by["reorder-planner"]; !p.GetPaused() || p.DeliveredPerSecond == nil || p.GetDeliveredPerSecond() != 0 {
		t.Errorf("reorder-planner paused %v at %v/s (set %v)", p.GetPaused(), p.GetDeliveredPerSecond(), p.DeliveredPerSecond != nil)
	}
	if n := by["invoice-renderer"]; !n.GetPresent() || n.DeliveredPerSecond != nil || n.GetLastDelivered() != nil || n.GetNumPending() != 0 {
		t.Errorf("invoice-renderer = %v", n)
	}
	if last := cs[len(cs)-1]; last.GetEntity().GetConsumer() != "invoice-renderer" {
		t.Errorf("last by rate = %s, want invoice-renderer", last.GetEntity().GetConsumer())
	}
	d := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{System: "example-global", Account: "APP", Stream: "order-events", Consumer: "invoice-renderer"})))
	if got := d.GetServers()[0].GetCreated().AsTime(); !got.Equal(at(14, 29, 20)) {
		t.Errorf("invoice-renderer created %v", got)
	}
}
