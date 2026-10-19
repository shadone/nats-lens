package storeprov_test

// GetPlacement with two servers that claim the lead.

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Two servers claim the lead; nats-0 wins by name. The matrix cell of
// nats-1 is BEHIND by the winner's list, so its lag must be the winner's
// (9), and the tenure must be the winner's, not the other claimant's.
func TestPlacementTwoClaimantsLagAndTenure(t *testing.T) {
	type list struct {
		lead    string
		current bool
		lag     int64
	}
	winner, loser := list{"S0", false, 9}, list{"S2", true, 0}
	for _, order := range [][]list{{winner, loser}, {loser, winner}} {
		st := emptyStore(t)
		r := rows{t, st}
		ids := []string{"S0", "S1", "S2"}
		for i, id := range ids {
			r.server(id, "nats-"+strconv.Itoa(i))
		}
		r.epoch(1, t0, ids...)
		r.add("streams", map[string]any{"account": "APP", "stream": "order-jobs", "first_epoch": 1, "last_epoch": 1})
		since := map[string]time.Time{"S0": t0.Add(-time.Hour), "S2": t0.Add(-10 * time.Minute)}
		for _, id := range ids {
			cols := map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "order-jobs",
				"is_leader": id != "S1", "messages": 10, "bytes": 100}
			if s, ok := since[id]; ok {
				cols["leader_since"] = s
			}
			r.add("stream_stats", cols)
			r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": "order-jobs",
				"first_epoch": 1, "last_epoch": 1, "config_hash": "h", "replicas": 3})
		}
		for _, l := range order {
			r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": l.lead, "account": "APP", "stream": "order-jobs",
				"peer_name": "nats-1", "current": l.current, "offline": false, "lag": l.lag})
		}
		c := serveSeries(t, st, t0.Add(30*time.Second))
		rg := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}
		a := asset(must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{Range: rg}))), "APP/order-jobs")
		if got := cellsText(a); !slices.Equal(got, []string{"nats-0:LEADER", "nats-1:BEHIND", "nats-2:DISAGREES"}) {
			t.Errorf("order %v: cells %v", order, got)
		}
		if l := cellOn(a, "nats-1"); l.Lag == nil || l.GetLag() != 9 {
			t.Errorf("order %v: nats-1 is BEHIND by nats-0's list, lag %v, want 9", order, l.Lag)
		}
		if !a.GetLeaderSince().AsTime().Equal(since["S0"]) {
			t.Errorf("order %v: leader nats-0, leader_since %v, want nats-0's %v", order, a.GetLeaderSince().AsTime(), since["S0"])
		}
	}
}
