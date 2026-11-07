package storeprov_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The first claimant by name (nats-0) lists no replicas; nats-2,
// the other claimant, does. placementOf picks nats-0. Do details agree?
func TestDetailsFirstClaimantWithoutList(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	ids := []string{"S0", "S1", "S2"}
	for i, id := range ids {
		r.server(id, "nats-"+strconv.Itoa(i))
	}
	r.epoch(1, t0, ids...)
	r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 1})
	r.add("consumers", map[string]any{"account": "APP", "stream": "orders", "consumer": "ship", "first_epoch": 1, "last_epoch": 1})
	for _, id := range ids {
		r.add("stream_stats", map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "orders", "is_leader": id != "S1", "messages": 10})
		r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": "orders",
			"first_epoch": 1, "last_epoch": 1, "config_hash": "h", "replicas": 3})
		r.add("consumer_stats", map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "orders", "consumer": "ship",
			"is_leader": id != "S1", "delivered_stream_seq": 5})
	}
	r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": "S2", "account": "APP", "stream": "orders", "peer_name": "nats-1",
		"current": false, "offline": false, "lag": 4})
	r.add("consumer_replicas", map[string]any{"epoch": 1, "server_id": "S2", "account": "APP", "stream": "orders", "consumer": "ship",
		"peer_name": "nats-1", "current": false, "offline": false, "lag": 4})
	c := serveSeries(t, st, t0.Add(30*time.Second))
	rg := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}
	pl := must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{Range: rg})))
	a := asset(pl, "APP/orders")
	t.Logf("matrix stream: leader %s cells %v nats-1 lag %v", a.GetLeader(), cellsText(a), cellOn(a, "nats-1").Lag)
	for _, v := range must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: "APP", Stream: "orders", Range: rg}))).GetServers() {
		t.Logf("GetStream %s leader=%v current=%v lag=%v", v.GetServer(), v.GetIsLeader(), v.Current, v.Lag)
		if v.GetServer() == "nats-1" && v.Lag != nil {
			t.Errorf("GetStream nats-1 shows nats-2's lag %d, matrix (leader nats-0) shows none", v.GetLag())
		}
	}
	for _, v := range must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: "APP", Stream: "orders", Consumer: "ship", Range: rg}))).GetServers() {
		if v.GetServer() == "nats-1" && v.Lag != nil {
			t.Errorf("GetConsumer nats-1 shows nats-2's lag %d", v.GetLag())
		}
	}
}

// An offline replica listed by the chosen leader, and a second
// claimant that names it online, for a stream and its consumer. The
// matrix shows nats-1 OFFLINE from the chosen leader's list and nats-2
// DISAGREES. This test pins two gaps of the details as they are today
// (both leftovers in docs/plans/next.md): they list only the servers
// with a stats row, so nats-1 is missing from them, and they report
// each server's own leader claim, so nats-0 and nats-2 both show as
// leader where the matrix names nats-0 alone.
func TestDetailsOfflineByChosenLeader(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	ids := []string{"S0", "S1", "S2"}
	for i, id := range ids {
		r.server(id, "nats-"+strconv.Itoa(i))
	}
	r.epoch(1, t0, "S0", "S2")
	r.add("streams", map[string]any{"account": "APP", "stream": "orders", "first_epoch": 1, "last_epoch": 1})
	r.add("consumers", map[string]any{"account": "APP", "stream": "orders", "consumer": "ship", "first_epoch": 1, "last_epoch": 1})
	for _, id := range []string{"S0", "S2"} {
		r.add("stream_stats", map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "orders", "is_leader": true, "messages": 10})
		r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": "orders",
			"first_epoch": 1, "last_epoch": 1, "config_hash": "h", "replicas": 3})
		r.add("consumer_config", map[string]any{"server_id": id, "account": "APP", "stream": "orders", "consumer": "ship",
			"first_epoch": 1, "last_epoch": 1, "config_hash": "c", "durable_name": "ship"})
		r.add("consumer_stats", map[string]any{"epoch": 1, "server_id": id, "account": "APP", "stream": "orders", "consumer": "ship",
			"is_leader": true, "delivered_stream_seq": 5})
	}
	r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": "S2", "account": "APP", "stream": "orders", "peer_name": "nats-1",
		"current": true, "offline": false, "lag": 0})
	r.add("stream_replicas", map[string]any{"epoch": 1, "server_id": "S0", "account": "APP", "stream": "orders", "peer_name": "nats-1",
		"current": false, "offline": true, "lag": 7})
	r.add("consumer_replicas", map[string]any{"epoch": 1, "server_id": "S2", "account": "APP", "stream": "orders", "consumer": "ship",
		"peer_name": "nats-1", "current": true, "offline": false, "lag": 0})
	r.add("consumer_replicas", map[string]any{"epoch": 1, "server_id": "S0", "account": "APP", "stream": "orders", "consumer": "ship",
		"peer_name": "nats-1", "current": false, "offline": true, "lag": 7})
	c := serveSeries(t, st, t0.Add(30*time.Second))
	rg := &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Hour)), End: timestamppb.New(t0.Add(30 * time.Second))}
	a := asset(must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{Range: rg}))), "APP/orders")
	if got, want := a.GetLeader(), "nats-0"; got != want {
		t.Errorf("matrix leader %q, want %q", got, want)
	}
	if got, want := strings.Join(cellsText(a), " "), "nats-0:LEADER nats-1:OFFLINE nats-2:DISAGREES"; got != want {
		t.Errorf("matrix cells %q, want %q", got, want)
	}
	var got []string
	for _, v := range must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: "APP", Stream: "orders", Range: rg}))).GetServers() {
		got = append(got, fmt.Sprintf("%s leader=%v current=%v offline=%v lag=%v", v.GetServer(), v.GetIsLeader(), v.GetCurrent(), v.GetOffline(), v.Lag != nil))
	}
	want := []string{
		"nats-0 leader=true current=true offline=false lag=false",
		"nats-2 leader=true current=true offline=false lag=false",
	}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("GetStream servers\n got %q\nwant %q", got, want)
	}
	ca := asset(must(c.GetPlacement(ctx, connect.NewRequest(&v1.GetPlacementRequest{Range: rg}))), "APP/orders/ship")
	if got, want := ca.GetLeader(), "nats-0"; got != want {
		t.Errorf("matrix consumer leader %q, want %q", got, want)
	}
	if got, want := strings.Join(cellsText(ca), " "), "nats-0:LEADER nats-1:OFFLINE nats-2:DISAGREES"; got != want {
		t.Errorf("matrix consumer cells %q, want %q", got, want)
	}
	got = nil
	for _, v := range must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: "APP", Stream: "orders", Consumer: "ship", Range: rg}))).GetServers() {
		got = append(got, fmt.Sprintf("%s leader=%v current=%v offline=%v lag=%v", v.GetServer(), v.GetIsLeader(), v.GetCurrent(), v.GetOffline(), v.Lag != nil))
	}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("GetConsumer servers\n got %q\nwant %q", got, want)
	}
}
