package storeprov_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func tileByID(tiles []*v1.Tile, id string) *v1.Tile {
	for _, t := range tiles {
		if t.GetId() == id {
			return t
		}
	}
	return nil
}

// The overview returns the nine tiles in order, each with a description
// for its tooltip.
func TestOverviewTileSet(t *testing.T) {
	o := must(client(t).GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour()})))
	var ids []string
	for _, tile := range o.GetTiles() {
		ids = append(ids, tile.GetId())
		if tile.GetDescription() == "" {
			t.Errorf("tile %s has no description", tile.GetId())
		}
	}
	want := []string{"servers", "meta_leader", "streams", "kv", "object_stores", "consumers", "accounts", "clients", "storage"}
	if !slices.Equal(ids, want) {
		t.Fatalf("tiles = %v, want %v", ids, want)
	}
}

// The meta leader tile names the leader most servers report, and counts
// the changes in the range.
func TestMetaLeaderTileValueText(t *testing.T) {
	o := must(client(t).GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour()})))
	m := tileByID(o.GetTiles(), "meta_leader")
	if m == nil {
		t.Fatal("no meta_leader tile")
	}
	if m.GetValueText() != "nats-1" {
		t.Errorf("value_text = %q, want nats-1", m.GetValueText())
	}
	if m.GetSeries().GetMetric() != "cluster.meta_leader_changes" || m.Value == nil || m.GetValue() != 0 {
		t.Errorf("series %s, value %v; want cluster.meta_leader_changes and 0 changes", m.GetSeries().GetMetric(), m.Value)
	}
}

// The clients tile counts the distinct applications, the part of a
// connection name before "@": two pods of catalog-api are one app.
func TestClientsTileCountsApps(t *testing.T) {
	st := fixtureStore(t)
	for cid, name := range map[int]string{20: "catalog-api@pod-7f", 21: "catalog-api@pod-2c", 22: "media-worker@pod-1"} {
		if _, err := st.DB().Exec(`INSERT INTO connections (epoch, server_id, cid, name) VALUES (3, 'NFAKESERVER0ID', ?, ?)`, cid, name); err != nil {
			t.Fatal(err)
		}
	}
	_, c0 := serve(t, st)
	o := must(c0.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour()})))
	c := tileByID(o.GetTiles(), "clients")
	if c == nil || c.GetSubtitle() == "" {
		t.Fatalf("clients tile = %v", c)
	}
	if !strings.HasPrefix(c.GetSubtitle(), "3 apps · ") || !strings.HasSuffix(c.GetSubtitle(), " slow") {
		t.Errorf("subtitle = %q, want 3 apps and the slow consumer events", c.GetSubtitle())
	}
}

// The meta leader tile needs more than half of the expected servers to
// name the leader, as cluster.meta_leader_changes does: one server of
// three naming it is not enough, though no other server names another.
func TestMetaLeaderTileNeedsAMajority(t *testing.T) {
	for name, votes := range map[string][]string{"": {"nats-1", "", ""}, "nats-1": {"nats-1", "nats-1", ""}} {
		st := emptyStore(t)
		r := rows{t, st}
		ids := []string{"S0", "S1", "S2"}
		for i, id := range ids {
			r.server(id, "nats-"+strconv.Itoa(i))
		}
		r.epoch(1, t0, ids...)
		for i, id := range ids {
			r.add("server_stats", map[string]any{"epoch": 1, "server_id": id, "meta_leader": votes[i]})
		}
		c := serveSeries(t, st, t0.Add(30*time.Second))
		o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{
			Range: &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}})))
		if got := tileByID(o.GetTiles(), "meta_leader").GetValueText(); got != name {
			t.Errorf("votes %v: value_text = %q, want %q", votes, got, name)
		}
	}
}
